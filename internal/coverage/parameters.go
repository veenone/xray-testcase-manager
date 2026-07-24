package coverage

import (
	"fmt"

	"github.com/google/uuid"
)

// The parameter model is a three-level tree owned by a canonical requirement:
//
//	ParamGroup (Session, Mechanism, …)
//	  └─ Parameter (pMechanism, …)
//	       └─ ParamValue (CKM_RSA_PKCS, CKR_*, boundary, …)  ← coverage unit
//
// Error codes and boundary conditions are ParamValues distinguished by
// ValueKind, so the coverage math stays uniform.

// ParamModel is the full tree for one version of a canonical requirement.
type ParamModel struct {
	VersionID string       `json:"versionId"`
	Groups    []ParamGroup `json:"groups"`
}

// ParamGroup is a worksheet-tab-level grouping of parameters.
type ParamGroup struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	SortOrder  int         `json:"sortOrder"`
	Parameters []Parameter `json:"parameters"`
}

// Parameter is one input dimension within a group.
type Parameter struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	Description string       `json:"description"`
	SortOrder   int          `json:"sortOrder"`
	Values      []ParamValue `json:"values"`
}

// ParamValue is a distinct value — the unit coverage is measured over.
type ParamValue struct {
	ID         string `json:"id"`
	ValueLabel string `json:"valueLabel"`
	ValueKind  string `json:"valueKind"` // value | errorcode | boundary
	ErrorCode  string `json:"errorCode"`
	IsRequired bool   `json:"isRequired"`
	Notes      string `json:"notes"`
	SortOrder  int    `json:"sortOrder"`
}

// NodeEdit is the upsert payload for any node in the tree. Kind selects the
// table; ID empty means insert (a new uuid is returned), otherwise update.
// Only the fields relevant to Kind are read.
type NodeEdit struct {
	Kind string `json:"kind"` // group | parameter | value

	// group
	CanonicalID string `json:"canonicalId"`
	// group (Topic 2: groups root at a version, not the canonical)
	VersionID string `json:"versionId"`
	// parameter
	GroupID string `json:"groupId"`
	// value
	ParameterID string `json:"parameterId"`

	ID         string `json:"id"`
	Name       string `json:"name"`       // group/parameter name OR value label
	ParamKind  string `json:"paramKind"`  // parameter.kind
	ValueKind  string `json:"valueKind"`  // value.value_kind
	ErrorCode  string `json:"errorCode"`  // value.error_code
	IsRequired bool   `json:"isRequired"` // value.is_required
	Notes      string `json:"notes"`      // parameter.description OR value.notes
	SortOrder  int    `json:"sortOrder"`
}

// GetParamModel returns the full parameter tree for one version of a canonical
// requirement, ordered by sort_order at every level.
func (m *Module) GetParamModel(profileID, versionID string) (ParamModel, error) {
	model := ParamModel{VersionID: versionID, Groups: []ParamGroup{}}

	groupRows, err := m.db.Query(
		`SELECT id, name, sort_order FROM coverage_param_group
		  WHERE profile_id = ? AND version_id = ?
		  ORDER BY sort_order, name COLLATE NOCASE`,
		profileID, versionID)
	if err != nil {
		return model, fmt.Errorf("read groups: %w", err)
	}
	defer groupRows.Close()

	groupIdx := map[string]int{}
	for groupRows.Next() {
		var g ParamGroup
		if err := groupRows.Scan(&g.ID, &g.Name, &g.SortOrder); err != nil {
			return model, err
		}
		g.Parameters = []Parameter{}
		groupIdx[g.ID] = len(model.Groups)
		model.Groups = append(model.Groups, g)
	}
	if err := groupRows.Err(); err != nil {
		return model, err
	}
	if len(model.Groups) == 0 {
		return model, nil
	}

	paramRows, err := m.db.Query(
		`SELECT p.id, p.group_id, p.name, p.kind, p.description, p.sort_order
		   FROM coverage_parameter p
		   JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		  WHERE p.profile_id = ? AND g.version_id = ?
		  ORDER BY p.sort_order, p.name COLLATE NOCASE`,
		profileID, versionID)
	if err != nil {
		return model, fmt.Errorf("read parameters: %w", err)
	}
	defer paramRows.Close()

	paramLoc := map[string][2]int{} // parameter id -> {groupIndex, paramIndex}
	for paramRows.Next() {
		var p Parameter
		var groupID string
		if err := paramRows.Scan(&p.ID, &groupID, &p.Name, &p.Kind, &p.Description, &p.SortOrder); err != nil {
			return model, err
		}
		gi, ok := groupIdx[groupID]
		if !ok {
			continue
		}
		p.Values = []ParamValue{}
		model.Groups[gi].Parameters = append(model.Groups[gi].Parameters, p)
		paramLoc[p.ID] = [2]int{gi, len(model.Groups[gi].Parameters) - 1}
	}
	if err := paramRows.Err(); err != nil {
		return model, err
	}

	valueRows, err := m.db.Query(
		`SELECT v.id, v.parameter_id, v.value_label, v.value_kind, v.error_code,
		        v.is_required, v.notes, v.sort_order
		   FROM coverage_param_value v
		   JOIN coverage_parameter p   ON p.profile_id = v.profile_id AND p.id = v.parameter_id
		   JOIN coverage_param_group g ON g.profile_id = p.profile_id AND g.id = p.group_id
		  WHERE v.profile_id = ? AND g.version_id = ?
		  ORDER BY v.sort_order, v.value_label COLLATE NOCASE`,
		profileID, versionID)
	if err != nil {
		return model, fmt.Errorf("read values: %w", err)
	}
	defer valueRows.Close()

	for valueRows.Next() {
		var v ParamValue
		var paramID string
		var required int
		if err := valueRows.Scan(&v.ID, &paramID, &v.ValueLabel, &v.ValueKind,
			&v.ErrorCode, &required, &v.Notes, &v.SortOrder); err != nil {
			return model, err
		}
		v.IsRequired = required != 0
		loc, ok := paramLoc[paramID]
		if !ok {
			continue
		}
		grp := &model.Groups[loc[0]]
		grp.Parameters[loc[1]].Values = append(grp.Parameters[loc[1]].Values, v)
	}
	return model, valueRows.Err()
}

// UpsertNode inserts or updates a group, parameter, or value and returns its id.
func (m *Module) UpsertNode(profileID string, n NodeEdit) (string, error) {
	switch n.Kind {
	case "group":
		return m.upsertGroup(profileID, n)
	case "parameter":
		return m.upsertParameter(profileID, n)
	case "value":
		return m.upsertValue(profileID, n)
	default:
		return "", fmt.Errorf("unknown node kind %q", n.Kind)
	}
}

func (m *Module) upsertGroup(profileID string, n NodeEdit) (string, error) {
	if n.Name == "" {
		return "", fmt.Errorf("group name is required")
	}
	if n.ID == "" {
		if n.VersionID == "" {
			return "", fmt.Errorf("versionId is required for a new group")
		}
		id := uuid.NewString()
		_, err := m.db.Exec(
			`INSERT INTO coverage_param_group (profile_id, id, canonical_id, version_id, name, sort_order)
			 VALUES (?, ?, '', ?, ?, ?)`,
			profileID, id, n.VersionID, n.Name, n.SortOrder)
		return id, err
	}
	_, err := m.db.Exec(
		`UPDATE coverage_param_group SET name = ?, sort_order = ?
		  WHERE profile_id = ? AND id = ?`,
		n.Name, n.SortOrder, profileID, n.ID)
	return n.ID, err
}

func (m *Module) upsertParameter(profileID string, n NodeEdit) (string, error) {
	if n.Name == "" {
		return "", fmt.Errorf("parameter name is required")
	}
	kind := n.ParamKind
	if kind == "" {
		kind = "value"
	}
	if n.ID == "" {
		if n.GroupID == "" {
			return "", fmt.Errorf("groupId is required for a new parameter")
		}
		id := uuid.NewString()
		_, err := m.db.Exec(
			`INSERT INTO coverage_parameter (profile_id, id, group_id, name, kind, description, sort_order)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			profileID, id, n.GroupID, n.Name, kind, n.Notes, n.SortOrder)
		return id, err
	}
	_, err := m.db.Exec(
		`UPDATE coverage_parameter SET name = ?, kind = ?, description = ?, sort_order = ?
		  WHERE profile_id = ? AND id = ?`,
		n.Name, kind, n.Notes, n.SortOrder, profileID, n.ID)
	return n.ID, err
}

func (m *Module) upsertValue(profileID string, n NodeEdit) (string, error) {
	if n.Name == "" {
		return "", fmt.Errorf("value label is required")
	}
	kind := n.ValueKind
	if kind == "" {
		kind = "value"
	}
	required := 0
	if n.IsRequired {
		required = 1
	}
	if n.ID == "" {
		if n.ParameterID == "" {
			return "", fmt.Errorf("parameterId is required for a new value")
		}
		id := uuid.NewString()
		_, err := m.db.Exec(
			`INSERT INTO coverage_param_value
			   (profile_id, id, parameter_id, value_label, value_kind, error_code, is_required, notes, sort_order)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			profileID, id, n.ParameterID, n.Name, kind, n.ErrorCode, required, n.Notes, n.SortOrder)
		return id, err
	}
	_, err := m.db.Exec(
		`UPDATE coverage_param_value
		    SET value_label = ?, value_kind = ?, error_code = ?, is_required = ?, notes = ?, sort_order = ?
		  WHERE profile_id = ? AND id = ?`,
		n.Name, kind, n.ErrorCode, required, n.Notes, n.SortOrder, profileID, n.ID)
	return n.ID, err
}

// DeleteNode removes a group, parameter, or value and everything beneath it
// (cascading down to value→test mappings), in one transaction.
func (m *Module) DeleteNode(profileID, kind, id string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	switch kind {
	case "value":
		if _, err := tx.Exec(`DELETE FROM coverage_value_test WHERE profile_id = ? AND value_id = ?`, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_param_value WHERE profile_id = ? AND id = ?`, profileID, id); err != nil {
			return err
		}
	case "parameter":
		if _, err := tx.Exec(
			`DELETE FROM coverage_value_test WHERE profile_id = ? AND value_id IN (
			   SELECT id FROM coverage_param_value WHERE profile_id = ? AND parameter_id = ?)`,
			profileID, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_param_value WHERE profile_id = ? AND parameter_id = ?`, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_parameter WHERE profile_id = ? AND id = ?`, profileID, id); err != nil {
			return err
		}
	case "group":
		if _, err := tx.Exec(
			`DELETE FROM coverage_value_test WHERE profile_id = ? AND value_id IN (
			   SELECT v.id FROM coverage_param_value v
			   JOIN coverage_parameter p ON p.profile_id = v.profile_id AND p.id = v.parameter_id
			   WHERE p.profile_id = ? AND p.group_id = ?)`,
			profileID, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`DELETE FROM coverage_param_value WHERE profile_id = ? AND parameter_id IN (
			   SELECT id FROM coverage_parameter WHERE profile_id = ? AND group_id = ?)`,
			profileID, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_parameter WHERE profile_id = ? AND group_id = ?`, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_group_publication WHERE profile_id = ? AND group_id = ?`, profileID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM coverage_param_group WHERE profile_id = ? AND id = ?`, profileID, id); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown node kind %q", kind)
	}
	return tx.Commit()
}
