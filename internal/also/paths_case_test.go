package also

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathIdentityIgnoresCaseOnCaseInsensitiveFilesystem(t *testing.T) {
	tmp := t.TempDir()
	mixed := filepath.Join(tmp, "MiXeD")
	if err := os.MkdirAll(mixed, 0o755); err != nil {
		t.Fatal(err)
	}
	if !caseInsensitive(mixed) {
		t.Skip("case-sensitive filesystem")
	}
	lower := filepath.Join(tmp, "mixed")
	if runtime.GOOS != "windows" {
		want, err := AbsPath(mixed)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := AbsPath(lower); err != nil || got != want {
			t.Fatalf("AbsPath(%q) = %q, %v; want %q", lower, got, err, want)
		}
	}
	if PathKey(lower) != PathKey(mixed) {
		t.Fatalf("PathKey disagreed for one directory: %q vs %q", PathKey(lower), PathKey(mixed))
	}
}

func TestPublishableFilesUsesCaseVariantRoot(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "Repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(repo, "AGENTS.md"), "rules\n")
	writeFile(t, filepath.Join(repo, "skills", "s", "SKILL.md"), "skill\n")
	if out, err := exec.Command("git", "-C", repo, "add", "-A").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if !caseInsensitive(repo) {
		t.Skip("case-sensitive filesystem")
	}
	lower := filepath.Join(tmp, "repo")
	files, ok := publishableFiles(lower)
	if !ok {
		t.Fatal("publishableFiles did not recognize the work tree")
	}
	for _, want := range []string{"AGENTS.md", "skills/s/SKILL.md"} {
		if !files[want] {
			t.Fatalf("case-variant root lost %q: %v", want, files)
		}
	}
}

func TestPushRejectsInvalidComponentFrontmatter(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "agents", "qa.md"), "---\ndescription: Owns verification: smoke checks and regressions.\n---\nbody\n")
	writeFile(t, filepath.Join(bundle, "skills", "comic", "SKILL.md"), "---\nname: comic\ndescription: Use when a page needs a comic: panels and ink.\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "opencode"}); err == nil {
		t.Fatal("push accepted invalid YAML frontmatter")
	} else {
		for _, want := range []string{"agents/qa.md", "skills/comic/SKILL.md"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error does not name %s: %v", want, err)
			}
		}
	}
	if got, err := repo.Packages(); err != nil || len(got) != 0 {
		t.Fatalf("failed push published a package: %v %v", got, err)
	}
	// Quoting the values makes the package valid.
	writeFile(t, filepath.Join(bundle, "agents", "qa.md"), "---\ndescription: \"Owns verification: smoke checks and regressions.\"\n---\nbody\n")
	writeFile(t, filepath.Join(bundle, "skills", "comic", "SKILL.md"), "---\nname: comic\ndescription: \"Use when a page needs a comic: panels and ink.\"\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "opencode"}); err != nil {
		t.Fatalf("valid frontmatter was rejected: %v", err)
	}
}

func TestAuditFlagsInvalidInstalledFrontmatter(t *testing.T) {
	tmp, _, _, cfg := setup(t)
	repo := openRepo(t, cfg)
	bundle := filepath.Join(tmp, "bundle")
	writeFile(t, filepath.Join(bundle, "agents", "qa.md"), "---\ndescription: \"Owns verification: smoke checks.\"\n---\nbody\n")
	if _, err := repo.Push(bundle, PushOptions{Name: "p", Harness: "opencode"}); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(tmp, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	plan, _, err := InstallPackage(cfg, InstallRequest{Package: "p", Harness: "opencode", Project: project}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, plan.Install.Files[0].Path, "---\ndescription: broken: yaml\n---\nbody\n")
	st, _ := LoadState()
	report, err := Audit(st, project, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range report.Findings {
		if f.Code == "frontmatter-invalid" && f.Severity == "fail" {
			return
		}
	}
	t.Fatalf("invalid installed frontmatter was not a failure: %+v", report.Findings)
}
