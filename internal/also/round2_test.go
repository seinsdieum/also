package also

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mcpEntry(t *testing.T, configPath, name string) map[string]any {
	t.Helper()
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := doc.RawAt("mcp", "servers", name)
	if !ok {
		t.Fatalf("%s has no mcp.servers.%s entry", configPath, name)
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		t.Fatalf("parse mcp.servers.%s: %v", name, err)
	}
	return entry
}

// An upgrade merges the new declaration into the value the user had before the
// first install, not into the value the previous package version left behind.
func TestUpgradeDropsOldPackageKeysKeepsUserKeys(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	project := publishOpenCodeMCP(t, cfg, tmp, "p",
		`{"mcp":{"servers":{"x":{"command":["x"],"env":{"A":"1"},"oldkey":"stale"}}}}`)
	configPath := filepath.Join(project, "opencode.json")
	writeFile(t, configPath, `{
  "theme": "user-theme",
  "mcp": {
    "servers": {
      "x": {"userkey": "keep"}
    }
  }
}`)

	installOpenCodeWithConfig(t, cfg, project, "p")
	v1 := mcpEntry(t, configPath, "x")
	for _, key := range []string{"command", "env", "oldkey", "userkey"} {
		if _, ok := v1[key]; !ok {
			t.Fatalf("v1 entry is missing %q: %v", key, v1)
		}
	}

	// Publish v2 under the same package name: the stale key disappears while
	// the pre-install user key stays.
	publishOpenCodeMCP(t, cfg, tmp, "p", `{"mcp":{"servers":{"x":{"command":["x"],"env":{"A":"1"}}}}}`)
	st, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	results, err := UpgradeInstallsInProject(cfg, st, project, []string{"p"}, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Err != nil || results[0].Status != "upgraded" {
		t.Fatalf("upgrade did not converge: %+v", results)
	}

	v2 := mcpEntry(t, configPath, "x")
	if _, ok := v2["oldkey"]; ok {
		t.Fatalf("upgrade kept a key only the previous package version declared: %v", v2)
	}
	if _, ok := v2["command"]; !ok {
		t.Fatalf("upgrade dropped a declared key: %v", v2)
	}
	if env, ok := v2["env"].(map[string]any); !ok || env["A"] != "1" {
		t.Fatalf("upgrade dropped the declared env key: %v", v2)
	}
	if v2["userkey"] != "keep" {
		t.Fatalf("upgrade dropped the user key: %v", v2)
	}
	doc, err := ParseJSONC(mustReadTestFile(t, configPath))
	if err != nil {
		t.Fatal(err)
	}
	if raw, ok := doc.RawAt("theme"); !ok || string(raw) != `"user-theme"` {
		t.Fatalf("upgrade changed an unrelated user key: %s", raw)
	}

	st, err = LoadState()
	if err != nil {
		t.Fatal(err)
	}
	installs := st.InstallsOf("p")
	if len(installs) != 1 {
		t.Fatalf("expected one install, got %d", len(installs))
	}
	if _, err := Uninstall(st, installs[0], false); err != nil {
		t.Fatal(err)
	}
	restored := mcpEntry(t, configPath, "x")
	if restored["userkey"] != "keep" {
		t.Fatalf("uninstall did not restore the user key: %v", restored)
	}
	if _, ok := restored["command"]; ok {
		t.Fatalf("uninstall kept the package key: %v", restored)
	}
}

// Appending an array element keeps the file's existing indentation unit and a
// reinstall reproduces the same bytes.
func TestInstallArrayIndentationFollowsConfig(t *testing.T) {
	cases := []struct {
		name string
		unit string
	}{
		{"two-space", "  "},
		{"four-space", "    "},
		{"tab", "\t"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp, _, _, cfg := setup(t)
			project := publishOpenCodeMCP(t, cfg, tmp, "p", `{"permissions":{"allow":["Bash(ls:*)"]}}`)
			configPath := filepath.Join(project, "opencode.json")
			src := "{\n" +
				tc.unit + `"permissions": {` + "\n" +
				tc.unit + tc.unit + `"allow": []` + "\n" +
				tc.unit + `}` + "\n" +
				"}\n"
			writeFile(t, configPath, src)

			installOpenCodeWithConfig(t, cfg, project, "p")
			first := string(mustReadTestFile(t, configPath))
			wantElement := tc.unit + tc.unit + tc.unit + `"Bash(ls:*)"`
			found := false
			for _, line := range strings.Split(first, "\n") {
				if strings.Contains(line, `"Bash(ls:*)"`) {
					if line != wantElement {
						t.Fatalf("array element used the wrong indentation: %q, want %q\n%s", line, wantElement, first)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("install did not append the managed array element:\n%s", first)
			}

			installOpenCodeWithConfig(t, cfg, project, "p")
			second := string(mustReadTestFile(t, configPath))
			if second != first {
				t.Fatalf("second install changed the config file\nfirst:\n%s\nsecond:\n%s", first, second)
			}
		})
	}
}

// RemoveEqual must match the exact element. A float64 comparison conflates
// integers one apart above 2^53, so the old code removed a user value instead
// of the managed one.
func TestJSONCRemoveEqualKeepsLargeIntegersExact(t *testing.T) {
	t.Run("managed element", func(t *testing.T) {
		// All three integers collapse to the same float64 (2^54).
		doc, err := ParseJSONC([]byte(`{"allow":[18014398509481984,18014398509481986]}`))
		if err != nil {
			t.Fatal(err)
		}
		out, err := doc.Append(json.Number("18014398509481985"), "allow")
		if err != nil {
			t.Fatal(err)
		}
		withManaged, err := ParseJSONC(out)
		if err != nil {
			t.Fatal(err)
		}
		out, found, err := withManaged.RemoveEqual(json.RawMessage(`18014398509481985`), "allow")
		if err != nil || !found {
			t.Fatalf("remove managed element: found=%v err=%v", found, err)
		}
		final, err := ParseJSONC(out)
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := final.RawAt("allow")
		if !ok {
			t.Fatalf("allow array disappeared: %s", out)
		}
		var got []json.RawMessage
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		values := make([]string, 0, len(got))
		for _, v := range got {
			values = append(values, string(v))
		}
		want := []string{"18014398509481984", "18014398509481986"}
		if strings.Join(values, ",") != strings.Join(want, ",") {
			t.Fatalf("array = %v, want both original integers in order %v", values, want)
		}
	})

	t.Run("suggested pair", func(t *testing.T) {
		doc, err := ParseJSONC([]byte(`[9007199254740993,9007199254740992]`))
		if err != nil {
			t.Fatal(err)
		}
		out, found, err := doc.RemoveEqual(json.RawMessage(`9007199254740992`))
		if err != nil || !found {
			t.Fatalf("remove element: found=%v err=%v", found, err)
		}
		got := string(out)
		if !strings.Contains(got, "9007199254740993") {
			t.Fatalf("removal dropped the wrong integer: %s", got)
		}
		if strings.Contains(got, "9007199254740992") {
			t.Fatalf("removal kept the requested integer: %s", got)
		}
	})
}

// LockWouldChange canonicalises a copy: previewing must not reorder the caller's
// in-memory lock, so later decisions still see what was loaded.
func TestLockWouldChangeDoesNotMutateCaller(t *testing.T) {
	dir := t.TempDir()
	a := LockEntry{Package: "a", Namespace: "ns", Version: "1", Harness: "claude"}
	b := LockEntry{Package: "b", Namespace: "ns", Version: "1", Harness: "claude"}
	// Write hand-reversed bytes without SaveLock, which would sort them.
	data, err := json.MarshalIndent(&Lockfile{Schema: SchemaLock, Packages: []LockEntry{b, a}}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LockFile), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	caller := &Lockfile{Schema: SchemaLock, Packages: []LockEntry{b, a}}

	changed, err := LockWouldChange(dir, caller, a)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("hand-reversed lock must preview a change")
	}
	if caller.Packages[0].Package != "b" || caller.Packages[1].Package != "a" {
		t.Fatalf("LockWouldChange reordered the caller's lock: %+v", caller.Packages)
	}
	if caller.Schema != SchemaLock {
		t.Fatalf("LockWouldChange changed the caller's schema: %q", caller.Schema)
	}
}
