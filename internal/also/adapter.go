package also

// This file defines the adapter contract. A harness is an adapter: the system
// owns install/merge/digest behaviour, while the adapter owns everything that
// is specific to one harness (paths, capabilities, config formats, conversion
// of commands/agents/MCP/hooks, detection rules and post-install hints).
//
// To add a harness, create a new adapters_<id>.go file with a constructor and
// register it from init(). The registry validates the adapter before use, so
// an invalid or unsafe adapter cannot participate in an install.

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Command dialects. They are ecosystem formats, not harness identities.
const (
	CommandClaude = "claude"
	CommandQwen   = "qwen"
)

// Agent frontmatter dialects.
const (
	AgentClaude   = "claude"
	AgentOpenCode = "opencode"
)

// Plugin format.
const PluginOpenCode = "opencode"

// MCPFormat is the native MCP encoding of a harness. Reusing a format is the
// normal way to support a new harness that follows an existing ecosystem.
type MCPFormat string

const (
	MCPNone         MCPFormat = ""
	MCPClaudeJSON   MCPFormat = "claude-json"
	MCPQwenJSON     MCPFormat = "qwen-json"
	MCPOpenCodeJSON MCPFormat = "opencode-json"
	MCPCodexTOML    MCPFormat = "codex-toml"
)

// HookAdapter describes where a harness keeps declarative hooks. Empty Configs
// means the harness has no portable declarative hooks.
type HookAdapter struct {
	Configs []string // bundle-relative config files that may contain hooks
	Events  []string // supported event names
	Target  func(Target) (file, rootKey string)
	// Extract overrides the default JSON/TOML extraction for a new format.
	Extract func(configs []string, files map[string]*SnapshotFile, roots []string) (HookSet, []string, error)
}

// MCPAdapter describes where a harness keeps MCP servers and how they are
// encoded. Resolve optionally overrides the project/global MCP destination
// when a harness searches several config filenames.
type MCPAdapter struct {
	Format  MCPFormat
	Configs []string // bundle-relative config files that may contain servers
	Resolve func(scope, project string) string
}

// SettingsAdapter describes the native config keys that can be merged safely
// on same-harness installs. Target selects the file that holds those keys.
type SettingsAdapter struct {
	Keys    []string
	Configs []string
	Target  func(Target) string
	// Extract overrides the default JSONC key extraction for a new format.
	Extract func(keys, configs []string, files map[string]*SnapshotFile, roots []string) (map[string]any, []string, error)
}

// CommandAdapter declares the command placeholder dialect.
type CommandAdapter struct{ Format string }

// AgentAdapter declares the agent frontmatter dialect.
type AgentAdapter struct{ Format string }

// PluginAdapter declares the plugin format. Plugins only translate between
// harnesses that use the same format.
type PluginAdapter struct{ Format string }

// DetectRule contributes weight when any of Paths exists in a directory.
type DetectRule struct {
	Paths  []string `json:"paths"`
	Weight int      `json:"weight"`
}

// InstallHint is emitted for project installs when the conditions match.
// With neither flag set the hint is unconditional.
type InstallHint struct {
	WhenMCP      bool   `json:"when_mcp,omitempty"`
	WhenSettings bool   `json:"when_settings,omitempty"`
	Text         string `json:"text"`
}

// Component declares an additional file collection (workflows, prompts, ...).
// Standard kinds (instructions, skills, commands, agents, plugins, hooks) have
// dedicated fields; custom kinds are copied by default and may set Convert.
type Component struct {
	Kind       string                                                                            `json:"kind"`
	BundleDir  string                                                                            `json:"bundle_dir"`
	ProjectDir string                                                                            `json:"project_dir,omitempty"`
	GlobalDir  string                                                                            `json:"global_dir,omitempty"`
	Extensions []string                                                                          `json:"extensions,omitempty"`
	Opaque     bool                                                                              `json:"opaque,omitempty"` // only install into the same harness
	Format     string                                                                            `json:"format,omitempty"` // optional: skip unless source and target agree
	Convert    func(data []byte, source, target *Harness, name string) ([]byte, []string, error) `json:"-"`
}

// Harness is the adapter contract. It is called Harness because that is what
// it represents; all harness-specific branching lives in the adapters_*.go
// files, not in install/render/mcp/hooks.
type Harness struct {
	ID      string
	Display string
	Aliases []string

	GlobalBase string // relative to home
	Global     Paths
	Project    Paths    // relative to the project root
	Bundle     Paths    // relative to a bundle root
	Roots      []string // bundle subdirectories to inspect for components

	Hooks    HookAdapter
	MCP      MCPAdapter
	Settings SettingsAdapter

	Commands CommandAdapter
	Agents   AgentAdapter
	Plugins  PluginAdapter

	// Components adds adapter-declared file collections beyond the standard
	// kinds (for example workflows or prompts).
	Components []Component

	// ExtraDirs are opaque harness directories copied verbatim on same-harness
	// installs (status lines, output styles, themes, extensions).
	ExtraDirs []string
	// ExtraBase is the project-relative directory holding ExtraDirs.
	ExtraBase string

	Detect       []DetectRule
	InstallHints []InstallHint
}

// Paths are relative to an adapter base (home config dir, project root, or bundle root).
// Global paths may use a ~/ prefix; they may not be absolute.
type Paths struct {
	Instr     string `json:"instr,omitempty"`
	Skills    string `json:"skills,omitempty"`
	Commands  string `json:"commands,omitempty"`
	Agents    string `json:"agents,omitempty"`
	Plugins   string `json:"plugins,omitempty"`
	Hooks     string `json:"hooks,omitempty"`
	Settings  string `json:"settings,omitempty"`
	HooksJSON string `json:"hooks_json,omitempty"`
	MCP       string `json:"mcp,omitempty"`
}

var harnesses []*Harness

// externalHarnesses are declarative adapters loaded from disk. Keeping them
// separate lets add/remove refresh the community registry without touching
// compiled-in adapters.
var externalHarnesses []*Harness

// registerAdapter is called by adapter files from init(). Built-in adapters are
// code, so an invalid one is a programming error and panics during init.
func registerAdapter(h *Harness) {
	if err := addAdapter(h); err != nil {
		panic(err)
	}
}

func addAdapter(h *Harness) error {
	if err := validateAdapter(h); err != nil {
		return fmt.Errorf("invalid adapter %q: %w", h.ID, err)
	}
	for _, cur := range harnesses {
		if cur.ID == h.ID {
			return fmt.Errorf("duplicate adapter id %q", h.ID)
		}
		if err := aliasesConflict(cur, h); err != nil {
			return err
		}
	}
	harnesses = append(harnesses, h)
	return nil
}

func aliasesConflict(a, b *Harness) error {
	names := map[string]string{}
	add := func(h *Harness) error {
		for _, name := range append([]string{h.ID}, h.Aliases...) {
			if owner, ok := names[name]; ok {
				return fmt.Errorf("name %q is claimed by both %s and %s", name, owner, h.ID)
			}
			names[name] = h.ID
		}
		return nil
	}
	if err := add(a); err != nil {
		return err
	}
	return add(b)
}

// ValidateAdapters validates a registry and its cross-adapter constraints.
func ValidateAdapters(list []*Harness) error {
	ids := map[string]string{}
	for _, h := range list {
		if err := validateAdapter(h); err != nil {
			return fmt.Errorf("adapter %q: %w", h.ID, err)
		}
		for _, name := range append([]string{h.ID}, h.Aliases...) {
			if owner, ok := ids[name]; ok {
				return fmt.Errorf("name %q is claimed by both %s and %s", name, owner, h.ID)
			}
			ids[name] = h.ID
		}
	}
	return nil
}

func validateAdapter(h *Harness) error {
	if !ValidName(h.ID) {
		return fmt.Errorf("invalid id")
	}
	if strings.TrimSpace(h.Display) == "" {
		return fmt.Errorf("display name is required")
	}
	ids := map[string]bool{h.ID: true}
	for _, a := range h.Aliases {
		norm := strings.ToLower(strings.TrimSpace(a))
		if norm != a {
			return fmt.Errorf("alias %q must be lowercase and trimmed", a)
		}
		if !ValidName(a) {
			return fmt.Errorf("invalid alias %q", a)
		}
		if ids[a] {
			return fmt.Errorf("alias %q duplicates another name", a)
		}
		ids[a] = true
	}
	if h.GlobalBase != "" {
		if filepath.IsAbs(h.GlobalBase) {
			return fmt.Errorf("global_base must be relative to the home directory")
		}
		if err := validateBasePath("global_base", h.GlobalBase); err != nil {
			return err
		}
	}
	for name, p := range map[string]Paths{"global": h.Global, "project": h.Project, "bundle": h.Bundle} {
		for field, v := range pathFields(p) {
			if v == "" {
				continue
			}
			label := name + "." + field
			if name == "bundle" {
				if err := validateRel(label, v); err != nil {
					return err
				}
				continue
			}
			if name == "project" && (strings.HasPrefix(v, "~/") || filepath.IsAbs(v)) {
				return fmt.Errorf("%s must be relative to the project", label)
			}
			if name == "global" && filepath.IsAbs(v) {
				return fmt.Errorf("%s must be relative to the home directory", label)
			}
			if err := validateBasePath(label, v); err != nil {
				return err
			}
		}
	}
	if h.Bundle.Instr == "" {
		return fmt.Errorf("bundle instructions path is required")
	}
	for _, r := range h.Roots {
		if r == "" {
			continue
		}
		if err := validateRel("root", r); err != nil {
			return err
		}
	}
	for _, d := range h.ExtraDirs {
		if err := validateRel("extra_dir", d); err != nil {
			return err
		}
	}
	if len(h.ExtraDirs) > 0 && h.ExtraBase == "" {
		return fmt.Errorf("extra dirs require extra_base")
	}
	if h.ExtraBase != "" {
		if err := validateRel("extra_base", h.ExtraBase); err != nil {
			return err
		}
	}
	if h.MCP.Format != MCPNone {
		if h.Bundle.MCP == "" {
			return fmt.Errorf("MCP format %q requires bundle.mcp", h.MCP.Format)
		}
		if _, ok := mcpCodecs[string(h.MCP.Format)]; !ok {
			return fmt.Errorf("unknown MCP format %q", h.MCP.Format)
		}
		if len(h.MCP.Configs) == 0 {
			return fmt.Errorf("MCP format %q requires at least one source config", h.MCP.Format)
		}
		for _, c := range h.MCP.Configs {
			if err := validateConfigName("mcp config", c); err != nil {
				return err
			}
		}
		if h.Project.MCP == "" && h.Global.MCP == "" && h.MCP.Resolve == nil {
			return fmt.Errorf("MCP format %q requires an MCP destination", h.MCP.Format)
		}
	}
	if len(h.Hooks.Configs) > 0 {
		if len(h.Hooks.Events) == 0 {
			return fmt.Errorf("hooks config requires supported events")
		}
		if h.Hooks.Target == nil {
			return fmt.Errorf("hooks config requires a target")
		}
		for _, c := range h.Hooks.Configs {
			if err := validateConfigName("hook config", c); err != nil {
				return err
			}
		}
	}
	if len(h.Settings.Keys) > 0 {
		if h.Bundle.Settings == "" && h.Bundle.MCP == "" {
			return fmt.Errorf("settings keys require bundle.settings or bundle.mcp")
		}
		if len(h.Settings.Configs) == 0 || h.Settings.Target == nil {
			return fmt.Errorf("settings keys require source configs and a target")
		}
		for _, c := range h.Settings.Configs {
			if err := validateConfigName("settings config", c); err != nil {
				return err
			}
		}
		for _, k := range h.Settings.Keys {
			if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "/\\") {
				return fmt.Errorf("invalid settings key %q", k)
			}
		}
	}
	if h.Commands.Format != "" && !commandFormatKnown(h.Commands.Format) {
		return fmt.Errorf("unknown command format %q", h.Commands.Format)
	}
	if h.Agents.Format != "" && !agentFormatKnown(h.Agents.Format) {
		return fmt.Errorf("unknown agent format %q", h.Agents.Format)
	}
	if h.Plugins.Format != "" && !pluginFormatKnown(h.Plugins.Format) {
		return fmt.Errorf("unknown plugin format %q", h.Plugins.Format)
	}
	if h.Commands.Format != "" && h.Project.Commands == "" && h.Global.Commands == "" {
		return fmt.Errorf("command format requires a commands directory")
	}
	if h.Agents.Format != "" && h.Project.Agents == "" && h.Global.Agents == "" {
		return fmt.Errorf("agent format requires an agents directory")
	}
	if h.Plugins.Format != "" && h.Project.Plugins == "" && h.Global.Plugins == "" {
		return fmt.Errorf("plugin format requires a plugins directory")
	}
	if h.MCP.Format == MCPCodexTOML {
		for _, c := range h.MCP.Configs {
			if !strings.HasSuffix(strings.ToLower(c), ".toml") {
				return fmt.Errorf("codex-toml MCP requires TOML configs, got %s", c)
			}
		}
	}
	standard := map[string]bool{
		"instructions": true, "skills": true, "commands": true,
		"agents": true, "plugins": true, "hooks": true, "extras": true,
	}
	seenKinds := map[string]bool{}
	for _, c := range h.Components {
		if !ValidName(c.Kind) || standard[c.Kind] || seenKinds[c.Kind] {
			return fmt.Errorf("invalid or duplicate component kind %q", c.Kind)
		}
		seenKinds[c.Kind] = true
		if c.BundleDir == "" {
			return fmt.Errorf("component %q needs a bundle dir", c.Kind)
		}
		if err := validateRel("component "+c.Kind+" bundle", c.BundleDir); err != nil {
			return err
		}
		if c.ProjectDir == "" && c.GlobalDir == "" {
			return fmt.Errorf("component %q needs a project or global dir", c.Kind)
		}
		if c.ProjectDir != "" {
			if strings.HasPrefix(c.ProjectDir, "~/") || filepath.IsAbs(c.ProjectDir) {
				return fmt.Errorf("component %q project dir must be relative to the project", c.Kind)
			}
			if err := validateRel("component "+c.Kind+" project", c.ProjectDir); err != nil {
				return err
			}
		}
		if c.GlobalDir != "" {
			if filepath.IsAbs(c.GlobalDir) {
				return fmt.Errorf("component %q global dir must be relative to home", c.Kind)
			}
			if err := validateBasePath("component "+c.Kind+" global", c.GlobalDir); err != nil {
				return err
			}
		}
		for _, ext := range c.Extensions {
			if !strings.HasPrefix(ext, ".") || strings.ContainsAny(ext, `/\`) {
				return fmt.Errorf("component %q has invalid extension %q", c.Kind, ext)
			}
		}
	}
	for _, r := range h.Detect {
		if r.Weight <= 0 {
			return fmt.Errorf("detect rule weight must be positive")
		}
		for _, p := range r.Paths {
			if err := validateRel("detect path", p); err != nil {
				return err
			}
		}
	}
	return nil
}

func pathFields(p Paths) map[string]string {
	return map[string]string{
		"instr": p.Instr, "skills": p.Skills, "commands": p.Commands,
		"agents": p.Agents, "plugins": p.Plugins, "hooks": p.Hooks,
		"settings": p.Settings, "hooks_json": p.HooksJSON, "mcp": p.MCP,
	}
}

func validateBasePath(label, p string) error {
	if strings.HasPrefix(p, "~/") {
		return validateRel(label, strings.TrimPrefix(p, "~/"))
	}
	if filepath.IsAbs(p) {
		if filepath.Clean(p) != p {
			return fmt.Errorf("%s is not clean: %s", label, p)
		}
		return nil
	}
	return validateRel(label, p)
}

var reservedSegments = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".also": true,
	".ssh": true, ".gnupg": true, ".aws": true, ".docker": true, ".kube": true,
}

func validateRel(label, p string) error {
	if err := SafeRel(p); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if path.Clean(p) != filepath.ToSlash(p) {
		return fmt.Errorf("%s is not clean: %s", label, p)
	}
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if reservedSegments[strings.ToLower(part)] {
			return fmt.Errorf("%s uses reserved path component %q: %s", label, part, p)
		}
	}
	return nil
}

func validateConfigName(label, name string) error {
	if err := validateRel(label, name); err != nil {
		return err
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%s must be a file name: %s", label, name)
	}
	return nil
}

func hasReservedSegment(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if reservedSegments[strings.ToLower(part)] {
			return true
		}
	}
	return false
}

var pluginFormats = map[string]bool{PluginOpenCode: true}

func RegisterPluginFormat(name string) {
	if strings.TrimSpace(name) == "" {
		panic("plugin format needs a name")
	}
	pluginFormats[name] = true
}

func pluginFormatKnown(f string) bool {
	return pluginFormats[f]
}

// ConfigNames returns the bundle-relative config basenames for this adapter.
func (h *Harness) ConfigNames() map[string]bool {
	m := map[string]bool{}
	add := func(p string) {
		if p != "" {
			m[filepath.Base(p)] = true
		}
	}
	for _, p := range []string{h.Bundle.Settings, h.Bundle.HooksJSON, h.Bundle.MCP} {
		add(p)
	}
	for _, list := range [][]string{h.MCP.Configs, h.Hooks.Configs, h.Settings.Configs} {
		for _, c := range list {
			add(c)
		}
	}
	add(h.Global.Settings)
	add(h.Global.HooksJSON)
	add(h.Global.MCP)
	return m
}

// HookEvents returns the supported event set for the adapter.
func (h *Harness) HookEvents() map[string]bool {
	out := map[string]bool{}
	for _, ev := range h.Hooks.Events {
		out[ev] = true
	}
	return out
}

// HooksTarget returns the file and root key for declarative hooks.
func (h *Harness) HooksTarget(t Target) (string, string) {
	if h.Hooks.Target == nil {
		return "", ""
	}
	return h.Hooks.Target(t)
}

// SettingsTarget returns the file that holds mergeable settings keys.
func (h *Harness) SettingsTarget(t Target) string {
	if h.Settings.Target == nil {
		return ""
	}
	return h.Settings.Target(t)
}

func registeredAdapters() []*Harness {
	out := append([]*Harness{}, harnesses...)
	return append(out, externalHarnesses...)
}

// Harnesses returns the registered adapters sorted by id.
func Harnesses() []*Harness {
	ensureExternalAdapters()
	out := registeredAdapters()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
