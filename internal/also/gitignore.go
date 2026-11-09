package also

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IgnoreKind names the managed .gitignore scopes.
type IgnoreKind string

const (
	IgnoreInfra IgnoreKind = "infra"
	IgnoreAlso  IgnoreKind = "you"
)

const ignoreBeginPrefix = "# also:begin "
const ignoreEndPrefix = "# also:end "

func ignoreBegin(kind IgnoreKind) string { return ignoreBeginPrefix + string(kind) }
func ignoreEnd(kind IgnoreKind) string   { return ignoreEndPrefix + string(kind) }

// GitProjectRoot finds the git repository containing start.
func GitProjectRoot(start string) (string, error) {
	dir, err := AbsPath(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("not inside a git project: %s", start)
}

// InfraIgnoreEntries returns the top-level project paths used by harness
// adapters. When harnessIDs is empty every registered adapter contributes.
func InfraIgnoreEntries(harnessIDs []string) []string {
	var list []*Harness
	if len(harnessIDs) == 0 {
		list = Harnesses()
	} else {
		for _, id := range harnessIDs {
			if h, ok := ResolveHarness(id); ok {
				list = append(list, h)
			}
		}
	}
	seen := map[string]bool{}
	add := func(p string) {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" || strings.HasPrefix(p, "~/") || filepath.IsAbs(p) || p == "." {
			return
		}
		first := strings.Split(p, "/")[0]
		if first == "" || first == "." || first == ".." || reservedSegments[strings.ToLower(first)] {
			return
		}
		if strings.Contains(p, "/") {
			first += "/"
		}
		seen[first] = true
		if strings.HasSuffix(first, ".json") && !strings.HasPrefix(first, ".") {
			seen[strings.TrimSuffix(first, ".json")+".jsonc"] = true
		}
	}
	addPaths := func(h *Harness) {
		p := h.Project
		for _, v := range []string{
			p.Instr, p.Skills, p.Commands, p.Agents, p.Plugins,
			p.Hooks, p.Settings, p.HooksJSON, p.MCP,
		} {
			add(v)
		}
		for _, c := range h.Components {
			add(c.ProjectDir)
		}
		if h.ExtraBase != "" {
			add(h.ExtraBase)
		}
		for _, d := range h.ExtraDirs {
			if h.ExtraBase != "" {
				add(filepath.Join(h.ExtraBase, d))
			} else {
				add(d)
			}
		}
	}
	for _, h := range list {
		addPaths(h)
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		if strings.HasSuffix(p, "/") {
			delete(seen, strings.TrimSuffix(p, "/"))
		}
	}
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

var alsoIgnoreEntries = []string{".also-layers/", ".also/"}

// AlsoIgnoreEntries returns also's own project metadata paths.
func AlsoIgnoreEntries() []string { return append([]string{}, alsoIgnoreEntries...) }

// UpdateGitIgnore adds or removes a managed block in the project .gitignore.
func UpdateGitIgnore(project string, kind IgnoreKind, add bool, entries []string) (bool, error) {
	root, err := GitProjectRoot(project)
	if err != nil {
		return false, err
	}
	return UpdateGitIgnoreAt(filepath.Join(root, ".gitignore"), kind, add, entries)
}

// transformGitIgnore is the single precondition and transform path for the
// managed block. UpdateGitIgnoreAt writes its result; GitIgnoreWouldChange only
// inspects it, so both reject the same paths and agree on what changed.
func transformGitIgnore(path string, kind IgnoreKind, add bool, entries []string) (next string, changed bool, mode os.FileMode, exists bool, err error) {
	if kind != IgnoreInfra && kind != IgnoreAlso {
		return "", false, 0, false, fmt.Errorf("unknown ignore scope %q; use infra or you", kind)
	}
	mode = 0o644
	if info, lerr := os.Lstat(path); lerr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", false, 0, false, fmt.Errorf("refusing to replace symlink %s; make it a regular file first", path)
		}
		mode = info.Mode().Perm()
		exists = true
	} else if !os.IsNotExist(lerr) {
		return "", false, 0, false, lerr
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", false, 0, false, rerr
	}
	content := string(data)
	if add {
		next, changed = setIgnoreBlock(content, kind, entries)
	} else {
		next, changed = removeIgnoreBlock(content, kind)
	}
	return next, changed, mode, exists, nil
}

// UpdateGitIgnoreAt updates a specific .gitignore file. It is used for
// archive checkouts that have no .git yet but still carry a managed block.
func UpdateGitIgnoreAt(path string, kind IgnoreKind, add bool, entries []string) (bool, error) {
	next, changed, mode, exists, err := transformGitIgnore(path, kind, add, entries)
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if strings.TrimSpace(next) == "" {
		if exists {
			return true, os.Remove(path)
		}
		return true, nil
	}
	return true, atomicWrite(path, []byte(next), mode)
}

// GitIgnoreWouldChange reports whether UpdateGitIgnoreAt would change the file.
// It reads the same bytes and applies the same managed-block transform without
// writing, so formatting inside the block counts exactly as the writer's byte
// comparison does.
func GitIgnoreWouldChange(path string, kind IgnoreKind, add bool, entries []string) (bool, error) {
	_, changed, _, _, err := transformGitIgnore(path, kind, add, entries)
	return changed, err
}

// IgnoreBlockEntries reads the entries of the managed block, so a scoped
// exclude can merge with other harnesses instead of replacing them.
func IgnoreBlockEntries(project string, kind IgnoreKind) ([]string, error) {
	root, err := GitProjectRoot(project)
	if err != nil {
		return nil, err
	}
	return IgnoreBlockEntriesAt(filepath.Join(root, ".gitignore"), kind)
}

// IgnoreBlockEntriesAt reads a managed block from a specific .gitignore. It
// works for archive checkouts that have no .git yet.
func IgnoreBlockEntriesAt(path string, kind IgnoreKind) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	content := string(data)
	start, end, ok := findIgnoreBlock(content, kind)
	if !ok {
		return nil, nil
	}
	var out []string
	for _, line := range strings.Split(content[start:end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "# also:") {
			continue
		}
		out = append(out, line)
	}
	return UniqueStrings(out), nil
}

func findIgnoreBlock(content string, kind IgnoreKind) (start, end int, ok bool) {
	begin, finish := ignoreBegin(kind), ignoreEnd(kind)
	bi := strings.Index(content, begin)
	if bi < 0 {
		return 0, 0, false
	}
	ei := strings.Index(content[bi:], finish)
	if ei < 0 {
		return 0, 0, false
	}
	ei += bi
	start = bi
	if ls := strings.LastIndexByte(content[:bi], '\n'); ls >= 0 {
		start = ls + 1
	}
	end = ei + len(finish)
	if nl := strings.IndexByte(content[end:], '\n'); nl >= 0 {
		end += nl + 1
	}
	return start, end, true
}

func setIgnoreBlock(content string, kind IgnoreKind, entries []string) (string, bool) {
	var lines []string
	for _, e := range UniqueStrings(entries) {
		if e != "" {
			lines = append(lines, e)
		}
	}
	block := ignoreBegin(kind) + "\n" + strings.Join(lines, "\n")
	if len(lines) > 0 {
		block += "\n"
	}
	block += ignoreEnd(kind) + "\n"
	if start, end, ok := findIgnoreBlock(content, kind); ok {
		next := content[:start] + block + content[end:]
		return next, next != content
	}
	prefix := ""
	if strings.TrimSpace(content) != "" {
		if !strings.HasSuffix(content, "\n") {
			prefix = "\n\n"
		} else if !strings.HasSuffix(content, "\n\n") {
			prefix = "\n"
		}
	}
	next := content + prefix + block
	return next, next != content
}

func removeIgnoreBlock(content string, kind IgnoreKind) (string, bool) {
	start, end, ok := findIgnoreBlock(content, kind)
	if !ok {
		return content, false
	}
	next := content[:start] + content[end:]
	return next, true
}
