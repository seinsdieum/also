package cli

import (
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

func TestCheckLockConflicts(t *testing.T) {
	plan := func(value string) *also.Plan {
		return &also.Plan{Edits: []also.ConfigEdit{{
			File: "opencode.json", Kind: "json-set", Path: []string{"default_agent"}, Value: []byte(value),
		}}}
	}
	if err := checkLockConflicts([]*also.Plan{plan(`"a"`), plan(`"b"`)}, false); err == nil {
		t.Fatal("different values in two lock entries must conflict")
	}
	if err := checkLockConflicts([]*also.Plan{plan(`"a"`), plan(`"a"`)}, false); err != nil {
		t.Fatalf("identical values should pass: %v", err)
	}
	if err := checkLockConflicts([]*also.Plan{plan(`"a"`), plan(`"b"`)}, true); err != nil {
		t.Fatalf("--force should allow the explicit overwrite: %v", err)
	}
}
