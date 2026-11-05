package also

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Paths are relative to an adapter base (home config dir, project root, or bundle root).
// An absolute or ~-prefixed value is used as-is.
type Paths struct {
	Instr     string
	Skills    string
	Commands  string
	Agents    string
	Plugins   string
	Hooks     string
	Settings  string
	HooksJSON string
	MCP       string
}

type Harness struct {
	ID      string
	Display string
	Aliases []string

	GlobalBase string // relative to home, or absolute
	Global     Paths
	Project    Paths    // relative to the project root
	Bundle     Paths    // relative to a bundle root
	Roots      []string // bundle subdirectories to inspect for components

	HooksKind string // claude | codex | qwen | none
	MCPKind   string // claude | opencode | codex | qwen | none

	// SettingsKeys are top-level config keys that can be merged safely when the
	// source and target harness use the same settings model (same harness).
	SettingsKeys []string
	// ExtraDirs are opaque harness directories copied verbatim on same-harness
	// installs (status lines, output styles, themes, extensions).
	ExtraDirs []string
	// ExtraBase is the project-relative directory holding ExtraDirs.
	ExtraBase string

	HasCommands bool
	HasAgents   bool
	HasPlugins  bool
}

var harnesses = []*Harness{
	{
		ID: "claude", Display: "Claude Code", Aliases: []string{"claude-code", "cc"},
		GlobalBase: "~/.claude",
		Global: Paths{
			Instr: "CLAUDE.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: "~/.claude.json",
		},
		Project: Paths{
			Instr: "CLAUDE.md", Skills: ".claude/skills", Commands: ".claude/commands",
			Agents: ".claude/agents", Hooks: ".claude/hooks", Settings: ".claude/settings.json",
			MCP: ".mcp.json",
		},
		Bundle: Paths{
			Instr: "CLAUDE.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: ".mcp.json",
		},
		Roots:        []string{"", ".claude"},
		HooksKind:    "claude",
		MCPKind:      "claude",
		SettingsKeys: []string{"permissions", "statusLine"},
		ExtraDirs:    []string{"status_lines", "output-styles", "themes"},
		ExtraBase:    ".claude",
		HasCommands:  true,
		HasAgents:    true,
	},
	{
		ID: "opencode", Display: "OpenCode", Aliases: []string{"oc", "open-code"},
		GlobalBase: "~/.config/opencode",
		Global: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Plugins: "plugins", Hooks: "hooks", MCP: "opencode.json",
		},
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".opencode/skills", Commands: ".opencode/commands",
			Agents: ".opencode/agents", Plugins: ".opencode/plugins", Hooks: ".opencode/hooks",
			MCP: "opencode.json",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Plugins: "plugins", Hooks: "hooks", MCP: "opencode.json",
		},
		Roots:        []string{"", ".opencode"},
		HooksKind:    "none",
		MCPKind:      "opencode",
		SettingsKeys: []string{"permissions"},
		ExtraDirs:    []string{"themes"},
		ExtraBase:    ".opencode",
		HasCommands:  true,
		HasAgents:    true,
		HasPlugins:   true,
	},
	{
		ID: "codex", Display: "Codex CLI", Aliases: []string{"codex-cli"},
		GlobalBase: "~/.codex",
		Global: Paths{
			Instr: "AGENTS.md", Skills: "~/.agents/skills", Hooks: "hooks",
			HooksJSON: "hooks.json", MCP: "config.toml",
		},
		Project: Paths{
			Instr: "AGENTS.md", Skills: ".agents/skills", Hooks: ".codex/hooks",
			HooksJSON: ".codex/hooks.json", MCP: ".codex/config.toml",
		},
		Bundle: Paths{
			Instr: "AGENTS.md", Skills: "skills", Hooks: "hooks",
			HooksJSON: "hooks.json", MCP: "config.toml",
		},
		Roots:     []string{"", ".agents", ".codex"},
		HooksKind: "codex",
		MCPKind:   "codex",
		ExtraDirs: []string{"themes"},
		ExtraBase: ".codex",
	},
	{
		ID: "qwen", Display: "Qwen Code", Aliases: []string{"qwen-code", "qwen3"},
		GlobalBase: "~/.qwen",
		Global: Paths{
			Instr: "QWEN.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: "settings.json",
		},
		Project: Paths{
			Instr: "QWEN.md", Skills: ".qwen/skills", Commands: ".qwen/commands",
			Agents: ".qwen/agents", Hooks: ".qwen/hooks", Settings: ".qwen/settings.json",
			MCP: ".qwen/settings.json",
		},
		Bundle: Paths{
			Instr: "QWEN.md", Skills: "skills", Commands: "commands", Agents: "agents",
			Hooks: "hooks", Settings: "settings.json", MCP: "settings.json",
		},
		Roots:        []string{"", ".qwen", ".agents"},
		HooksKind:    "qwen",
		MCPKind:      "qwen",
		SettingsKeys: []string{"permissions"},
		ExtraDirs:    []string{"status_lines", "extensions", "themes"},
		ExtraBase:    ".qwen",
		HasCommands:  true,
		HasAgents:    true,
	},
}

func Harnesses() []*Harness { return harnesses }

func HarnessIDs() []string {
	ids := make([]string, 0, len(harnesses))
	for _, h := range harnesses {
		ids = append(ids, h.ID)
	}
	return ids
}

func ResolveHarness(s string) (*Harness, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "@")
	for _, h := range harnesses {
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
	Scope     string
	Project   string // absolute, empty for global
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
}

func (t Target) ComponentDirs() []string {
	return []string{t.Skills, t.Commands, t.Agents, t.Plugins, t.Hooks}
}

func (t Target) Supports(kind string) bool {
	switch kind {
	case "commands":
		return t.Harness.HasCommands
	case "agents":
		return t.Harness.HasAgents
	case "plugins":
		return t.Harness.HasPlugins
	}
	return true
}

func (h *Harness) targetBase(scope, project string) string {
	if scope == "global" {
		return ExpandHome(h.GlobalBase)
	}
	return project
}

func (h *Harness) paths(scope string) Paths {
	if scope == "global" {
		return h.Global
	}
	return h.Project
}

func (h *Harness) resolve(scope, project, rel string) string {
	if rel == "" {
		return ""
	}
	if strings.HasPrefix(rel, "~/") || filepath.IsAbs(rel) {
		return ExpandHome(rel)
	}
	return filepath.Join(h.targetBase(scope, project), rel)
}

// TargetPath returns the resolved absolute path for a scope.
func (h *Harness) TargetPath(scope, project string) Target {
	p := h.paths(scope)
	t := Target{
		Harness: h, Scope: scope, Project: project,
		Instr:     h.resolve(scope, project, p.Instr),
		Skills:    h.resolve(scope, project, p.Skills),
		Commands:  h.resolve(scope, project, p.Commands),
		Agents:    h.resolve(scope, project, p.Agents),
		Plugins:   h.resolve(scope, project, p.Plugins),
		Hooks:     h.resolve(scope, project, p.Hooks),
		Settings:  h.resolve(scope, project, p.Settings),
		HooksJSON: h.resolve(scope, project, p.HooksJSON),
		MCP:       h.resolve(scope, project, p.MCP),
		Root:      h.targetBase(scope, project),
	}
	if h.ID == "opencode" {
		t.MCP = pickOpenCodeConfig(scope, project)
	}
	if scope == "global" {
		t.ExtraDir = t.Root
	} else {
		t.ExtraDir = filepath.Join(project, h.ExtraBase)
	}
	return t
}

func pickOpenCodeConfig(scope, project string) string {
	var candidates []string
	if scope == "global" {
		base := ExpandHome("~/.config/opencode")
		candidates = []string{
			filepath.Join(base, "opencode.jsonc"),
			filepath.Join(base, "opencode.json"),
		}
	} else {
		candidates = []string{
			filepath.Join(project, ".opencode", "opencode.jsonc"),
			filepath.Join(project, ".opencode", "opencode.json"),
			filepath.Join(project, "opencode.jsonc"),
			filepath.Join(project, "opencode.json"),
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
			return c
		}
	}
	if scope == "global" {
		return filepath.Join(ExpandHome("~/.config/opencode"), "opencode.json")
	}
	return filepath.Join(project, "opencode.json")
}

// DetectHarness identifies a harness from a bundle or project directory.
// Returns an error listing candidates when the layout is ambiguous.
func DetectHarness(dir string) (string, error) {
	type score struct {
		id string
		n  int
	}
	scores := map[string]int{}
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(dir, rel))
		return err == nil
	}
	add := func(id string, n int) { scores[id] += n }

	if exists("CLAUDE.md") {
		add("claude", 10)
	}
	if exists(".claude") {
		add("claude", 6)
	}
	if exists("QWEN.md") {
		add("qwen", 10)
	}
	if exists(".qwen") {
		add("qwen", 6)
	}
	if exists("opencode.json") || exists("opencode.jsonc") {
		add("opencode", 10)
	}
	if exists(".opencode") {
		add("opencode", 6)
	}
	if exists(".codex") || exists("hooks.json") {
		add("codex", 8)
	}
	if exists("config.toml") {
		add("codex", 6)
	}
	if exists(".agents") {
		// A shared skills directory; it does not identify one harness.
		add("codex", 1)
		add("qwen", 1)
		add("opencode", 1)
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

// InstructionName returns the name of the harness instruction file.
func (h *Harness) InstructionName() string {
	return filepath.Base(h.Bundle.Instr)
}
