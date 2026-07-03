package testrepo

import (
	"encoding/json"
	"fmt"
)

// ReqReqLink is one Requirement -> Requirement directional issue link (e.g.
// "requires"). Stored locally in requirement_link and pushed to Jira on commit
// via the "req_req_link_set" pending-change entity type.
type ReqReqLink struct {
	FromKey  string `json:"fromKey"`
	ToKey    string `json:"toKey"`
	LinkType string `json:"linkType"`
	LinkID   string `json:"linkId"`
}

// reqqLinkSnap is one current ToKey + LinkID pair captured before a set, so
// the removed links can be deleted in Jira by id and restored on discard.
type reqqLinkSnap struct {
	ToKey  string `json:"toKey"`
	LinkID string `json:"linkId"`
}

// ReplaceAllReqReqLinks overwrites the cached Requirement->Requirement links for
// a profile (used by the syncer during a full requirements sync). It replaces
// all links in one transaction.
func (r *Repository) ReplaceAllReqReqLinks(profileID string, links []ReqReqLink) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM requirement_link WHERE profile_id = ?`, profileID,
	); err != nil {
		return fmt.Errorf("clear requirement links: %w", err)
	}
	for _, l := range links {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO requirement_link
			   (profile_id, from_requirement_key, to_requirement_key, link_type, link_id)
			 VALUES (?, ?, ?, ?, ?)`,
			profileID, l.FromKey, l.ToKey, l.LinkType, l.LinkID,
		); err != nil {
			return fmt.Errorf("insert requirement link %s->%s: %w", l.FromKey, l.ToKey, err)
		}
	}
	return tx.Commit()
}

// GetRequirementLinks returns the outbound Requirement->Requirement links for a
// single requirement (all link types).
func (r *Repository) GetRequirementLinks(profileID, requirementKey string) ([]ReqReqLink, error) {
	rows, err := r.db.Query(
		`SELECT from_requirement_key, to_requirement_key, link_type, link_id
		 FROM requirement_link
		 WHERE profile_id = ? AND from_requirement_key = ?
		 ORDER BY link_type, to_requirement_key`,
		profileID, requirementKey)
	if err != nil {
		return nil, fmt.Errorf("get requirement links: %w", err)
	}
	defer rows.Close()
	out := []ReqReqLink{}
	for rows.Next() {
		var l ReqReqLink
		if err := rows.Scan(&l.FromKey, &l.ToKey, &l.LinkType, &l.LinkID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetRequirementLinks replaces this requirement's outbound links of the given
// linkType to the supplied target keys, and queues a pending_change
// (entity_type "req_req_link_set", entity_key = fromKey, field = linkType) for
// commit. Setting the same set is a no-op. The before snapshot preserves each
// prior link's Jira id so a removed link can be deleted in Jira and restored
// on discard.
func (r *Repository) SetRequirementLinks(profileID, fromKey, linkType string, toKeys []string) error {
	newSet := uniqueSorted(toKeys)

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Read current links for this (from, linkType) pair.
	curRows, err := tx.Query(
		`SELECT to_requirement_key, link_id FROM requirement_link
		 WHERE profile_id = ? AND from_requirement_key = ? AND link_type = ?
		 ORDER BY to_requirement_key`,
		profileID, fromKey, linkType)
	if err != nil {
		return fmt.Errorf("read current req links: %w", err)
	}
	cur := []reqqLinkSnap{}
	curKeys := []string{}
	linkByKey := map[string]string{}
	for curRows.Next() {
		var k, id string
		if err := curRows.Scan(&k, &id); err != nil {
			_ = curRows.Close()
			return err
		}
		cur = append(cur, reqqLinkSnap{ToKey: k, LinkID: id})
		curKeys = append(curKeys, k)
		linkByKey[k] = id
	}
	_ = curRows.Close()
	if err := curRows.Err(); err != nil {
		return err
	}
	if equalOrder(curKeys, newSet) {
		return nil
	}

	// Replace the links in the local store.
	if _, err := tx.Exec(
		`DELETE FROM requirement_link
		 WHERE profile_id = ? AND from_requirement_key = ? AND link_type = ?`,
		profileID, fromKey, linkType,
	); err != nil {
		return fmt.Errorf("clear req links: %w", err)
	}
	for _, k := range newSet {
		if _, err := tx.Exec(
			`INSERT INTO requirement_link
			   (profile_id, from_requirement_key, to_requirement_key, link_type, link_id)
			 VALUES (?, ?, ?, ?, ?)`,
			profileID, fromKey, k, linkType, linkByKey[k],
		); err != nil {
			return fmt.Errorf("insert req link: %w", err)
		}
	}

	// Queue a pending change so the commit path pushes this to Jira.
	beforeJSON, _ := json.Marshal(cur)
	afterJSON, _ := json.Marshal(newSet)
	if err := upsertPendingChange(
		tx, profileID, entityReqReqLinkSet, fromKey, linkType,
		string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityReqReqLinkSet, fromKey,
		"set-req-links-local", linkType, string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	return tx.Commit()
}
