package also

// Declarative adapters let a community add a harness without recompiling when
// it reuses an existing encoding. They cannot execute code; they can only pick
// registered formats and declare validated paths.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type AdapterSpec struct {
	ID           string          `json:"id"`
	Display      string          `json:"display"`
	Aliases      []string        `json:"aliases,omitempty"`
	GlobalBase   string          `json:"global_base,omitempty"`
	Global       Paths           `json:"global"`
	Project      Paths           `json:"project"`
	Bundle       Paths           `json:"bundle"`
	Roots        []string        `json:"roots,omitempty"`
	Hooks        *HookSpec       `json:"hooks,omitempty"`
	MCP          *MCPSpec        `json:"mcp,omitempty"`
	Settings     *SettingsSpec   `json:"settings,omitempty"`
	Commands     *CommandAdapter `json:"commands,omitempty"`
	Agents       *AgentAdapter   `json:"agents,omitempty"`
	Plugins      *PluginAdapter  `json:"plugins,omitempty"`
	Components   []Component     `json:"components,omitempty"`
	ExtraDirs    []string        `json:"extra_dirs,omitempty"`
	ExtraBase    string          `json:"extra_base,omitempty"`
	Detect       []DetectRule    `json:"detect,omitempty"`
	InstallHints []InstallHint   `json:"install_hints,omitempty"`
}

type HookSpec struct {
	Configs []string `json:"configs"`
	Events  []string `json:"events"`
	Target  string   `json:"target"` // settings | hooks_json | mcp
	RootKey string   `json:"root_key,omitempty"`
}

type MCPSpec struct {
	Format            MCPFormat `json:"format"`
	Configs           []string  `json:"configs"`
	ProjectCandidates []string  `json:"project_candidates,omitempty"`
	GlobalCandidates  []string  `json:"global_candidates,omitempty"`
}

type SettingsSpec struct {
	Keys    []string `json:"keys"`
	Configs []string `json:"configs"`
	Target  string   `json:"target"` // settings | hooks_json | mcp
}

func (s AdapterSpec) Harness() (*Harness, error) {
	settingsFile := targetFunc(s.SettingsTarget())
	h := &Harness{
		ID: s.ID, Display: s.Display, Aliases: s.Aliases,
		GlobalBase: s.GlobalBase, Global: s.Global, Project: s.Project, Bundle: s.Bundle,
		Roots: s.Roots, ExtraDirs: s.ExtraDirs, ExtraBase: s.ExtraBase,
		Detect: s.Detect, InstallHints: s.InstallHints, Components: s.Components,
	}
	if s.Hooks != nil {
		fileFn := targetFunc(s.Hooks.Target)
		if fileFn == nil {
			return nil, fmt.Errorf("hooks.target must be settings, hooks_json or mcp")
		}
		rootKey := s.Hooks.RootKey
		if rootKey == "" {
			rootKey = "hooks"
		}
		h.Hooks = HookAdapter{
			Configs: s.Hooks.Configs, Events: s.Hooks.Events,
			Target: func(t Target) (string, string) { return fileFn(t), rootKey },
		}
	}
	if s.MCP != nil {
		h.MCP = MCPAdapter{Format: s.MCP.Format, Configs: s.MCP.Configs}
		for _, c := range s.MCP.ProjectCandidates {
			if err := validateRel("mcp project candidate", c); err != nil {
				return nil, err
			}
		}
		for _, c := range s.MCP.GlobalCandidates {
			if err := validateBasePath("mcp global candidate", c); err != nil {
				return nil, err
			}
		}
		if len(s.MCP.ProjectCandidates) > 0 || len(s.MCP.GlobalCandidates) > 0 {
			h.MCP.Resolve = mcpResolver(h, s.MCP.ProjectCandidates, s.MCP.GlobalCandidates)
		}
	}
	if s.Settings != nil {
		if settingsFile == nil {
			return nil, fmt.Errorf("settings.target must be settings, hooks_json or mcp")
		}
		h.Settings = SettingsAdapter{Keys: s.Settings.Keys, Configs: s.Settings.Configs, Target: settingsFile}
	}
	if s.Commands != nil {
		h.Commands = *s.Commands
	}
	if s.Agents != nil {
		h.Agents = *s.Agents
	}
	if s.Plugins != nil {
		h.Plugins = *s.Plugins
	}
	return h, nil
}

func (s AdapterSpec) SettingsTarget() string {
	if s.Settings == nil {
		return ""
	}
	return s.Settings.Target
}

func targetFunc(name string) func(Target) string {
	switch name {
	case "settings":
		return func(t Target) string { return t.Settings }
	case "hooks_json":
		return func(t Target) string { return t.HooksJSON }
	case "mcp":
		return func(t Target) string { return t.MCP }
	}
	return nil
}

func mcpResolver(h *Harness, projectCandidates, globalCandidates []string) func(scope, project string) string {
	return func(scope, project string) string {
		var candidates []string
		if scope == "global" {
			for _, c := range globalCandidates {
				candidates = append(candidates, h.resolve(scope, project, c))
			}
		} else {
			for _, c := range projectCandidates {
				candidates = append(candidates, h.resolve(scope, project, c))
			}
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
				return c
			}
		}
		if len(candidates) > 0 {
			return candidates[0]
		}
		return h.resolve(scope, project, h.paths(scope).MCP)
	}
}

var externalAdaptersOnce sync.Once

func ensureExternalAdapters() {
	externalAdaptersOnce.Do(func() {
		dir := os.Getenv("ALSO_ADAPTERS")
		if dir == "" {
			dir = filepath.Join(HomeDir(), "adapters")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				warnAdapter(path, err)
				continue
			}
			var spec AdapterSpec
			if err := json.Unmarshal(data, &spec); err != nil {
				warnAdapter(path, err)
				continue
			}
			h, err := spec.Harness()
			if err != nil {
				warnAdapter(path, err)
				continue
			}
			if err := addAdapter(h); err != nil {
				warnAdapter(path, err)
			}
		}
	})
}

func warnAdapter(path string, err error) {
	fmt.Fprintf(os.Stderr, "also: ignoring adapter %s: %v\n", path, err)
}
