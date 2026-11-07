package also

import (
	"bytes"
	"fmt"
	"io"
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

// runGitBytes returns raw stdout (for git archive) with stderr kept separate.
func runGitBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return out.Bytes(), nil
}

// gitBatchBlobs reads many <commit>:<path> objects in one git process. Missing
// objects simply have no entry in the result.
func gitBatchBlobs(dir string, specs []string) (map[string][]byte, error) {
	if len(specs) == 0 {
		return map[string][]byte{}, nil
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = dir
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		for _, spec := range specs {
			_, _ = io.WriteString(stdin, spec+"\n")
		}
		_ = stdin.Close()
	}()
	limit := int64(len(specs))*(1<<20) + 1<<20
	data, readErr := io.ReadAll(io.LimitReader(stdout, limit))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		return nil, fmt.Errorf("git cat-file --batch: %s", strings.TrimSpace(errOut.String()))
	}
	if int64(len(data)) >= limit {
		return nil, fmt.Errorf("git cat-file --batch: output exceeds %d bytes", limit)
	}
	out := map[string][]byte{}
	for _, spec := range specs {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			break
		}
		fields := strings.Fields(string(data[:nl]))
		data = data[nl+1:]
		if len(fields) < 3 || fields[1] != "blob" {
			continue
		}
		var size int64
		if _, err := fmt.Sscanf(fields[2], "%d", &size); err != nil || size < 0 || int64(len(data)) < size+1 {
			break
		}
		out[spec] = data[:size]
		data = data[size+1:]
	}
	return out, nil
}

// publishableFiles returns the paths git would track under root, relative to
// root. The bool is false when root is not inside a git work tree.
func publishableFiles(root string) (map[string]bool, bool) {
	top, err := runGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, false
	}
	top = filepath.Clean(strings.TrimSpace(top))
	// git resolves symlinks in --show-toplevel (e.g. /var -> /private/var on
	// macOS); resolve root too so relative paths line up.
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	out, err := runGitBytes(top, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		full := filepath.Join(top, filepath.FromSlash(p))
		rel, err := filepath.Rel(root, full)
		if err != nil {
			continue
		}
		allowed[filepath.ToSlash(rel)] = true
	}
	return allowed, true
}

// runGitBytesLimit streams raw stdout but refuses to buffer more than max bytes.
func runGitBytesLimit(dir string, max int64, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var errOut bytes.Buffer
	cmd.Stderr = &errOut
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, max+1))
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, readErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(errOut.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("git %s: output exceeds %d bytes", strings.Join(args, " "), max)
	}
	return data, nil
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
		if err := setLFConfig(dir); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return false, err
	}
	// Git for Windows defaults to core.autocrlf=true, which rewrites the
	// worktree to CRLF and breaks content digests. Force LF for set clones.
	if _, err := runGit("", "clone", "--quiet", "--config", "core.autocrlf=false", "--config", "core.eol=lf", remote, dir); err != nil {
		if lsErr != nil {
			return false, fmt.Errorf("cannot reach remote %s: %v", remote, lsErr)
		}
		return false, err
	}
	return false, nil
}

func setLFConfig(dir string) error {
	if _, err := runGit(dir, "config", "core.autocrlf", "false"); err != nil {
		return err
	}
	_, err := runGit(dir, "config", "core.eol", "lf")
	return err
}

// ensureLineEndings repairs a clone made with core.autocrlf=true. The digest
// covers worktree bytes, so CRLF files fail the package integrity check.
func ensureLineEndings(dir string) error {
	out, err := runGit(dir, "config", "--get", "core.autocrlf")
	val := strings.TrimSpace(out)
	if err == nil && val == "false" {
		return nil
	}
	if err == nil && val == "true" {
		// Never touch a checkout with user changes.
		dirty, _, derr := gitDirty(dir)
		if derr != nil {
			return derr
		}
		if dirty {
			return nil
		}
		if err := setLFConfig(dir); err != nil {
			return err
		}
		// git checkout alone skips files whose stat is unchanged even though
		// the eol conversion changed, so remove tracked files first.
		files, err := runGit(dir, "ls-files", "-z")
		if err != nil {
			return err
		}
		for _, rel := range strings.Split(files, "\x00") {
			if rel == "" {
				continue
			}
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		_, err = runGit(dir, "checkout", "--", ".")
		return err
	}
	return setLFConfig(dir)
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
	// Version snapshots live in tags; branch pulls do not always fetch them.
	if _, err := runGit(dir, "fetch", "--tags", "--quiet"); err != nil {
		return fmt.Errorf("cannot fetch version tags: %w", err)
	}
	return nil
}

func pushTag(dir, tag string) error {
	if _, err := runGit(dir, "push", "--quiet", "origin", "refs/tags/"+tag); err != nil {
		return fmt.Errorf("cannot push version tag %s: %w", tag, err)
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

// versionTimes returns the newest commit time and log order per
// "pkg/version" path. Git timestamps have second granularity, so log order is
// the tie-breaker for two versions pushed within the same second.
func versionTimes(dir string) (map[string]time.Time, map[string]int, error) {
	out, err := runGit(dir, "log", "--format=%x01%ct", "--name-only")
	if err != nil {
		if !hasCommits(dir) {
			return map[string]time.Time{}, map[string]int{}, nil
		}
		return nil, nil, err
	}
	times := map[string]time.Time{}
	order := map[string]int{}
	var ts int64
	seq := -1
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "\x01") {
			seq++
			fmt.Sscanf(line[1:], "%d", &ts)
			continue
		}
		parts := strings.Split(line, "/")
		if len(parts) >= 4 && parts[0] == "packages" {
			key := parts[1] + "/" + parts[2]
			if _, ok := times[key]; !ok {
				times[key] = time.Unix(ts, 0)
				order[key] = seq
			}
		}
	}
	return times, order, nil
}

// aheadOfUpstream reports whether the current branch has commits to push.
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
