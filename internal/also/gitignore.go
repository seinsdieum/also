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
	if kind != IgnoreInfra && kind != IgnoreAlso {
		return false, fmt.Errorf("unknown ignore scope %q; use infra or you", kind)
	}
	root, err := GitProjectRoot(project)
	if err != nil {
		return false, err
	}
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	content := string(data)
	next, changed := content, false
	if add {
		next, changed = setIgnoreBlock(content, kind, entries)
	} else {
		next, changed = removeIgnoreBlock(content, kind)
	}
	if !changed {
		return false, nil
	}
	if strings.TrimSpace(next) == "" {
		if _, err := os.Stat(path); err == nil {
			return true, os.Remove(path)
		}
		return true, nil
	}
	return true, atomicWrite(path, []byte(next), 0o644)
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
	for _, e := range uniqueStrings(entries) {
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
