package testrepo

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// encodeEnvironments serialises a set of Test Environment names into the stored
// JSON array string, deduped and sorted for a stable comparison. An empty set
// stores "" (none) rather than "[]" so the column default and "no environments"
// coincide.
func encodeEnvironments(envs []string) string {
	clean := uniqueSorted(envs)
	if len(clean) == 0 {
		return ""
	}
	b, _ := json.Marshal(clean)
	return string(b)
}

// decodeEnvironments parses the stored JSON array back into a slice, returning
// an empty slice for "" / malformed input.
func decodeEnvironments(stored string) []string {
	if stored == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(stored), &out); err != nil {
		return []string{}
	}
	return out
}

// environmentFilterPattern builds a LIKE pattern that matches a single
// environment within the stored JSON array. The stored form contains each name
// double-quoted (e.g. `["Staging","Chrome"]`), so matching the JSON-quoted
// token `"Staging"` avoids substring collisions like "Prod" vs "Production"
// (the latter is stored as `"Production"`, which does not contain `"Prod"`).
func environmentFilterPattern(env string) string {
	quoted, _ := json.Marshal(env) // wraps in double-quotes, escaping as needed
	return "%" + string(quoted) + "%"
}

// ContainerEnvironments returns the Test Environments currently assigned to a
// container, or an empty slice if none / unknown.
func (r *Repository) ContainerEnvironments(profileID, containerKey string) ([]string, error) {
	var stored string
	err := r.db.QueryRow(
		`SELECT environments FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, containerKey,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("container %s not found", containerKey)
	}
	if err != nil {
		return nil, fmt.Errorf("read container environments: %w", err)
	}
	return decodeEnvironments(stored), nil
}

// SetContainerEnvironments replaces a Test Execution's Test Environments with the
// given set and queues the change for commit. The whole desired set is stored as
// one coalesced pending row (before / after JSON arrays) under entity_type
// container_env, mirroring SetTestPreconditions, so reverting to the original set
// drops the row. No-op when the set is unchanged. The commit engine pushes it as
// a Test Environments custom-field update on the execution issue.
func (r *Repository) SetContainerEnvironments(profileID, containerKey string, envs []string) error {
	newSet := uniqueSorted(envs)

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var stored string
	err = tx.QueryRow(
		`SELECT environments FROM test_container WHERE profile_id = ? AND jira_key = ?`,
		profileID, containerKey,
	).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("container %s not found", containerKey)
	}
	if err != nil {
		return fmt.Errorf("read container: %w", err)
	}
	currentSet := decodeEnvironments(stored)
	if equalOrder(currentSet, newSet) {
		return nil
	}

	if _, err := tx.Exec(
		`UPDATE test_container SET environments = ? WHERE profile_id = ? AND jira_key = ?`,
		encodeEnvironments(newSet), profileID, containerKey,
	); err != nil {
		return fmt.Errorf("update container environments: %w", err)
	}

	beforeJSON, err := json.Marshal(currentSet)
	if err != nil {
		return fmt.Errorf("marshal current environments: %w", err)
	}
	afterJSON, err := json.Marshal(newSet)
	if err != nil {
		return fmt.Errorf("marshal new environments: %w", err)
	}
	// Containers carry no cached updated_at watermark, so commit is best-effort
	// last-writer-wins (base_version empty), like a precondition edit.
	if err := upsertPendingChange(
		tx, profileID, entityContainerEnv, containerKey, "environments",
		string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	if err := writeAudit(
		tx, profileID, entityContainerEnv, containerKey,
		"set-environments-local", "environments",
		string(beforeJSON), string(afterJSON), "",
	); err != nil {
		return err
	}
	return tx.Commit()
}

// BulkEditContainers applies a Test Environments operation across a batch of
// containers, computing each container's new set and queuing it via
// SetContainerEnvironments. Operations:
//
//   - "set_env":    replace the environments with op.Value (a single name)
//   - "add_env":    add op.Value if not already present
//   - "remove_env": remove op.Value if present
//
// Returns the standard BulkEditResult (succeeded/failed per container key).
func (r *Repository) BulkEditContainers(profileID string, containerKeys []string, op BulkEdit) (BulkEditResult, error) {
	result := BulkEditResult{Succeeded: []string{}, Failed: []BulkFailure{}}
	for _, key := range containerKeys {
		current, err := r.ContainerEnvironments(profileID, key)
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		newSet, err := applyEnvOperation(op, current)
		if err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		if err := r.SetContainerEnvironments(profileID, key, newSet); err != nil {
			result.Failed = append(result.Failed, BulkFailure{TestKey: key, Error: err.Error()})
			continue
		}
		result.Succeeded = append(result.Succeeded, key)
	}
	return result, nil
}

// applyEnvOperation computes the new environment set for one container given the
// current set and the bulk operation. It does not write anything.
func applyEnvOperation(op BulkEdit, current []string) ([]string, error) {
	switch op.Operation {
	case "set_env":
		if op.Value == "" {
			return []string{}, nil
		}
		return []string{op.Value}, nil
	case "add_env":
		if op.Value == "" {
			return nil, fmt.Errorf("an environment value is required")
		}
		return uniqueSorted(append(append([]string{}, current...), op.Value)), nil
	case "remove_env":
		if op.Value == "" {
			return nil, fmt.Errorf("an environment value is required")
		}
		out := make([]string, 0, len(current))
		for _, e := range current {
			if e != op.Value {
				out = append(out, e)
			}
		}
		sort.Strings(out)
		return out, nil
	}
	return nil, fmt.Errorf("unknown container operation %q", op.Operation)
}
