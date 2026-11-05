package also

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return msg, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

// gitCommitArgs adds a fallback identity when the machine has none configured.
func gitCommitArgs(dir string, args ...string) []string {
	if _, err := runGit(dir, "var", "GIT_AUTHOR_IDENT"); err == nil {
		return args
	}
	return append([]string{"-c", "user.name=also", "-c", "user.email=also@localhost"}, args...)
}

func isGitRepo(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && st.IsDir()
}

func hasCommits(dir string) bool {
	_, err := runGit(dir, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func gitDirty(dir string) (bool, string, error) {
	out, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return false, "", err
	}
	s := strings.TrimSpace(out)
	return s != "", s, nil
}

func cloneOrInit(remote, dir string) (created bool, err error) {
	out, lsErr := runGit("", "ls-remote", "--heads", remote)
	empty := lsErr == nil && strings.TrimSpace(out) == ""
	if empty {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return false, err
		}
		if _, err := runGit(dir, "init", "-b", "main"); err != nil {
			return false, err
		}
		if _, err := runGit(dir, "remote", "add", "origin", remote); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, err
	}
	if _, err := runGit("", "clone", "--quiet", remote, dir); err != nil {
		if lsErr != nil {
			return false, fmt.Errorf("cannot reach remote %s: %v", remote, lsErr)
		}
		return false, err
	}
	return false, nil
}

func pullFF(dir string) error {
	if !hasCommits(dir) {
		return nil
	}
	if _, err := runGit(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		// No upstream yet (the first push failed or the remote was empty).
		return nil
	}
	if _, err := runGit(dir, "pull", "--ff-only", "--quiet"); err != nil {
		return fmt.Errorf("cannot fast-forward the local also clone (run 'git -C %s status' and resolve): %w", dir, err)
	}
	return nil
}

func pushHead(dir string) error {
	args := []string{"push", "--quiet", "origin", "HEAD"}
	if _, err := runGit(dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		args = []string{"push", "--quiet", "--set-upstream", "origin", "HEAD"}
	}
	if _, err := runGit(dir, args...); err == nil {
		return nil
	}
	// Another machine may have pushed between our pull and push.
	branch, berr := runGit(dir, "symbolic-ref", "--short", "HEAD")
	if berr != nil {
		return fmt.Errorf("push failed: %w", berr)
	}
	if _, err := runGit(dir, "pull", "--rebase", "--quiet", "origin", strings.TrimSpace(branch)); err != nil {
		_, _ = runGit(dir, "rebase", "--abort")
		return fmt.Errorf("push rejected and rebase failed; the new version is committed locally in %s. Resolve manually, then retry 'also push'", dir)
	}
	if _, err := runGit(dir, "push", "--quiet", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push failed; the new version is committed locally in %s: %w", dir, err)
	}
	return nil
}

func commitPaths(dir, msg string, paths ...string) (bool, error) {
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := runGit(dir, args...); err != nil {
		return false, err
	}
	if _, err := runGit(dir, "diff", "--cached", "--quiet"); err == nil {
		return false, nil
	}
	cargs := gitCommitArgs(dir, "commit", "--quiet", "-m", msg)
	if _, err := runGit(dir, cargs...); err != nil {
		return false, err
	}
	return true, nil
}

// versionTimes returns the newest commit time per "pkg/version" path.
func versionTimes(dir string) (map[string]time.Time, error) {
	out, err := runGit(dir, "log", "--format=%x01%ct", "--name-only")
	if err != nil {
		if !hasCommits(dir) {
			return map[string]time.Time{}, nil
		}
		return nil, err
	}
	times := map[string]time.Time{}
	var ts int64
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "\x01") {
			fmt.Sscanf(line[1:], "%d", &ts)
			continue
		}
		parts := strings.Split(line, "/")
		if len(parts) >= 4 && parts[0] == "packages" {
			key := parts[1] + "/" + parts[2]
			if _, ok := times[key]; !ok {
				times[key] = time.Unix(ts, 0)
			}
		}
	}
	return times, nil
}

// committedVersions returns pkg/version keys present at HEAD, derived from the package manifest.
func committedVersions(dir string) (map[string]bool, error) {
	if !hasCommits(dir) {
		return map[string]bool{}, nil
	}
	out, err := runGit(dir, "ls-tree", "-r", "--name-only", "HEAD", "--", "packages")
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "/")
		if len(parts) == 4 && parts[0] == "packages" && parts[3] == PackageManifestFile {
			set[parts[1]+"/"+parts[2]] = true
		}
	}
	return set, nil
}

func isVersionCommitted(dir, pkg, version string) bool {
	_, err := runGit(dir, "cat-file", "-e", "HEAD:packages/"+pkg+"/"+version+"/"+PackageManifestFile)
	return err == nil
}

func aheadOfUpstream(dir string) bool {
	out, err := runGit(dir, "rev-list", "--count", "@{u}..HEAD")
	if err != nil {
		return hasCommits(dir)
	}
	n := strings.TrimSpace(out)
	return n != "0" && n != ""
}

func ensureLocalExcludes(dir string) {
	path := filepath.Join(dir, ".git", "info", "exclude")
	data, _ := os.ReadFile(path)
	text := string(data)
	want := ".also-pkg-*\n.also-tmp-*\n"
	if strings.Contains(text, ".also-pkg-*") {
		return
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(text+want), 0o644)
}
