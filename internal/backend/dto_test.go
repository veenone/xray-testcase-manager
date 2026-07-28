package backend

import (
	"encoding/json"
	"testing"
)

// TestIssueLinkTypeJSONKeys pins the wire contract: the frontend (and the
// generated Wails model) read lowercase name/inward/outward. Without json
// tags Go would marshal capitalized field names, silently yielding undefined
// values in the UI and an empty link-type dropdown (#275 regression).
func TestIssueLinkTypeJSONKeys(t *testing.T) {
	raw, err := json.Marshal(IssueLinkType{Name: "Tests", Inward: "tested by", Outward: "tests"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"name", "inward", "outward"} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing lowercase JSON key %q in %s", key, raw)
		}
	}
	if got["name"] != "Tests" || got["inward"] != "tested by" || got["outward"] != "tests" {
		t.Errorf("values = %v, want Tests/tested by/tests", got)
	}
}
