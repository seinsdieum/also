package also

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Selection is a set of component parts requested for a partial install,
// for example "skill:review" or "commands". An empty selection means all parts.
type Selection struct {
	all         bool
	kinds       map[string]bool
	items       map[string]map[string]bool
	exceptKinds map[string]bool
	exceptItems map[string]map[string]bool
	hasExcept   bool
}

var standardKindAliases = map[string]string{
	"instruction": "instructions", "instructions": "instructions", "instr": "instructions",
	"skill": "skills", "skills": "skills",
	"command": "commands", "commands": "commands",
	"agent": "agents", "agents": "agents",
	"plugin": "plugins", "plugins": "plugins",
	"hook": "hooks", "hooks": "hooks",
	"mcp": "mcp", "mcps": "mcp", "server": "mcp", "servers": "mcp",
	"setting": "settings", "settings": "settings",
	"extra": "extras", "extras": "extras",
}

// CanonicalComponentKind resolves a user-facing kind alias to the kind used by
// the standard system or a registered adapter component.
func CanonicalComponentKind(kind string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(kind))
	if k == "" {
		return "", false
	}
	if c, ok := standardKindAliases[k]; ok {
		return c, true
	}
	for _, h := range Harnesses() {
		for _, c := range h.Components {
			if k == c.Kind || strings.TrimSuffix(k, "s") == c.Kind {
				return c.Kind, true
			}
		}
	}
	return "", false
}

// NewSelection parses selectors such as "skill:review", "skills", or
// "mcp:github". No selectors or "all" means every part.
func NewSelection(parts []string) (*Selection, error) {
	return NewSelectionWithExclusions(parts, nil)
}

// NewSelectionWithExclusions parses include selectors plus exclusions. An empty
// include list with exclusions means "everything except these".
func NewSelectionWithExclusions(parts, except []string) (*Selection, error) {
	s := &Selection{
		kinds: map[string]bool{}, items: map[string]map[string]bool{},
		exceptKinds: map[string]bool{}, exceptItems: map[string]map[string]bool{},
	}
	all, err := addSelectors(s, parts, false)
	if err != nil {
		return nil, err
	}
	if all {
		s.all = true
	}
	if len(except) > 0 {
		s.hasExcept = true
		if _, err := addSelectors(s, except, true); err != nil {
			return nil, err
		}
	}
	if len(parts) == 0 && len(except) == 0 {
		s.all = true
	}
	if len(parts) == 0 && len(except) > 0 {
		s.all = true
	}
	if len(s.kinds) == 0 && len(s.items) == 0 && !s.hasExcept {
		s.all = true
	}
	return s, nil
}

func addSelectors(s *Selection, raw []string, except bool) (bool, error) {
	all := false
	for _, selector := range raw {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		if strings.EqualFold(selector, "all") {
			all = true
			continue
		}
		kind, name := selector, ""
		if i := strings.Index(selector, ":"); i >= 0 {
			kind, name = selector[:i], strings.TrimSpace(selector[i+1:])
		}
		canon, ok := CanonicalComponentKind(kind)
		if !ok {
			return false, fmt.Errorf("unknown component kind %q; try: skill, command, agent, plugin, hook, mcp, settings, instructions", kind)
		}
		if name == "" {
			if except {
				s.exceptKinds[canon] = true
			} else {
				s.kinds[canon] = true
			}
			continue
		}
		target := s.items
		if except {
			target = s.exceptItems
		}
		if target[canon] == nil {
			target[canon] = map[string]bool{}
		}
		target[canon][name] = true
	}
	return all, nil
}

func (s *Selection) IsAll() bool     { return s == nil || (s.all && !s.hasExcept) }
func (s *Selection) Filtering() bool { return s != nil && (!s.all || s.hasExcept) }

func nameMatches(names map[string]bool, name string) bool {
	if len(names) == 0 || name == "" {
		return false
	}
	candidates := []string{name, strings.TrimSuffix(name, path.Ext(name)), filepath.Base(name), strings.TrimSuffix(filepath.Base(name), path.Ext(name))}
	for _, c := range candidates {
		if names[c] {
			return true
		}
	}
	return false
}

func (s *Selection) excluded(kind, name string) bool {
	if s == nil || !s.hasExcept {
		return false
	}
	canon, ok := CanonicalComponentKind(kind)
	if !ok {
		canon = strings.ToLower(strings.TrimSpace(kind))
	}
	if s.exceptKinds[canon] {
		return true
	}
	return nameMatches(s.exceptItems[canon], name)
}

// Wants reports whether a kind was selected at all, either whole or by name.
func (s *Selection) Wants(kind string) bool {
	if s == nil {
		return true
	}
	canon, ok := CanonicalComponentKind(kind)
	if !ok {
		canon = kind
	}
	if s.exceptKinds[canon] {
		return false
	}
	if s.all {
		return true
	}
	return s.kinds[canon] || len(s.items[canon]) > 0
}

// Allows reports whether a named part of a kind is selected. An empty name
// selects the whole kind.
func (s *Selection) Allows(kind, name string) bool {
	if s == nil || s.excluded(kind, name) {
		return false
	}
	if s.all {
		return true
	}
	canon, ok := CanonicalComponentKind(kind)
	if !ok {
		canon = strings.ToLower(strings.TrimSpace(kind))
	}
	if s.kinds[canon] {
		return true
	}
	return nameMatches(s.items[canon], name)
}

func filterPartsBySelection(parts *bundleParts, h *Harness, sel *Selection) {
	if !sel.Filtering() {
		return
	}
	if parts.Instr != "" && !sel.Allows("instructions", "") {
		parts.Instr = ""
	}
	parts.Skills = filterSkills(parts.Skills, sel)
	parts.Commands = filterRels(parts.Commands, h, h.Bundle.Commands, "commands", sel)
	parts.Agents = filterRels(parts.Agents, h, h.Bundle.Agents, "agents", sel)
	parts.Plugins = filterRels(parts.Plugins, h, h.Bundle.Plugins, "plugins", sel)
	parts.Hooks = filterRels(parts.Hooks, h, h.Bundle.Hooks, "hooks", sel)
	parts.Extras = filterRels(parts.Extras, h, "", "extras", sel)
	if !sel.Wants("mcp") && !sel.Wants("hooks") && !sel.Wants("settings") {
		parts.Configs = nil
	}
	for kind, rels := range parts.Custom {
		var kept []string
		for _, rel := range rels {
			c := h.Component(kind)
			dir := ""
			if c != nil {
				dir = c.BundleDir
			}
			if sel.Allows(kind, trimComponentPrefix(trimRootPrefix(rel, h), dir)) {
				kept = append(kept, rel)
			}
		}
		if len(kept) == 0 {
			delete(parts.Custom, kind)
		} else {
			parts.Custom[kind] = kept
		}
	}
}

func filterSkills(in []skillRef, sel *Selection) []skillRef {
	var out []skillRef
	for _, sk := range in {
		if sel.Allows("skills", sk.Name) {
			out = append(out, sk)
		}
	}
	return out
}

func filterRels(in []string, h *Harness, dir, kind string, sel *Selection) []string {
	var out []string
	for _, rel := range in {
		name := trimRootPrefix(rel, h)
		name = trimComponentPrefix(name, dir)
		if sel.Allows(kind, name) {
			out = append(out, rel)
		}
	}
	return out
}

// AvailableParts lists selectors that can be installed from a package. It is
// used to explain a partial install that matched nothing.
func AvailableParts(pkg *Package, source *Harness) []string {
	parts, err := ClassifyPackage(pkg, source)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	add := func(kind, name string) {
		key := kind
		if name != "" {
			key += ":" + name
		}
		seen[key] = true
	}
	if parts.Instr != "" {
		add("instructions", "")
	}
	for _, sk := range parts.Skills {
		add("skills", sk.Name)
	}
	for _, rel := range parts.Commands {
		add("commands", trimComponentPrefix(trimRootPrefix(rel, source), source.Bundle.Commands))
	}
	for _, rel := range parts.Agents {
		add("agents", trimComponentPrefix(trimRootPrefix(rel, source), source.Bundle.Agents))
	}
	for _, rel := range parts.Plugins {
		add("plugins", trimComponentPrefix(trimRootPrefix(rel, source), source.Bundle.Plugins))
	}
	for _, rel := range parts.Hooks {
		add("hooks", trimComponentPrefix(trimRootPrefix(rel, source), source.Bundle.Hooks))
	}
	for kind, rels := range parts.Custom {
		c := source.Component(kind)
		dir := ""
		if c != nil {
			dir = c.BundleDir
		}
		for _, rel := range rels {
			add(kind, trimComponentPrefix(trimRootPrefix(rel, source), dir))
		}
	}
	for _, rel := range parts.Extras {
		add("extras", trimRootPrefix(rel, source))
	}
	roots := source.BundleRootsFromFiles(pkg.Files)
	if mcp, _, err := ExtractBundleMCP(source, pkg.Files, roots); err == nil {
		for name := range mcp {
			add("mcp", name)
		}
	}
	if hooks, _, err := ExtractBundleHooks(source, pkg.Files, roots); err == nil {
		for event := range hooks {
			add("hooks", event)
		}
	}
	if settings, _, err := ExtractBundleSettings(source, pkg.Files, roots); err == nil {
		for key := range settings {
			add("settings", key)
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
