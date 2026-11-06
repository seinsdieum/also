package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seinsdieum/also/internal/also"
)

// TestFourByFourMatrix round-trips instructions and a skill through every
// source/target harness pair using only local bare remotes.
func TestFourByFourMatrix(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ALSO_HOME", filepath.Join(tmp, "also"))

	remote := filepath.Join(tmp, "remote.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", remote).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	cfg := &also.Config{
		Schema: also.SchemaConfig, DefaultScope: "project",
		Namespaces: map[string]also.NamespaceConfig{"personal": {Remote: remote}},
	}
	repo, err := also.OpenSet("personal", cfg.Namespaces["personal"], false)
	if err != nil {
		t.Fatal(err)
	}

	harnesses := []string{"claude", "opencode", "codex", "qwen"}
	for _, source := range harnesses {
		bundle := filepath.Join(tmp, "src-"+source)
		writeBundle(t, bundle, source)
		if _, err := repo.Push(bundle, also.PushOptions{Name: "src-" + source, Harness: source}); err != nil {
			t.Fatalf("push %s: %v", source, err)
		}
	}

	for _, source := range harnesses {
		for _, target := range harnesses {
			t.Run(source+"-to-"+target, func(t *testing.T) {
				mid := filepath.Join(tmp, "mid-"+source+"-"+target)
				if err := os.MkdirAll(mid, 0o755); err != nil {
					t.Fatal(err)
				}
				if _, _, err := also.InstallPackage(cfg, also.InstallRequest{
					Package: "src-" + source, Harness: target, Scope: "project", Project: mid, WithConfig: true,
				}, "personal", ""); err != nil {
					t.Fatalf("install into %s: %v", target, err)
				}
				bundleDir := filepath.Join(tmp, "adopt-"+source+"-"+target)
				if _, err := also.ExtractBundle(also.AdoptOptions{
					Name: "rt-" + source + "-" + target, Project: mid, Harness: target, Dest: bundleDir,
				}); err != nil {
					t.Fatalf("adopt from %s: %v", target, err)
				}
				pkg := "rt-" + source + "-" + target
				if _, err := repo.Push(filepath.Join(bundleDir, pkg), also.PushOptions{Name: pkg, Harness: target}); err != nil {
					t.Fatalf("push %s: %v", pkg, err)
				}
				back := filepath.Join(tmp, "back-"+source+"-"+target)
				if err := os.MkdirAll(back, 0o755); err != nil {
					t.Fatal(err)
				}
				plan, _, err := also.InstallPackage(cfg, also.InstallRequest{
					Package: pkg, Harness: source, Scope: "project", Project: back, WithConfig: true,
				}, "personal", "")
				if err != nil {
					t.Fatalf("install back into %s: %v", source, err)
				}
				sourceH, ok := also.ResolveHarness(source)
				if !ok {
					t.Fatal("unknown harness " + source)
				}
				target := sourceH.TargetPath("project", back)
				instr, err := os.ReadFile(target.Instr)
				if err != nil {
					t.Fatalf("read instructions: %v", err)
				}
				want := "rules from " + source
				if !strings.Contains(string(instr), want) {
					t.Fatalf("instructions lost %q:\n%s", want, instr)
				}
				skill := filepath.Join(target.Skills, "review", "SKILL.md")
				data, err := os.ReadFile(skill)
				if err != nil {
					t.Fatalf("read skill: %v", err)
				}
				if !strings.Contains(string(data), "body from "+source) {
					t.Fatalf("skill lost its body:\n%s", data)
				}
				if plan.Install.Harness != source {
					t.Fatalf("installed into %s, want %s", plan.Install.Harness, source)
				}
			})
		}
	}
}

func writeBundle(t *testing.T, dir, harness string) {
	t.Helper()
	h, ok := also.ResolveHarness(harness)
	if !ok {
		t.Fatal("unknown harness " + harness)
	}
	for _, item := range []struct {
		rel  string
		data string
	}{
		{h.Bundle.Instr, "rules from " + harness + "\n"},
		{"skills/review/SKILL.md", "---\nname: review\ndescription: from " + harness + "\n---\nbody from " + harness + "\n"},
	} {
		path := filepath.Join(dir, filepath.FromSlash(item.rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(item.data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
