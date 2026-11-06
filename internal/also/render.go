package also

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type RenderedFile struct {
	Dest      string
	Data      []byte
	Mode      fs.FileMode
	Component string // skill:<name> | command | agent | plugin | hook
	OriginRel string // path in the package origin that produced this file
}

type InstructionRender struct {
	File string
	Body []byte
}

type RenderPlan struct {
	Target         Target
	Source         *Harness
	Files          []RenderedFile
	Instr          *InstructionRender
	MCP            map[string]MCPServer
	Hooks          HookSet
	Settings       map[string]any
	MCPNative      bool
	SettingsNative bool
	Warnings       []string
	Skipped        []string
	ConfigFiles    []string
}

type skillRef struct {
	Name  string
	Root  string   // origin directory (empty for flat)
	Files []string // origin paths
	Flat  bool
}

type bundleParts struct {
	Instr    string
	Skills   []skillRef
	Commands []string
	Agents   []string
	Plugins  []string
	Hooks    []string
	Extras   []string
	Configs  []string
	Custom   map[string][]string
	Other    []string
	Warnings []string
}

func isMetaFile(rel string) bool {
	if strings.HasPrefix(rel, "docs/") || strings.HasPrefix(rel, ".github/") {
		return true
	}
	if strings.Contains(rel, "/") {
		return false
	}
	base := strings.ToLower(rel)
	for _, p := range []string{"readme", "license", "licence", "changelog", "contributing", "code_of_conduct"} {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	switch base {
	case ".gitignore", ".gitattributes", ".editorconfig", ".gitkeep":
		return true
	}
	return false
}

// ClassifyPackage maps an encoded origin tree back to harness components.
func ClassifyPackage(pkg *Package, h *Harness) (*bundleParts, error) {
	parts := &bundleParts{Custom: map[string][]string{}}
	roots := make([]string, 0, len(h.Roots))
	for _, r := range h.Roots {
		roots = append(roots, r)
	}
	// Longest root first so ".claude" wins over "".
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) > len(roots[j]) })

	configNames := h.ConfigNames()
	instructionCandidates := []string{}
	skillRoots := map[string]string{} // name -> origin dir
	flatSkills := map[string]string{} // name -> origin file
	claimedSkillFiles := map[string]bool{}
	var duplicates []string

	for _, rel := range pkg.Paths() {
		if strings.HasPrefix(rel, BundleLayerPrefix+"/") {
			continue
		}
		rest := rel
		for _, r := range roots {
			if r == "" {
				continue
			}
			if strings.HasPrefix(rel, r+"/") {
				rest = strings.TrimPrefix(rel, r+"/")
				break
			}
		}
		if rest == h.Bundle.Instr {
			instructionCandidates = append(instructionCandidates, rel)
			continue
		}
		under := func(dir string) (string, bool) {
			if dir == "" {
				return "", false
			}
			if strings.HasPrefix(rest, dir+"/") {
				return strings.TrimPrefix(rest, dir+"/"), true
			}
			return "", false
		}
		if rem, ok := under(h.Bundle.Skills); ok {
			if filepath.Base(rem) == "SKILL.md" {
				name := path.Base(path.Dir(rel))
				if prev, exists := skillRoots[name]; exists && prev != path.Dir(rel) {
					duplicates = append(duplicates, fmt.Sprintf("skill %q: %s and %s", name, prev, path.Dir(rel)))
				} else {
					skillRoots[name] = path.Dir(rel)
				}
			} else if !strings.Contains(rem, "/") && strings.HasSuffix(rem, ".md") {
				name := strings.TrimSuffix(rem, ".md")
				if prev, exists := flatSkills[name]; exists && prev != rel {
					duplicates = append(duplicates, fmt.Sprintf("skill %q: %s and %s", name, prev, rel))
				} else {
					flatSkills[name] = rel
				}
			}
			continue
		}
		if rem, ok := under(h.Bundle.Commands); ok {
			if !strings.HasSuffix(rem, ".md") {
				parts.Other = append(parts.Other, rel)
				continue
			}
			parts.Commands = append(parts.Commands, rel)
			continue
		}
		if rem, ok := under(h.Bundle.Agents); ok {
			if !strings.HasSuffix(rem, ".md") {
				parts.Other = append(parts.Other, rel)
				continue
			}
			parts.Agents = append(parts.Agents, rel)
			continue
		}
		if rem, ok := under(h.Bundle.Plugins); ok {
			_ = rem
			parts.Plugins = append(parts.Plugins, rel)
			continue
		}
		if rem, ok := under(h.Bundle.Hooks); ok {
			_ = rem
			parts.Hooks = append(parts.Hooks, rel)
			continue
		}
		custom := false
		for _, c := range h.Components {
			rem, ok := under(c.BundleDir)
			if !ok {
				continue
			}
			if len(c.Extensions) > 0 && !hasExtension(rem, c.Extensions) {
				continue
			}
			parts.Custom[c.Kind] = append(parts.Custom[c.Kind], rel)
			custom = true
			break
		}
		if custom {
			continue
		}
		extra := false
		for _, d := range h.ExtraDirs {
			if _, ok := under(d); ok {
				parts.Extras = append(parts.Extras, rel)
				extra = true
				break
			}
		}
		if extra {
			continue
		}
		if !strings.Contains(rest, "/") && configNames[rest] {
			parts.Configs = append(parts.Configs, rel)
			continue
		}
		if isMetaFile(rel) {
			continue
		}
		parts.Other = append(parts.Other, rel)
	}
	if len(duplicates) > 0 {
		return nil, fmt.Errorf("package has duplicate skill names:\n  %s", strings.Join(duplicates, "\n  "))
	}
	if len(instructionCandidates) > 1 {
		var contents []string
		for _, p := range instructionCandidates {
			contents = append(contents, string(pkg.Files[p].Data))
		}
		for _, c := range contents[1:] {
			if c != contents[0] {
				return nil, fmt.Errorf("package has multiple different instruction files: %s", strings.Join(instructionCandidates, ", "))
			}
		}
	}
	if len(instructionCandidates) > 0 {
		parts.Instr = instructionCandidates[0]
	}

	// Resolve outermost skill dirs; a nested SKILL.md inside a skill is a resource.
	var dirNames []string
	for name, dir := range skillRoots {
		dirNames = append(dirNames, name)
		_ = dir
	}
	sort.Slice(dirNames, func(i, j int) bool {
		return strings.Count(skillRoots[dirNames[i]], "/") < strings.Count(skillRoots[dirNames[j]], "/")
	})
	addedRoots := []string{}
	for _, name := range dirNames {
		root := skillRoots[name]
		inside := false
		for _, ar := range addedRoots {
			if strings.HasPrefix(root+"/", ar+"/") {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		var files []string
		for _, rel := range pkg.Paths() {
			if strings.HasPrefix(rel, root+"/") {
				files = append(files, rel)
				claimedSkillFiles[rel] = true
			}
		}
		parts.Skills = append(parts.Skills, skillRef{Name: name, Root: root, Files: files})
		addedRoots = append(addedRoots, root)
	}
	var flatNames []string
	for name := range flatSkills {
		flatNames = append(flatNames, name)
	}
	sort.Strings(flatNames)
	for _, name := range flatNames {
		rel := flatSkills[name]
		for _, s := range parts.Skills {
			if s.Name == name {
				return nil, fmt.Errorf("skill %q is defined both as %s and as a directory", name, rel)
			}
		}
		parts.Skills = append(parts.Skills, skillRef{Name: name, Files: []string{rel}, Flat: true})
		claimedSkillFiles[rel] = true
	}
	// Loose files under a skills directory are not skills.
	for _, rel := range pkg.Paths() {
		if claimedSkillFiles[rel] {
			continue
		}
		for _, sk := range h.Roots {
			dir := h.Bundle.Skills
			if sk != "" {
				dir = sk + "/" + h.Bundle.Skills
			}
			if strings.HasPrefix(rel, dir+"/") {
				parts.Other = append(parts.Other, rel)
				break
			}
		}
	}
	sort.Slice(parts.Skills, func(a, b int) bool { return parts.Skills[a].Name < parts.Skills[b].Name })
	sort.Strings(parts.Commands)
	sort.Strings(parts.Agents)
	sort.Strings(parts.Plugins)
	sort.Strings(parts.Hooks)
	sort.Strings(parts.Extras)
	sort.Strings(parts.Other)
	sort.Strings(parts.Configs)
	for _, rels := range parts.Custom {
		sort.Strings(rels)
	}
	return parts, nil
}

func RenderPackage(pkg *Package, source *Harness, target Target) (*RenderPlan, error) {
	return RenderPackageSelection(pkg, source, target, nil)
}

// RenderPackageSelection renders only the selected parts. An empty selection
// installs the whole package.
func RenderPackageSelection(pkg *Package, source *Harness, target Target, parts []string) (*RenderPlan, error) {
	sel, err := NewSelection(parts)
	if err != nil {
		return nil, err
	}
	plan, err := renderPackageMainSel(pkg, source, target, sel)
	if err != nil {
		return nil, err
	}
	layers := splitLayers(pkg.Files)
	layer := layers[target.Harness.ID]
	if layer == nil || source.ID == target.Harness.ID {
		return plan, nil
	}
	layerPkg := &Package{
		Meta:  PackageMeta{Schema: SchemaPkg, Name: pkg.Meta.Name, SourceHarness: target.Harness.ID},
		Files: layer,
	}
	layerPlan, err := renderPackageMainSel(layerPkg, target.Harness, target, sel)
	if err != nil {
		return nil, fmt.Errorf("render preserved %s layer: %w", target.Harness.ID, err)
	}
	merged := *plan
	merged.Warnings = append(append([]string{}, plan.Warnings...), layerPlan.Warnings...)
	merged.Skipped = append(append([]string{}, plan.Skipped...), layerPlan.Skipped...)
	if merged.Instr == nil && layerPlan.Instr != nil {
		merged.Instr = layerPlan.Instr
	}
	if len(layerPlan.ConfigFiles) > 0 {
		merged.MCP = layerPlan.MCP
		merged.Hooks = layerPlan.Hooks
		merged.Settings = layerPlan.Settings
		merged.MCPNative = true
		merged.SettingsNative = true
	}
	have := map[string]int{}
	for i, f := range merged.Files {
		have[f.Dest] = i
	}
	baseline := loadLayerManifest(pkg.Files)[target.Harness.ID]
	for _, f := range layerPlan.Files {
		if i, ok := have[f.Dest]; ok {
			main := &merged.Files[i]
			// Prefer the native layer only when the intermediate harness's
			// origin file still matches what also installed. A modified copy in
			// that harness wins. Compare the source file, not the re-rendered
			// output, because translation is not the identity.
			unchanged := false
			if main.OriginRel != "" {
				if of := pkg.Files[main.OriginRel]; of != nil {
					unchanged = baseline[f.OriginRel] == ShortHash(of.Data, 64)
				}
			} else if sha, ok := baseline[f.OriginRel]; ok {
				unchanged = sha == ShortHash(main.Data, 64)
			}
			if unchanged {
				main.Data = f.Data
				main.Mode = f.Mode
				main.Component = f.Component
				main.OriginRel = f.OriginRel
			}
			continue
		}
		merged.Files = append(merged.Files, f)
		have[f.Dest] = len(merged.Files) - 1
	}
	merged.ConfigFiles = append(append([]string{}, plan.ConfigFiles...), layerPlan.ConfigFiles...)
	return &merged, nil
}

func renderPackageMain(pkg *Package, source *Harness, target Target) (*RenderPlan, error) {
	return renderPackageMainSel(pkg, source, target, nil)
}

func renderPackageMainSel(pkg *Package, source *Harness, target Target, sel *Selection) (*RenderPlan, error) {
	parts, err := ClassifyPackage(pkg, source)
	if err != nil {
		return nil, err
	}
	filterPartsBySelection(parts, source, sel)
	plan := &RenderPlan{Target: target, Source: source, MCP: map[string]MCPServer{}, Hooks: HookSet{}}
	plan.ConfigFiles = parts.Configs
	add := func(dest string, rel string, component string) error {
		if err := SafeRel(rel); err != nil {
			return err
		}
		f := pkg.Files[rel]
		if f == nil {
			return fmt.Errorf("internal: missing file %s", rel)
		}
		plan.Files = append(plan.Files, RenderedFile{Dest: dest, Data: f.Data, Mode: f.Mode, Component: component, OriginRel: rel})
		return nil
	}

	if parts.Instr != "" {
		f := pkg.Files[parts.Instr]
		plan.Instr = &InstructionRender{File: target.Instr, Body: f.Data}
	}
	for _, sk := range parts.Skills {
		if target.Skills == "" {
			plan.Skipped = append(plan.Skipped, "skill:"+sk.Name)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("skills are not supported by %s; skill %q skipped", target.Harness.Display, sk.Name))
			continue
		}
		if sk.Flat {
			rel := sk.Files[0]
			if err := add(filepath.Join(target.Skills, sk.Name, "SKILL.md"), rel, "skill:"+sk.Name); err != nil {
				return nil, err
			}
			continue
		}
		root := sk.Root
		for _, rel := range sk.Files {
			sub := strings.TrimPrefix(rel, root+"/")
			if err := add(filepath.Join(target.Skills, sk.Name, filepath.FromSlash(sub)), rel, "skill:"+sk.Name); err != nil {
				return nil, err
			}
		}
	}
	for _, rel := range parts.Commands {
		if !target.Supports("commands") || target.Commands == "" {
			plan.Skipped = append(plan.Skipped, "command:"+rel)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("commands are not supported by %s; %s skipped", target.Harness.Display, rel))
			continue
		}
		sub := trimRootPrefix(rel, source)
		sub = trimComponentPrefix(sub, source.Bundle.Commands)
		data := pkg.Files[rel].Data
		if source.ID != target.Harness.ID {
			converted, warns := ConvertCommand(data, source.ID, target.Harness.ID, sub)
			data = converted
			plan.Warnings = append(plan.Warnings, warns...)
		}
		dest := filepath.Join(target.Commands, filepath.FromSlash(sub))
		plan.Files = append(plan.Files, RenderedFile{Dest: dest, Data: data, Mode: pkg.Files[rel].Mode, Component: "command", OriginRel: rel})
	}
	for _, rel := range parts.Agents {
		if !target.Supports("agents") || target.Agents == "" {
			plan.Skipped = append(plan.Skipped, "agent:"+rel)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("agents are not supported by %s; %s skipped", target.Harness.Display, rel))
			continue
		}
		sub := trimRootPrefix(rel, source)
		sub = trimComponentPrefix(sub, source.Bundle.Agents)
		dest := filepath.Join(target.Agents, filepath.FromSlash(sub))
		data := pkg.Files[rel].Data
		converted, warns, err := ConvertAgent(data, source.ID, target.Harness.ID, sub)
		if err != nil {
			return nil, fmt.Errorf("convert agent %s: %w", rel, err)
		}
		plan.Warnings = append(plan.Warnings, warns...)
		plan.Files = append(plan.Files, RenderedFile{Dest: dest, Data: converted, Mode: pkg.Files[rel].Mode, Component: "agent", OriginRel: rel})
	}
	for _, rel := range parts.Plugins {
		if !target.Supports("plugins") || target.Plugins == "" || source.Plugins.Format == "" || source.Plugins.Format != target.Harness.Plugins.Format {
			plan.Skipped = append(plan.Skipped, "plugin:"+rel)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("plugin %s cannot be translated to %s; skipped", rel, target.Harness.Display))
			continue
		}
		sub := trimRootPrefix(rel, source)
		sub = trimComponentPrefix(sub, source.Bundle.Plugins)
		if err := add(filepath.Join(target.Plugins, filepath.FromSlash(sub)), rel, "plugin"); err != nil {
			return nil, err
		}
	}
	for _, rel := range parts.Hooks {
		if target.Hooks == "" || len(source.Hooks.Configs) == 0 {
			plan.Skipped = append(plan.Skipped, "hook-script:"+rel)
			continue
		}
		sub := trimRootPrefix(rel, source)
		sub = trimComponentPrefix(sub, source.Bundle.Hooks)
		if err := add(filepath.Join(target.Hooks, filepath.FromSlash(sub)), rel, "hook"); err != nil {
			return nil, err
		}
	}
	for _, c := range source.Components {
		rels := parts.Custom[c.Kind]
		if len(rels) == 0 {
			continue
		}
		tc := target.Harness.Component(c.Kind)
		dir := target.ComponentDir(c.Kind)
		skip := dir == "" || target.Harness.ID != source.ID && c.Opaque
		if !skip && c.Format != "" {
			skip = tc == nil || tc.Format != c.Format
		}
		if skip {
			for _, rel := range rels {
				plan.Skipped = append(plan.Skipped, c.Kind+":"+rel)
			}
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s components cannot be installed into %s; skipped", c.Kind, target.Harness.Display))
			continue
		}
		for _, rel := range rels {
			sub := trimRootPrefix(rel, source)
			sub = trimComponentPrefix(sub, c.BundleDir)
			data := pkg.Files[rel].Data
			if c.Convert != nil && source.ID != target.Harness.ID {
				converted, warns, err := c.Convert(data, source, target.Harness, sub)
				if err != nil {
					return nil, fmt.Errorf("convert %s/%s: %w", c.Kind, rel, err)
				}
				plan.Warnings = append(plan.Warnings, warns...)
				data = converted
			}
			dest := filepath.Join(dir, filepath.FromSlash(sub))
			plan.Files = append(plan.Files, RenderedFile{Dest: dest, Data: data, Mode: pkg.Files[rel].Mode, Component: c.Kind, OriginRel: rel})
		}
	}
	if len(parts.Commands) > 0 && source.ID != target.Harness.ID && target.Supports("commands") {
		plan.Warnings = append(plan.Warnings, "command bodies are copied verbatim; argument placeholders and harness-specific variables may differ in the target harness")
	}
	for _, rel := range parts.Extras {
		if source.ID != target.Harness.ID || target.ExtraDir == "" {
			plan.Skipped = append(plan.Skipped, "extra:"+rel)
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is harness-specific and was preserved in the package, but not installed into %s", rel, target.Harness.Display))
			continue
		}
		sub := trimRootPrefix(rel, source)
		if err := add(filepath.Join(target.ExtraDir, filepath.FromSlash(sub)), rel, "extra"); err != nil {
			return nil, err
		}
	}
	if len(parts.Configs) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("config files contribute MCP, hooks and portable settings; other keys in %s are preserved but not installed", strings.Join(parts.Configs, ", ")))
	}
	if len(parts.Other) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("not installed (not a portable component): %s", strings.Join(parts.Other, ", ")))
	}
	plan.Warnings = append(plan.Warnings, parts.Warnings...)

	// MCP and hooks.
	roots := source.BundleRootsFromFiles(pkg.Files)
	servers, warn, err := ExtractBundleMCP(source, pkg.Files, roots)
	if err != nil {
		return nil, err
	}
	plan.Warnings = append(plan.Warnings, warn...)
	if !sel.IsAll() {
		for name := range servers {
			if !sel.Allows("mcp", name) {
				delete(servers, name)
			}
		}
	}
	plan.MCP = servers
	hooks, warn, err := ExtractBundleHooks(source, pkg.Files, roots)
	if err != nil {
		return nil, err
	}
	plan.Warnings = append(plan.Warnings, warn...)
	if !sel.IsAll() && !sel.Wants("hooks") {
		hooks = HookSet{}
	}
	if len(hooks) > 0 {
		translated, warn := RenderHooks(target.Harness, source, hooks)
		plan.Warnings = append(plan.Warnings, warn...)
		plan.Hooks = translated
	}
	settings, warn, err := ExtractBundleSettings(source, pkg.Files, roots)
	if err != nil {
		return nil, err
	}
	plan.Warnings = append(plan.Warnings, warn...)
	if !sel.IsAll() {
		for key := range settings {
			if !sel.Allows("settings", key) {
				delete(settings, key)
			}
		}
	}
	plan.Settings = settings
	if len(settings) > 0 && source.ID != target.Harness.ID {
		var names []string
		for k := range settings {
			names = append(names, k)
		}
		sort.Strings(names)
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("settings %s are preserved in the package but not installed cross-harness; review them manually", strings.Join(names, ", ")))
	}
	if target.Harness.MCP.Format == MCPNone || target.MCP == "" {
		if len(servers) > 0 {
			plan.Warnings = append(plan.Warnings, "MCP is not supported for this harness; MCP servers skipped")
		}
		plan.MCP = map[string]MCPServer{}
	}
	if target.Scope == "project" {
		for _, hint := range target.Harness.InstallHints {
			triggered := true
			if hint.WhenMCP || hint.WhenSettings {
				triggered = (hint.WhenMCP && len(plan.MCP) > 0) || (hint.WhenSettings && len(plan.Settings) > 0)
			}
			if triggered {
				plan.Warnings = append(plan.Warnings, hint.Text)
			}
		}
	}
	return plan, nil
}

func trimRootPrefix(rel string, h *Harness) string {
	for _, r := range h.Roots {
		if r != "" && strings.HasPrefix(rel, r+"/") {
			return strings.TrimPrefix(rel, r+"/")
		}
	}
	return rel
}

func trimComponentPrefix(rel, dir string) string {
	if dir == "" {
		return rel
	}
	if strings.HasPrefix(rel, dir+"/") {
		return strings.TrimPrefix(rel, dir+"/")
	}
	return rel
}
