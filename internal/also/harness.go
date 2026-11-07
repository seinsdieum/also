package also

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HarnessIDs returns the registered adapter ids.
func HarnessIDs() []string {
	var ids []string
	for _, h := range Harnesses() {
		ids = append(ids, h.ID)
	}
	return ids
}

func ResolveHarness(s string) (*Harness, bool) {
	ensureExternalAdapters()
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "@")
	for _, h := range Harnesses() {
		if s == h.ID {
			return h, true
		}
		for _, a := range h.Aliases {
			if s == a {
				return h, true
			}
		}
	}
	return nil, false
}

func MustHarness(id string) *Harness {
	h, ok := ResolveHarness(id)
	if !ok {
		panic("unknown harness " + id)
	}
	return h
}

// Target resolves absolute destinations for an install.
type Target struct {
	Harness   *Harness
	Project   string // absolute project root
	Instr     string
	Skills    string
	Commands  string
	Agents    string
	Plugins   string
	Hooks     string
	Settings  string
	HooksJSON string
	MCP       string
	ExtraDir  string
	Root      string
	// CustomDirs maps adapter-declared component kinds to absolute dirs.
	CustomDirs map[string]string
}

func (t Target) ComponentDirs() []string {
	dirs := []string{t.Skills, t.Commands, t.Agents, t.Plugins, t.Hooks}
	for _, d := range t.CustomDirs {
		dirs = append(dirs, d)
	}
	return dirs
}

func (t Target) Supports(kind string) bool {
	switch kind {
	case "commands":
		return t.Harness.Commands.Format != ""
	case "agents":
		return t.Harness.Agents.Format != ""
	case "plugins":
		return t.Harness.Plugins.Format != ""
	}
	if kind == "" {
		return false
	}
	_, ok := t.CustomDirs[kind]
	return ok
}

func (t Target) ComponentDir(kind string) string {
	return t.CustomDirs[kind]
}

func (h *Harness) resolve(project, rel string) string {
	if rel == "" {
		return ""
	}
	if strings.HasPrefix(rel, "~/") || filepath.IsAbs(rel) {
		return ExpandHome(rel)
	}
	return filepath.Join(project, rel)
}

// TargetPath returns the resolved absolute paths for a project install.
func (h *Harness) TargetPath(project string) Target {
	p := h.Project
	t := Target{
		Harness: h, Project: project,
		Instr:     h.resolve(project, p.Instr),
		Skills:    h.resolve(project, p.Skills),
		Commands:  h.resolve(project, p.Commands),
		Agents:    h.resolve(project, p.Agents),
		Plugins:   h.resolve(project, p.Plugins),
		Hooks:     h.resolve(project, p.Hooks),
		Settings:  h.resolve(project, p.Settings),
		HooksJSON: h.resolve(project, p.HooksJSON),
		MCP:       h.resolve(project, p.MCP),
		Root:      project,
		ExtraDir:  filepath.Join(project, h.ExtraBase),
	}
	if h.MCP.Resolve != nil {
		t.MCP = h.MCP.Resolve(project)
	}
	t.CustomDirs = map[string]string{}
	for _, c := range h.Components {
		if c.ProjectDir == "" {
			continue
		}
		t.CustomDirs[c.Kind] = h.resolve(project, c.ProjectDir)
	}
	return t
}

// DetectHarness identifies a harness from a bundle or project directory.
// Returns an error listing candidates when the layout is ambiguous.
func DetectHarness(dir string) (string, error) {
	ensureExternalAdapters()
	type score struct {
		id string
		n  int
	}
	scores := map[string]int{}
	for _, h := range Harnesses() {
		for _, rule := range h.Detect {
			for _, rel := range rule.Paths {
				if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
					scores[h.ID] += rule.Weight
					break
				}
			}
		}
	}

	var best []score
	for id, n := range scores {
		if n == 0 {
			continue
		}
		best = append(best, score{id, n})
	}
	sort.Slice(best, func(i, j int) bool {
		if best[i].n != best[j].n {
			return best[i].n > best[j].n
		}
		return best[i].id < best[j].id
	})
	if len(best) == 0 {
		return "", fmt.Errorf("cannot detect harness in %s; pass --harness (available: %v)", dir, HarnessIDs())
	}
	if len(best) > 1 && best[0].n == best[1].n {
		return "", fmt.Errorf("ambiguous harness in %s (%s and %s); pass --harness (available: %v)",
			dir, best[0].id, best[1].id, HarnessIDs())
	}
	return best[0].id, nil
}

func (h *Harness) Component(kind string) *Component {
	for i := range h.Components {
		if h.Components[i].Kind == kind {
			return &h.Components[i]
		}
	}
	return nil
}

func hasExtension(rel string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	for _, e := range exts {
		if ext == strings.ToLower(e) {
			return true
		}
	}
	return false
}

// BundleRoots returns candidate relative roots present in a bundle, in priority order.
func (h *Harness) BundleRoots(bundleDir string) []string {
	var out []string
	for _, r := range h.Roots {
		if r == "" {
			out = append(out, "")
			continue
		}
		if st, err := os.Stat(filepath.Join(bundleDir, r)); err == nil && st.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// BundleRootsFromFiles is BundleRoots for a package loaded from git, where
// there is no worktree to stat.
func (h *Harness) BundleRootsFromFiles(files map[string]*SnapshotFile) []string {
	var out []string
	for _, r := range h.Roots {
		if r == "" {
			out = append(out, "")
			continue
		}
		prefix := r + "/"
		for rel := range files {
			if strings.HasPrefix(rel, prefix) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// InstructionName returns the name of the harness instruction file.
func (h *Harness) InstructionName() string {
	return filepath.Base(h.Bundle.Instr)
}
