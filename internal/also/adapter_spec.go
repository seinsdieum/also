package also

// Declarative adapters let a community add a harness without recompiling when
// it reuses an existing encoding. They cannot execute code; they can only pick
// registered formats and declare validated paths.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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

// AdapterDir is where community declarative adapters live.
func AdapterDir() string {
	if dir := os.Getenv("ALSO_ADAPTERS"); dir != "" {
		return ExpandHome(dir)
	}
	return filepath.Join(HomeDir(), "adapters")
}

// validateExternalAdapter keeps a community spec inside its own global base.
// Built-in adapters may use absolute home paths for legacy locations; external
// specs may not, because a URL-supplied spec must not be able to edit ~/.bashrc.
func validateExternalAdapter(h *Harness) error {
	hasGlobal := false
	for _, v := range pathFields(h.Global) {
		if v == "" {
			continue
		}
		hasGlobal = true
		if strings.HasPrefix(v, "~/") || filepath.IsAbs(v) {
			return fmt.Errorf("external adapter global path %q must be relative to global_base", v)
		}
	}
	if hasGlobal && h.GlobalBase == "" {
		return fmt.Errorf("external adapter with global paths needs global_base")
	}
	if h.GlobalBase != "" {
		base := ExpandHome(h.GlobalBase)
		home := ExpandHome("~")
		if !isWithin(home, base) || filepath.Clean(base) == filepath.Clean(home) {
			return fmt.Errorf("external adapter global_base must be a directory below the home directory")
		}
	}
	for _, c := range h.Components {
		if c.GlobalDir != "" && (strings.HasPrefix(c.GlobalDir, "~/") || filepath.IsAbs(c.GlobalDir)) {
			return fmt.Errorf("external adapter component %q global dir must be relative to home", c.Kind)
		}
	}
	return nil
}

func ensureExternalAdapters() {
	externalAdaptersOnce.Do(func() {
		entries, err := os.ReadDir(AdapterDir())
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
				continue
			}
			path := filepath.Join(AdapterDir(), e.Name())
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
			if err := validateExternalAdapter(h); err != nil {
				warnAdapter(path, err)
				continue
			}
			if err := ValidateAdapters(append(registeredAdapters(), h)); err != nil {
				warnAdapter(path, err)
				continue
			}
			externalHarnesses = append(externalHarnesses, h)
		}
	})
}

func warnAdapter(path string, err error) {
	fmt.Fprintf(os.Stderr, "also: ignoring adapter %s: %v\n", path, err)
}

const maxAdapterSpecSize = 1 << 20

// AddAdapterFile installs a declarative harness spec from a local file or URL.
func AddAdapterFile(source string) (*Harness, string, error) {
	data, err := loadAdapterSource(source)
	if err != nil {
		return nil, "", err
	}
	var spec AdapterSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, "", fmt.Errorf("parse adapter spec: %w", err)
	}
	h, err := spec.Harness()
	if err != nil {
		return nil, "", err
	}
	ensureExternalAdapters()
	others := make([]*Harness, 0, len(registeredAdapters()))
	for _, cur := range harnesses {
		others = append(others, cur)
	}
	for _, cur := range externalHarnesses {
		if cur.ID != h.ID {
			others = append(others, cur)
		}
	}
	if err := validateExternalAdapter(h); err != nil {
		return nil, "", err
	}
	if err := ValidateAdapters(append(others, h)); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(AdapterDir(), 0o755); err != nil {
		return nil, "", err
	}
	dest := filepath.Join(AdapterDir(), h.ID+".json")
	if err := atomicWrite(dest, data, 0o644); err != nil {
		return nil, "", err
	}
	replaced := false
	for i, cur := range externalHarnesses {
		if cur.ID == h.ID {
			externalHarnesses[i] = h
			replaced = true
			break
		}
	}
	if !replaced {
		externalHarnesses = append(externalHarnesses, h)
	}
	return h, dest, nil
}

func loadAdapterSource(source string) ([]byte, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("adapter source is empty; pass a JSON file or URL")
	}
	var data []byte
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		client := &http.Client{Timeout: 20 * time.Second}
		resp, err := client.Get(source)
		if err != nil {
			return nil, fmt.Errorf("fetch adapter: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch adapter: %s returned %s", source, resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxAdapterSpecSize+1))
		if err != nil {
			return nil, fmt.Errorf("read adapter: %w", err)
		}
	} else {
		path := strings.TrimPrefix(source, "file://")
		abs, err := AbsPath(path)
		if err != nil {
			return nil, err
		}
		data, err = os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("read adapter: %w", err)
		}
	}
	if len(data) > maxAdapterSpecSize {
		return nil, fmt.Errorf("adapter spec is too large (max %d bytes)", maxAdapterSpecSize)
	}
	return data, nil
}

// RemoveAdapter deletes an external declarative adapter. Built-ins cannot be
// removed this way.
func RemoveAdapter(id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	ensureExternalAdapters()
	for _, h := range harnesses {
		if h.ID == id {
			return "", fmt.Errorf("%s is a built-in harness and cannot be removed", id)
		}
	}
	var target *Harness
	for i, h := range externalHarnesses {
		if h.ID == id {
			target = h
			externalHarnesses = append(externalHarnesses[:i], externalHarnesses[i+1:]...)
			break
		}
	}
	if target == nil {
		var ids []string
		for _, h := range externalHarnesses {
			ids = append(ids, h.ID)
		}
		return "", fmt.Errorf("no external harness %q; installed: %v", id, ids)
	}
	dest := filepath.Join(AdapterDir(), id+".json")
	if _, err := os.Stat(dest); err != nil {
		entries, _ := os.ReadDir(AdapterDir())
		for _, e := range entries {
			path := filepath.Join(AdapterDir(), e.Name())
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			var spec AdapterSpec
			if json.Unmarshal(data, &spec) == nil && spec.ID == id {
				dest = path
				break
			}
		}
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return dest, nil
}

func resetExternalAdapters() {
	externalHarnesses = nil
	externalAdaptersOnce = sync.Once{}
}

// ExternalAdapterIDs lists community adapters loaded from disk.
func ExternalAdapterIDs() []string {
	ensureExternalAdapters()
	var ids []string
	for _, h := range externalHarnesses {
		ids = append(ids, h.ID)
	}
	return ids
}

func IsExternalAdapter(id string) bool {
	for _, cur := range ExternalAdapterIDs() {
		if cur == id {
			return true
		}
	}
	return false
}
