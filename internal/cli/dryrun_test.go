package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

const dryRunTail = "no changes written; re-run without --dry-run to apply"

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupNamespace(t *testing.T) string {
	t.Helper()
	tmp := setupHome(t)
	remote := setupRemote(t)
	if err := runCLI("setup", "--yes", "--harness", "claude", "--namespace", "personal="+remote, "--default-namespace", "personal"); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// publishDemoPackage sets up a namespace, publishes a two-skill package named
// demo, and returns the temp root. setupHome has already chdir'd into it.
func publishDemoPackage(t *testing.T) string {
	t.Helper()
	tmp := setupNamespace(t)
	bundle := filepath.Join(tmp, "bundle")
	writeTestFile(t, filepath.Join(bundle, "also.json"),
		`{"schema":"also.infra/v1","name":"demo","harness":"claude"}`)
	writeTestFile(t, filepath.Join(bundle, "CLAUDE.md"), "rules\n")
	writeTestFile(t, filepath.Join(bundle, ".mcp.json"),
		`{"mcpServers":{"demo":{"command":"demo-server"}}}`)
	writeTestFile(t, filepath.Join(bundle, "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: d\n---\nreview body\n")
	writeTestFile(t, filepath.Join(bundle, "skills", "deploy", "SKILL.md"),
		"---\nname: deploy\ndescription: d\n---\ndeploy body\n")
	if err := runCLI("push", bundle); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func newGitProject(t *testing.T, root, name string) string {
	t.Helper()
	project := filepath.Join(root, name)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", project).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return project
}

func enterDir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// homeStatePaths are the per-user files a dry run promises not to touch.
func homeStatePaths() []string {
	home := os.Getenv("ALSO_HOME")
	return []string{
		filepath.Join(home, "state.json"),
		filepath.Join(home, "backups"),
		filepath.Join(home, "config-origins"),
	}
}

// snapshotTree records path, mode and content under every root. A missing root
// is recorded as empty.
func snapshotTree(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			key := root + "\x00" + rel
			if info.IsDir() {
				snap[key] = "dir"
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			snap[key] = fmt.Sprintf("%o:%x", info.Mode().Perm(), data)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("snapshot %s: %v", root, err)
		}
	}
	return snap
}

func assertSnapshotEqual(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	for key, want := range before {
		got, ok := after[key]
		if !ok {
			t.Fatalf("%s lost %s", label, key)
		}
		if got != want {
			t.Fatalf("%s changed %s: before %q, after %q", label, key, want, got)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			t.Fatalf("%s gained %s", label, key)
		}
	}
}

func firstLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

func TestInstallDryRunWritesNothing(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	enterDir(t, project)
	roots := append([]string{project}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err != nil {
		t.Fatalf("install --dry-run: %v\n%s", err, out)
	}
	if got := firstLine(out); !strings.HasPrefix(got, "would install demo@") {
		t.Fatalf("first line = %q, want a would-install summary\n%s", got, out)
	}
	if !strings.Contains(out, "files: 2") {
		t.Fatalf("dry run did not list the two files:\n%s", out)
	}
	if strings.Count(out, "SKILL.md") != 2 {
		t.Fatalf("dry run did not name both skill files:\n%s", out)
	}
	if !strings.Contains(out, "instructions:") {
		t.Fatalf("dry run did not report the managed instructions block:\n%s", out)
	}
	if !strings.Contains(out, "config changes: 1") || !strings.Contains(out, ".mcp.json") {
		t.Fatalf("dry run did not list the config change:\n%s", out)
	}
	if got := lastLine(out); got != dryRunTail {
		t.Fatalf("last line = %q, want %q", got, dryRunTail)
	}
	if _, statErr := os.Stat(filepath.Join(project, "also.lock")); !os.IsNotExist(statErr) {
		t.Fatal("dry run created also.lock in a fresh project")
	}
	if _, statErr := os.Stat(filepath.Join(project, ".mcp.json")); !os.IsNotExist(statErr) {
		t.Fatal("dry run wrote the config change")
	}
	assertSnapshotEqual(t, "dry run", before, snapshotTree(t, roots...))
}

func TestInstallDryRunJSONAndPreviewParity(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	enterDir(t, project)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("install --json --dry-run: %v\n%s", err, out)
	}
	preview, ok := decodeJSON(t, out).(map[string]any)
	if !ok {
		t.Fatalf("dry-run JSON is not an object: %s", out)
	}
	if preview["dry_run"] != true {
		t.Fatalf("dry_run = %v, want true", preview["dry_run"])
	}
	rawPaths, ok := preview["paths"].([]any)
	if !ok || len(rawPaths) == 0 {
		t.Fatalf("paths = %v, want a non-empty array", preview["paths"])
	}
	files, ok := preview["files"].(float64)
	if !ok || int(files) != len(rawPaths) {
		t.Fatalf("files = %v, want %d (len(paths))", preview["files"], len(rawPaths))
	}
	wantWritten := map[string]bool{}
	canonProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	relToProject := func(path string) string {
		t.Helper()
		// Preview paths are canonical absolute paths; compare project-relative
		// so /var and /private/var spellings of the same temp tree still match.
		rel, rerr := filepath.Rel(canonProject, path)
		if rerr != nil {
			t.Fatalf("path %s is not under %s: %v", path, canonProject, rerr)
		}
		return rel
	}
	for _, raw := range rawPaths {
		path, ok := raw.(string)
		if !ok {
			t.Fatalf("path entry is not a string: %v", raw)
		}
		wantWritten[relToProject(path)] = true
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("dry run created %s", path)
		}
	}
	configFiles := map[string]bool{}
	rawConfigs, ok := preview["config_changes"].([]any)
	if !ok || len(rawConfigs) == 0 {
		t.Fatalf("config_changes = %v, want the MCP edit", preview["config_changes"])
	}
	for _, raw := range rawConfigs {
		change, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("config change is not an object: %v", raw)
		}
		file, ok := change["file"].(string)
		if !ok {
			t.Fatalf("config change has no file: %v", change)
		}
		configFiles[relToProject(file)] = true
		if _, statErr := os.Stat(file); !os.IsNotExist(statErr) {
			t.Fatalf("dry run wrote config file %s", file)
		}
	}
	if len(configFiles) == 0 {
		t.Fatal("fixture produced no config change to verify")
	}

	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatalf("real install after dry run: %v", err)
	}
	// Everything the real run writes through the plan must be in the preview.
	// also.lock, the managed .gitignore block, the instruction file and config
	// edits are reported separately and are not plan paths.
	written := map[string]bool{}
	instruction := filepath.Join(project, "CLAUDE.md")
	err = filepath.Walk(project, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" && path != project {
				return filepath.SkipDir
			}
			return nil
		}
		switch path {
		case filepath.Join(project, "also.lock"), filepath.Join(project, ".gitignore"), instruction:
			return nil
		}
		rel, rerr := filepath.Rel(project, path)
		if rerr != nil {
			return rerr
		}
		if configFiles[rel] {
			return nil
		}
		written[rel] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(written, wantWritten) {
		t.Fatalf("preview paths = %v, real install wrote %v", wantWritten, written)
	}
}

func TestInstallDryRunJSONReportsRemovals(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	enterDir(t, project)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	roots := append([]string{project}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--except", "skills:review", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("install --except --json --dry-run: %v\n%s", err, out)
	}
	m, ok := decodeJSON(t, out).(map[string]any)
	if !ok {
		t.Fatalf("dry-run JSON is not an object: %s", out)
	}
	if m["dry_run"] != true {
		t.Fatalf("dry_run = %v, want true", m["dry_run"])
	}
	rawPaths, ok := m["paths"].([]any)
	if !ok || len(rawPaths) == 0 {
		t.Fatalf("paths = %v, want the remaining skill", m["paths"])
	}
	rawRemoved, ok := m["removed"].([]any)
	if !ok || len(rawRemoved) == 0 {
		t.Fatalf("removed = %v, want the excluded skill", m["removed"])
	}
	joined := fmt.Sprint(rawRemoved)
	if !strings.Contains(joined, "review") {
		t.Fatalf("removed = %v, want the review skill", rawRemoved)
	}
	if files, ok := m["files"].(float64); !ok || int(files) != len(rawPaths) {
		t.Fatalf("files = %v, want %d (len(paths))", m["files"], len(rawPaths))
	}
	if _, statErr := os.Stat(filepath.Join(project, ".claude", "skills", "review", "SKILL.md")); statErr != nil {
		t.Fatalf("dry run removed a file: %v", statErr)
	}
	assertSnapshotEqual(t, "dry run with removals", before, snapshotTree(t, roots...))
}

func TestInstallDryRunConflictsLikeRealRun(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	enterDir(t, project)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(project, ".claude", "skills", "review", "SKILL.md")
	writeTestFile(t, managed, "---\nname: review\ndescription: d\n---\nlocally edited\n")
	roots := append([]string{project}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	_, realErr := runCLICapture(t, "install", "demo", "for", "claude")
	if realErr == nil || !strings.Contains(realErr.Error(), "was modified since it was installed; use --force") {
		t.Fatalf("real install error = %v, want the modified-file conflict", realErr)
	}
	_, dryErr := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if dryErr == nil || dryErr.Error() != realErr.Error() {
		t.Fatalf("dry-run error = %v, want the same error as the real run: %v", dryErr, realErr)
	}
	assertSnapshotEqual(t, "conflicting dry run", before, snapshotTree(t, roots...))

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--force", "--dry-run")
	if err != nil {
		t.Fatalf("--force --dry-run: %v\n%s", err, out)
	}
	if got := firstLine(out); !strings.HasPrefix(got, "would install demo@") {
		t.Fatalf("first line = %q, want a would-install summary\n%s", got, out)
	}
	assertSnapshotEqual(t, "--force dry run", before, snapshotTree(t, roots...))
}

func TestInstallFrozenDryRunWritesNothing(t *testing.T) {
	tmp := publishDemoPackage(t)
	source := newGitProject(t, tmp, "source")
	enterDir(t, source)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	lockData, err := os.ReadFile(filepath.Join(source, "also.lock"))
	if err != nil {
		t.Fatal(err)
	}
	target := newGitProject(t, tmp, "target")
	writeTestFile(t, filepath.Join(target, "also.lock"), string(lockData))
	enterDir(t, target)
	roots := append([]string{target}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "--frozen", "--dry-run")
	if err != nil {
		t.Fatalf("install --frozen --dry-run: %v\n%s", err, out)
	}
	if got := firstLine(out); got != "would install 1 package(s) from also.lock" {
		t.Fatalf("first line = %q", got)
	}
	if got := lastLine(out); got != dryRunTail {
		t.Fatalf("last line = %q, want %q", got, dryRunTail)
	}
	if _, statErr := os.Stat(filepath.Join(target, ".claude")); !os.IsNotExist(statErr) {
		t.Fatal("frozen dry run wrote harness files")
	}
	assertSnapshotEqual(t, "frozen dry run", before, snapshotTree(t, roots...))

	out, err = runCLICapture(t, "install", "--frozen", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("install --frozen --json --dry-run: %v\n%s", err, out)
	}
	entries, ok := decodeJSON(t, out).([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("frozen dry-run JSON = %s, want one entry", out)
	}
	entry, ok := entries[0].(map[string]any)
	if !ok || entry["dry_run"] != true {
		t.Fatalf("frozen dry-run entry = %v, want dry_run true", entries[0])
	}
	if paths, ok := entry["paths"].([]any); !ok || len(paths) == 0 {
		t.Fatalf("frozen dry-run entry paths = %v, want a non-empty array", entry["paths"])
	}
	assertSnapshotEqual(t, "frozen dry run after JSON", before, snapshotTree(t, roots...))
}

func TestRequirePolicyMissingPolicy(t *testing.T) {
	tmp := publishDemoPackage(t)

	// A real install creates a lock to replay for the frozen case.
	source := newGitProject(t, tmp, "source")
	enterDir(t, source)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	lockData, err := os.ReadFile(filepath.Join(source, "also.lock"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		prepare func(t *testing.T) string
		args    []string
	}{
		{
			name: "install",
			prepare: func(t *testing.T) string {
				return newGitProject(t, tmp, "install-project")
			},
			args: []string{"install", "demo", "for", "claude", "--require-policy"},
		},
		{
			name: "frozen install",
			prepare: func(t *testing.T) string {
				project := newGitProject(t, tmp, "frozen-project")
				writeTestFile(t, filepath.Join(project, "also.lock"), string(lockData))
				return project
			},
			args: []string{"install", "--frozen", "--require-policy"},
		},
		{
			name: "upgrade check",
			prepare: func(t *testing.T) string {
				return newGitProject(t, tmp, "upgrade-project")
			},
			args: []string{"upgrade", "--check", "--require-policy"},
		},
		{
			name: "audit",
			prepare: func(t *testing.T) string {
				return newGitProject(t, tmp, "audit-project")
			},
			args: []string{"audit", "--require-policy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			project := tc.prepare(t)
			enterDir(t, project)
			roots := append([]string{project}, homeStatePaths()...)
			before := snapshotTree(t, roots...)

			_, runErr := runCLICapture(t, tc.args...)
			if runErr == nil || !strings.HasPrefix(runErr.Error(), "policy: no also.policy.yml applies to") {
				t.Fatalf("error = %v, want the policy gate prefix", runErr)
			}
			assertSnapshotEqual(t, tc.name, before, snapshotTree(t, roots...))
		})
	}
}

func TestAuditPolicyJSONRequirePolicyExitsOne(t *testing.T) {
	tmp := setupNamespace(t)
	project := newGitProject(t, tmp, "project")
	enterDir(t, project)

	out, err := runCLICapture(t, "audit", "--policy", "--json", "--require-policy")
	if err == nil || !strings.HasPrefix(err.Error(), "policy: no also.policy.yml applies to") {
		t.Fatalf("error = %v, want the policy gate prefix\n%s", err, out)
	}
	got, ok := decodeJSON(t, out).(map[string]any)
	if !ok || got["exists"] != false {
		t.Fatalf("policy JSON = %s, want exists false", out)
	}

	out, err = runCLICapture(t, "audit", "--policy", "--json")
	if err != nil {
		t.Fatalf("audit --policy --json without --require-policy: %v\n%s", err, out)
	}
	got, ok = decodeJSON(t, out).(map[string]any)
	if !ok || got["exists"] != false {
		t.Fatalf("policy JSON = %s, want exists false", out)
	}
}

func TestRequirePolicyPresentPolicy(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	writeTestFile(t, filepath.Join(project, "also.policy.yml"), "schema: also.policy/v1\n")
	enterDir(t, project)
	roots := append([]string{project}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--require-policy", "--dry-run")
	if err != nil {
		t.Fatalf("install --require-policy with a policy: %v\n%s", err, out)
	}
	if got := firstLine(out); !strings.HasPrefix(got, "would install demo@") {
		t.Fatalf("first line = %q, want a would-install summary\n%s", got, out)
	}
	assertSnapshotEqual(t, "policy-gated dry run", before, snapshotTree(t, roots...))

	if err := runCLI("install", "demo", "for", "claude", "--require-policy"); err != nil {
		t.Fatalf("install --require-policy with a policy: %v", err)
	}
	if _, err := runCLICapture(t, "upgrade", "--check", "--require-policy"); err != nil {
		t.Fatalf("upgrade --check --require-policy with a policy: %v", err)
	}
	if _, err := runCLICapture(t, "audit", "--require-policy"); err != nil {
		t.Fatalf("audit --require-policy with a policy: %v", err)
	}
	out, err = runCLICapture(t, "audit", "--policy", "--json", "--require-policy")
	if err != nil {
		t.Fatalf("audit --policy --json --require-policy with a policy: %v\n%s", err, out)
	}
	got, ok := decodeJSON(t, out).(map[string]any)
	if !ok || got["exists"] != true {
		t.Fatalf("policy JSON = %s, want exists true", out)
	}
}

func TestGitignorePreviewMatchesRealWrite(t *testing.T) {
	tmp := publishDemoPackage(t)
	canonical := managedGitignore(t, "claude")
	malformed := strings.Replace(canonical, "# also:begin infra\n", "# also:begin infra\n\n", 1)
	if malformed == canonical {
		t.Fatal("fixture has no infra block to damage")
	}
	malformed = strings.TrimSuffix(malformed, "\n")
	if strings.HasSuffix(malformed, "\n") {
		t.Fatal("fixture still ends with a newline")
	}

	changed := newGitProject(t, tmp, "gitignore-changed")
	writeTestFile(t, filepath.Join(changed, ".gitignore"), malformed)
	enterDir(t, changed)
	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err != nil {
		t.Fatalf("dry run with a damaged .gitignore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "gitignore: would update") {
		t.Fatalf("dry run did not preview the .gitignore rewrite:\n%s", out)
	}
	if got := readTestFile(t, filepath.Join(changed, ".gitignore")); got != malformed {
		t.Fatalf("dry run changed .gitignore:\n%s", got)
	}
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatalf("real install after the preview: %v", err)
	}
	if got := readTestFile(t, filepath.Join(changed, ".gitignore")); got != canonical {
		t.Fatalf("real install did not rewrite .gitignore in canonical form\ngot:\n%s\nwant:\n%s", got, canonical)
	}

	canonicalProject := newGitProject(t, tmp, "gitignore-canonical")
	writeTestFile(t, filepath.Join(canonicalProject, ".gitignore"), canonical)
	enterDir(t, canonicalProject)
	out, err = runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err != nil {
		t.Fatalf("dry run with a canonical .gitignore: %v\n%s", err, out)
	}
	if strings.Contains(out, "gitignore:") {
		t.Fatalf("dry run reported a change for a canonical .gitignore:\n%s", out)
	}
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatalf("real install with a canonical .gitignore: %v", err)
	}
	if got := readTestFile(t, filepath.Join(canonicalProject, ".gitignore")); got != canonical {
		t.Fatalf("real install rewrote a canonical .gitignore\ngot:\n%s\nwant:\n%s", got, canonical)
	}
}

// managedGitignore is the canonical two-block file ensureManagedIgnores writes
// for one harness.
func managedGitignore(t *testing.T, harness string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".gitignore")
	if _, err := also.UpdateGitIgnoreAt(path, also.IgnoreInfra, true, also.InfraIgnoreEntries([]string{harness})); err != nil {
		t.Fatal(err)
	}
	if _, err := also.UpdateGitIgnoreAt(path, also.IgnoreAlso, true, also.AlsoIgnoreEntries()); err != nil {
		t.Fatal(err)
	}
	return readTestFile(t, path)
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFrozenDryRunReportsEntryDetails(t *testing.T) {
	tmp := publishDemoPackage(t)
	source := newGitProject(t, tmp, "source")
	enterDir(t, source)
	if err := runCLI("install", "demo", "for", "claude"); err != nil {
		t.Fatal(err)
	}
	lock := readTestFile(t, filepath.Join(source, "also.lock"))

	target := newGitProject(t, tmp, "target")
	writeTestFile(t, filepath.Join(target, "also.lock"), lock)
	enterDir(t, target)
	out, err := runCLICapture(t, "install", "--frozen", "--dry-run")
	if err != nil {
		t.Fatalf("frozen dry run: %v\n%s", err, out)
	}
	if got := firstLine(out); got != "would install 1 package(s) from also.lock" {
		t.Fatalf("first line = %q", got)
	}
	for _, want := range []string{
		"files: 2", "skills/review/SKILL.md", "skills/deploy/SKILL.md",
		"instructions:", "config changes: 1", ".mcp.json", "gitignore: would update",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("frozen dry run is missing %q:\n%s", want, out)
		}
	}
	if got := lastLine(out); got != dryRunTail {
		t.Fatalf("last line = %q, want %q", got, dryRunTail)
	}
	if _, statErr := os.Stat(filepath.Join(target, ".gitignore")); !os.IsNotExist(statErr) {
		t.Fatal("frozen dry run wrote .gitignore")
	}

	out, err = runCLICapture(t, "install", "--frozen", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("frozen JSON dry run: %v\n%s", err, out)
	}
	entries, ok := decodeJSON(t, out).([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("frozen dry-run JSON = %s, want one entry", out)
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("entry is not an object: %v", entries[0])
	}
	if entry["gitignore_change"] != true {
		t.Fatalf("gitignore_change = %v, want true", entry["gitignore_change"])
	}
	if _, ok := entry["lock_change"]; ok {
		t.Fatalf("frozen entry reported lock_change: %v", entry["lock_change"])
	}

	canonicalTarget := newGitProject(t, tmp, "target-canonical")
	writeTestFile(t, filepath.Join(canonicalTarget, "also.lock"), lock)
	writeTestFile(t, filepath.Join(canonicalTarget, ".gitignore"), managedGitignore(t, "claude"))
	enterDir(t, canonicalTarget)
	out, err = runCLICapture(t, "install", "--frozen", "--dry-run")
	if err != nil {
		t.Fatalf("frozen dry run with a canonical .gitignore: %v\n%s", err, out)
	}
	if strings.Contains(out, "gitignore:") {
		t.Fatalf("frozen dry run reported a canonical .gitignore change:\n%s", out)
	}
	out, err = runCLICapture(t, "install", "--frozen", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("frozen JSON dry run with a canonical .gitignore: %v\n%s", err, out)
	}
	entries = decodeJSON(t, out).([]any)
	entry = entries[0].(map[string]any)
	if _, ok := entry["gitignore_change"]; ok {
		t.Fatalf("canonical frozen entry reported gitignore_change: %v", entry["gitignore_change"])
	}
	if _, ok := entry["lock_change"]; ok {
		t.Fatalf("frozen entry reported lock_change: %v", entry["lock_change"])
	}
}

func commandHelp(t *testing.T, command string) string {
	t.Helper()
	root := NewRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{command, "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("%s --help: %v", command, err)
	}
	return buf.String()
}

func TestNewFlagsAreDocumentedAndCommandsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		command string
		flags   []string
	}{
		{"install", []string{"--dry-run", "--require-policy"}},
		{"upgrade", []string{"--require-policy"}},
		{"audit", []string{"--require-policy"}},
	} {
		help := commandHelp(t, tc.command)
		for _, flag := range tc.flags {
			if !strings.Contains(help, flag) {
				t.Fatalf("%s --help does not document %s:\n%s", tc.command, flag, help)
			}
		}
	}

	installHelp := commandHelp(t, "install")
	if strings.Contains(installHelp, "without writing anything") {
		t.Fatalf("install --help still claims a dry run writes nothing:\n%s", installHelp)
	}
	for _, want := range []string{"project files", "also.lock", "state", "backup"} {
		if !strings.Contains(installHelp, want) {
			t.Fatalf("install --help does not name %q in the dry-run contract:\n%s", want, installHelp)
		}
	}

	var visible []string
	for _, c := range NewRoot().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		visible = append(visible, c.Name())
	}
	sort.Strings(visible)
	want := []string{
		"adopt", "audit", "connect", "doctor", "init", "install", "list",
		"push", "setup", "show", "tag", "uninstall", "upgrade",
	}
	if !reflect.DeepEqual(visible, want) {
		t.Fatalf("visible commands = %v, want %v", visible, want)
	}
}

func TestDryRunReportsSymlinkedGitignoreLikeRealInstall(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	target := filepath.Join(tmp, "gitignore-target")
	writeTestFile(t, target, "user ignore\n")
	link := filepath.Join(project, ".gitignore")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks are unavailable on this platform: %v", err)
	}
	enterDir(t, project)
	roots := append([]string{project, target}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "refusing to replace symlink") {
		t.Fatalf("dry-run error = %v, want the symlink refusal\n%s", err, out)
	}
	if strings.Contains(out, "would install") || strings.Contains(out, dryRunTail) {
		t.Fatalf("dry run printed a success report despite the symlink:\n%s", out)
	}
	assertSnapshotEqual(t, "symlink dry run", before, snapshotTree(t, roots...))

	realOut, realErr := runCLICapture(t, "install", "demo", "for", "claude")
	if realErr == nil || realErr.Error() != err.Error() {
		t.Fatalf("real install error = %v, want the dry-run error %q", realErr, err)
	}
	if strings.Contains(realOut, "installed demo@") {
		t.Fatalf("real install printed a success report despite the symlink:\n%s", realOut)
	}
	if info, statErr := os.Lstat(link); statErr != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf(".gitignore is no longer a symlink: %v", statErr)
	}
	// The real path materializes the plan before it reaches .gitignore, so it
	// may leave partial writes. Record them instead of claiming otherwise.
	after := snapshotTree(t, roots...)
	label := func(key string) string { return strings.ReplaceAll(key, "\x00", "/") }
	for key, value := range after {
		if old, ok := before[key]; !ok {
			t.Logf("real install wrote before failing: %s", label(key))
		} else if old != value {
			t.Logf("real install changed before failing: %s", label(key))
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			t.Logf("real install removed before failing: %s", label(key))
		}
	}
}

func TestDryRunFailsWhenGitignoreIsDirectory(t *testing.T) {
	tmp := publishDemoPackage(t)
	project := newGitProject(t, tmp, "project")
	if err := os.Mkdir(filepath.Join(project, ".gitignore"), 0o755); err != nil {
		t.Fatal(err)
	}
	enterDir(t, project)
	roots := append([]string{project}, homeStatePaths()...)
	before := snapshotTree(t, roots...)

	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err == nil {
		t.Fatalf("dry run succeeded with a directory at .gitignore:\n%s", out)
	}
	if !strings.Contains(err.Error(), ".gitignore") {
		t.Fatalf("error = %v, want it to name .gitignore", err)
	}
	if strings.Contains(err.Error(), "refusing to replace symlink") {
		t.Fatalf("directory .gitignore was misreported as a symlink: %v", err)
	}
	if strings.Contains(out, "would install") || strings.Contains(out, dryRunTail) {
		t.Fatalf("dry run printed a success report despite the read error:\n%s", out)
	}
	assertSnapshotEqual(t, "directory .gitignore dry run", before, snapshotTree(t, roots...))
}

func TestDryRunJSONLockChange(t *testing.T) {
	t.Run("fresh then settled", func(t *testing.T) {
		installDemoProject(t)
		if got := dryRunInstallJSON(t)["lock_change"]; got != true {
			t.Fatalf("fresh dry-run lock_change = %v, want true", got)
		}
		if out := dryRunInstallHuman(t); !strings.Contains(out, "lock: would update") {
			t.Fatalf("fresh dry run did not preview the lock update:\n%s", out)
		}
		if err := runCLI("install", "demo", "for", "claude"); err != nil {
			t.Fatalf("real install: %v", err)
		}
		entry := dryRunInstallJSON(t)
		if got, ok := entry["lock_change"]; ok && got != false {
			t.Fatalf("settled dry-run lock_change = %v, want absent or false", got)
		}
		if out := dryRunInstallHuman(t); strings.Contains(out, "lock:") {
			t.Fatalf("settled dry run still previews a lock update:\n%s", out)
		}
	})

	t.Run("reversed packages", func(t *testing.T) {
		tmp, project := installDemoProject(t)
		if err := runCLI("install", "demo", "for", "claude"); err != nil {
			t.Fatal(err)
		}
		publishClaudePackage(t, tmp, "other")
		if err := runCLI("install", "other", "for", "claude"); err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(project, "also.lock")
		reverseLockPackages(t, lockPath)
		reversed := readTestFile(t, lockPath)

		if out := dryRunInstallHuman(t); !strings.Contains(out, "lock: would update") {
			t.Fatalf("dry run did not preview a hand-reversed lock:\n%s", out)
		}
		if got := dryRunInstallJSON(t)["lock_change"]; got != true {
			t.Fatalf("reversed lock_change = %v, want true", got)
		}
		if got := readTestFile(t, lockPath); got != reversed {
			t.Fatalf("dry run rewrote the reversed lock:\n%s", got)
		}

		if err := runCLI("install", "demo", "for", "claude"); err != nil {
			t.Fatalf("real install after the reversed preview: %v", err)
		}
		order := lockPackageNames(t, lockPath)
		if strings.Join(order, ",") != "demo,other" {
			t.Fatalf("real install wrote packages in %v, want the canonical demo,other", order)
		}
	})

	t.Run("missing schema", func(t *testing.T) {
		_, project := installDemoProject(t)
		if err := runCLI("install", "demo", "for", "claude"); err != nil {
			t.Fatal(err)
		}
		lockPath := filepath.Join(project, "also.lock")
		doc := readLockDoc(t, lockPath)
		delete(doc, "schema")
		writeLockDoc(t, lockPath, doc)
		withoutSchema := readTestFile(t, lockPath)

		if out := dryRunInstallHuman(t); !strings.Contains(out, "lock: would update") {
			t.Fatalf("dry run did not preview a lock without schema:\n%s", out)
		}
		if got := dryRunInstallJSON(t)["lock_change"]; got != true {
			t.Fatalf("schema-less lock_change = %v, want true", got)
		}
		if got := readTestFile(t, lockPath); got != withoutSchema {
			t.Fatalf("dry run rewrote the schema-less lock:\n%s", got)
		}

		if err := runCLI("install", "demo", "for", "claude"); err != nil {
			t.Fatalf("real install after the schema preview: %v", err)
		}
		restored := readLockDoc(t, lockPath)
		if restored["schema"] != "also.lock/v1" {
			t.Fatalf("real install did not restore the schema: %v", restored["schema"])
		}
		if got, ok := dryRunInstallJSON(t)["lock_change"]; ok && got != false {
			t.Fatalf("settled schema dry-run lock_change = %v, want absent or false", got)
		}
	})
}

func installDemoProject(t *testing.T) (tmp, project string) {
	t.Helper()
	tmp = publishDemoPackage(t)
	project = newGitProject(t, tmp, "project")
	enterDir(t, project)
	return tmp, project
}

func publishClaudePackage(t *testing.T, tmp, name string) {
	t.Helper()
	bundle := filepath.Join(tmp, "bundle-"+name)
	writeTestFile(t, filepath.Join(bundle, "also.json"),
		`{"schema":"also.infra/v1","name":"`+name+`","harness":"claude"}`)
	writeTestFile(t, filepath.Join(bundle, "CLAUDE.md"), name+"\n")
	writeTestFile(t, filepath.Join(bundle, "skills", name, "SKILL.md"),
		"---\nname: "+name+"\ndescription: d\n---\n"+name+"\n")
	if err := runCLI("push", bundle); err != nil {
		t.Fatal(err)
	}
}

func dryRunInstallJSON(t *testing.T) map[string]any {
	t.Helper()
	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--json", "--dry-run")
	if err != nil {
		t.Fatalf("install --json --dry-run: %v\n%s", err, out)
	}
	entry, ok := decodeJSON(t, out).(map[string]any)
	if !ok {
		t.Fatalf("dry-run JSON is not an object: %s", out)
	}
	return entry
}

func dryRunInstallHuman(t *testing.T) string {
	t.Helper()
	out, err := runCLICapture(t, "install", "demo", "for", "claude", "--dry-run")
	if err != nil {
		t.Fatalf("install --dry-run: %v\n%s", err, out)
	}
	return out
}

func readLockDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(readTestFile(t, path)), &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func writeLockDoc(t *testing.T, path string, doc map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func reverseLockPackages(t *testing.T, path string) {
	t.Helper()
	doc := readLockDoc(t, path)
	packages, ok := doc["packages"].([]any)
	if !ok || len(packages) < 2 {
		t.Fatalf("lock packages = %v, want at least two", doc["packages"])
	}
	for i, j := 0, len(packages)-1; i < j; i, j = i+1, j-1 {
		packages[i], packages[j] = packages[j], packages[i]
	}
	writeLockDoc(t, path, doc)
}

func lockPackageNames(t *testing.T, path string) []string {
	t.Helper()
	packages, ok := readLockDoc(t, path)["packages"].([]any)
	if !ok {
		t.Fatalf("%s has no packages array", path)
	}
	names := make([]string, 0, len(packages))
	for _, raw := range packages {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("lock entry is not an object: %v", raw)
		}
		name, _ := entry["package"].(string)
		names = append(names, name)
	}
	return names
}
