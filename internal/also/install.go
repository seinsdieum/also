package also

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type InstallOptions struct {
	Force      bool
	WithConfig bool
	// Parts selects individual components for a partial install. Empty = all.
	Parts []string
	// OldHarness is set by reshape to reuse the install slot of another harness.
	OldHarness string
}

type Plan struct {
	Install     Install
	Target      Target
	Render      *RenderPlan
	NewFiles    []RenderedFile
	RemoveFiles []InstalledFile
	Block       *BlockEdit
	BlockBody   []byte
	RemoveBlock *BlockEdit
	Edits       []ConfigEdit
	Warnings    []string
	Skipped     []string
	// OldInstallID is removed when a legacy slot id or a reshape changes the
	// install id.
	OldInstallID string
}

const blockBeginPrefix = "<!-- also:begin "
const blockEndPrefix = "<!-- also:end "

func blockBegin(id string) string { return blockBeginPrefix + id + " -->" }
func blockEnd(id string) string   { return blockEndPrefix + id + " -->" }

func findBlock(content, id string) (start, bodyStart, bodyEnd, end int, ok bool) {
	begin := blockBegin(id)
	endMarker := blockEnd(id)
	bi := strings.Index(content, begin)
	if bi < 0 {
		return 0, 0, 0, 0, false
	}
	ei := strings.Index(content[bi:], endMarker)
	if ei < 0 {
		return 0, 0, 0, 0, false
	}
	ei += bi
	start = bi
	if ls := strings.LastIndexByte(content[:bi], '\n'); ls >= 0 {
		start = ls + 1
	}
	bodyStart = bi + len(begin)
	bodyEnd = ei
	end = ei + len(endMarker)
	if nl := strings.IndexByte(content[end:], '\n'); nl >= 0 {
		end += nl + 1
	}
	return start, bodyStart, bodyEnd, end, true
}

func blockBodyHash(body []byte) string {
	return ShortHash(bytes.Trim(body, "\r\n \t"), 16)
}

func renderBlock(id string, body []byte) string {
	return blockBegin(id) + "\n" + strings.Trim(string(body), "\r\n") + "\n" + blockEnd(id) + "\n"
}

func containsMarker(body []byte) bool {
	return bytes.Contains(body, []byte(blockBeginPrefix)) || bytes.Contains(body, []byte(blockEndPrefix))
}

func BuildPlan(st *State, set *SetRepo, pkg *Package, version VersionInfo, harnessID, scope, project string, opts InstallOptions) (*Plan, error) {
	source, ok := ResolveHarness(pkg.Meta.SourceHarness)
	if !ok {
		return nil, fmt.Errorf("package %s uses unknown source harness %q", pkg.Meta.Name, pkg.Meta.SourceHarness)
	}
	targetH, ok := ResolveHarness(harnessID)
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", harnessID)
	}
	if scope == "project" {
		abs, err := AbsPath(project)
		if err != nil {
			return nil, err
		}
		project = abs
		if st, err := os.Stat(project); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("project directory does not exist: %s", project)
		}
	} else {
		project = ""
	}
	target := targetH.TargetPath(project)
	render, err := RenderPackageSelection(pkg, source, target, opts.Parts)
	if err != nil {
		return nil, err
	}
	id := InstallID(set.Name, pkg.Meta.Name, targetH.ID, scope, project)
	oldID := id
	if opts.OldHarness != "" && opts.OldHarness != targetH.ID {
		oldID = InstallID(set.Name, pkg.Meta.Name, opts.OldHarness, scope, project)
	}
	old := st.FindByID(oldID)
	if old == nil {
		// Adopt state written before harnesses were part of the slot id.
		candidate := st.FindByID(legacyInstallID(set.Name, pkg.Meta.Name, scope, project))
		if candidate != nil && candidate.Harness == sourceHarnessForOld(opts, targetH.ID) {
			old = candidate
		}
	}
	plan := &Plan{Target: target, Render: render}
	if old != nil && old.ID != id {
		plan.OldInstallID = old.ID
	}
	plan.Warnings = append(plan.Warnings, render.Warnings...)
	plan.Skipped = append(plan.Skipped, render.Skipped...)

	inst := Install{
		ID: id, Set: set.Name, SetRemote: set.Cfg.Remote, Package: pkg.Meta.Name,
		Version: version.Version, Label: version.Label, SourceHarness: source.ID,
		Harness: targetH.ID, Scope: scope, Project: project, Root: target.Root,
		WithConfig: opts.WithConfig, Parts: UniqueStrings(opts.Parts), InstalledAt: time.Now().UTC(),
	}

	// ---- regular files ----
	destSet := map[string]bool{}
	for _, f := range render.Files {
		if !fileAllowedRoot(f.Dest, target) {
			return nil, fmt.Errorf("refusing to write outside the target harness directory: %s", f.Dest)
		}
		if destSet[f.Dest] {
			return nil, fmt.Errorf("package produces the same file twice: %s", f.Dest)
		}
		destSet[f.Dest] = true
		if err := checkFileConflict(st, f.Dest, id, opts.Force); err != nil {
			return nil, err
		}
		plan.NewFiles = append(plan.NewFiles, f)
		inst.Files = append(inst.Files, InstalledFile{Path: f.Dest, SHA: ShortHash(f.Data, 64), Mode: fmt.Sprintf("%04o", f.Mode.Perm())})
	}
	if old != nil {
		for _, of := range old.Files {
			if destSet[of.Path] {
				continue
			}
			safe, warn, err := checkRemove(st, of, id, plan.OldInstallID, opts.Force)
			if err != nil {
				return nil, err
			}
			if warn != "" {
				plan.Warnings = append(plan.Warnings, warn)
			}
			if safe {
				plan.RemoveFiles = append(plan.RemoveFiles, of)
			}
		}
	}

	// ---- instructions block ----
	if render.Instr != nil {
		if render.Instr.File == "" {
			return nil, fmt.Errorf("harness %s has no instructions file", targetH.Display)
		}
		body := bytes.Trim(render.Instr.Body, "\r\n")
		if containsMarker(body) {
			return nil, fmt.Errorf("instructions contain also markers; remove them from the package")
		}
		bid := set.Name + "/" + pkg.Meta.Name + "/" + targetH.ID
		if old != nil && old.Block != nil && old.Block.File == render.Instr.File {
			// Keep the recorded id on upgrades so an existing managed block is
			// replaced in place instead of leaving a second one behind.
			bid = old.Block.ID
		}
		existing, err := os.ReadFile(render.Instr.File)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if !exists {
			existing = nil
		}
		if start, bs, be, _, found := findBlock(string(existing), bid); found {
			_ = start
			oldBody := existing[bs:be]
			oldHash := ""
			if old != nil && old.Block != nil {
				oldHash = old.Block.SHA
			}
			if oldHash == "" || blockBodyHash(oldBody) != oldHash {
				if !opts.Force {
					return nil, fmt.Errorf("instructions block %q in %s was modified by you; use --force to replace it", bid, render.Instr.File)
				}
			}
		}
		if old != nil && old.Block != nil && old.Block.File != render.Instr.File {
			plan.RemoveBlock = old.Block
		}
		createdFile := !exists
		if old != nil && old.Block != nil && old.Block.File == render.Instr.File {
			createdFile = old.Block.CreatedFile
		}
		plan.Block = &BlockEdit{File: render.Instr.File, ID: bid, SHA: blockBodyHash(body), CreatedFile: createdFile}
		plan.BlockBody = body
	} else if old != nil && old.Block != nil {
		plan.RemoveBlock = old.Block
	}

	// ---- MCP ----
	if !opts.WithConfig {
		if len(render.MCP) > 0 || len(render.Hooks) > 0 {
			plan.Warnings = append(plan.Warnings, "MCP and hooks were skipped (--no-config)")
		}
	} else {
		if len(render.MCP) > 0 {
			mcpSource := source
			if render.MCPNative {
				mcpSource = targetH
			}
			rendered, warns := RenderMCP(targetH, mcpSource, render.MCP)
			plan.Warnings = append(plan.Warnings, warns...)
			if !configPathAllowed(render.MCPPath(), target) {
				return nil, fmt.Errorf("adapter %s points MCP config outside the project root: %s", targetH.ID, render.MCPPath())
			}
			codec, ok := mcpCodec(targetH.MCP.Format)
			if !ok {
				return nil, fmt.Errorf("MCP format %q has no codec", targetH.MCP.Format)
			}
			for _, r := range rendered {
				edit, warn, err := codec.Plan(st, old, render.MCPPath(), r.Name, r, opts.Force)
				if err != nil {
					return nil, err
				}
				if warn != "" {
					plan.Warnings = append(plan.Warnings, warn)
				}
				if edit != nil {
					plan.Edits = append(plan.Edits, *edit)
				}
			}
		}
		if len(render.Hooks) > 0 {
			file, rootKey := hooksConfigPath(target)
			if file == "" {
				plan.Warnings = append(plan.Warnings, "hooks cannot be installed for this harness; skipped")
			} else {
				if !configPathAllowed(file, target) {
					return nil, fmt.Errorf("adapter %s points hooks config outside the project root: %s", targetH.ID, file)
				}
				events := make([]string, 0, len(render.Hooks))
				for ev := range render.Hooks {
					events = append(events, ev)
				}
				sort.Strings(events)
				for _, ev := range events {
					for _, group := range render.Hooks[ev] {
						path := []string{rootKey, ev}
						edit, added, err := planJSONAppend(file, path, group, old, opts.Force)
						if err != nil {
							return nil, err
						}
						if added {
							plan.Edits = append(plan.Edits, *edit)
						}
					}
				}
			}
		}
		if len(render.Settings) > 0 && (source.ID == targetH.ID || render.SettingsNative) {
			file := SettingsTargetFile(target)
			if file == "" {
				plan.Warnings = append(plan.Warnings, "settings keys were preserved in the package but this harness has no mergeable settings file")
			} else {
				if !configPathAllowed(file, target) {
					return nil, fmt.Errorf("adapter %s points settings outside the project root: %s", targetH.ID, file)
				}
				keys := make([]string, 0, len(render.Settings))
				for k := range render.Settings {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, key := range keys {
					edits, warns, err := planSettingsValue(st, old, file, []string{key}, render.Settings[key], opts.Force)
					if err != nil {
						return nil, err
					}
					plan.Warnings = append(plan.Warnings, warns...)
					plan.Edits = append(plan.Edits, edits...)
				}
			}
		}
	}
	inst.Warnings = UniqueStrings(plan.Warnings)
	inst.Skipped = UniqueStrings(plan.Skipped)
	plan.Install = inst
	return plan, nil
}

func sourceHarnessForOld(opts InstallOptions, target string) string {
	if opts.OldHarness != "" {
		return opts.OldHarness
	}
	return target
}

// UniqueStrings drops blank and duplicate entries, preserving order.
func UniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func (r *RenderPlan) MCPPath() string {
	return r.Target.MCP
}

func fileAllowedRoot(dest string, t Target) bool {
	if hasReservedSegment(dest) {
		return false
	}
	roots := append([]string{t.Root, t.ExtraDir}, t.ComponentDirs()...)
	if t.Project != "" {
		roots = append(roots, t.Project)
	}
	for _, r := range roots {
		if r != "" && isWithin(r, dest) {
			return true
		}
	}
	return false
}

// configPathAllowed bounds adapter-chosen config destinations. A community
// adapter must not be able to write outside the project through its
// MCP/hooks/settings hooks.
func configPathAllowed(dest string, t Target) bool {
	if dest == "" {
		return true
	}
	return t.Project != "" && isWithin(t.Project, dest)
}

func checkFileConflict(st *State, dest, id string, force bool) error {
	fi, err := os.Lstat(dest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("cannot install file over a directory: %s", dest)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return err
	}
	cur := ShortHash(data, 64)
	owner := st.ownerOf(dest)
	if owner != nil {
		for _, of := range owner.Files {
			if of.Path == dest && of.SHA == cur {
				return nil
			}
		}
		if !force {
			return fmt.Errorf("%s was modified since it was installed; use --force to overwrite", dest)
		}
		return nil
	}
	if force {
		return nil
	}
	return fmt.Errorf("%s already exists and is not managed by also; use --force to replace it (a backup will be kept)", dest)
}

func checkRemove(st *State, of InstalledFile, id, oldID string, force bool) (bool, string, error) {
	fi, err := os.Lstat(of.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, "", nil
		}
		return false, "", err
	}
	if fi.IsDir() {
		return false, fmt.Sprintf("not removing directory: %s", of.Path), nil
	}
	data, err := os.ReadFile(of.Path)
	if err != nil {
		return false, "", err
	}
	if ShortHash(data, 64) != of.SHA {
		if !force {
			return false, fmt.Sprintf("%s was modified; left in place", of.Path), nil
		}
	}
	owner := st.ownerOf(of.Path)
	if owner != nil && owner.ID != id && owner.ID != oldID {
		return false, fmt.Sprintf("%s is also owned by another install; left in place", of.Path), nil
	}
	return true, "", nil
}

// ---- config planning ----

func findOldEdit(old *Install, file string, path []string) *ConfigEdit {
	if old == nil {
		return nil
	}
	for i := range old.Edits {
		e := &old.Edits[i]
		if e.File == file && stringSliceEqual(e.Path, path) {
			return e
		}
	}
	return nil
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readConfigFile(path string) ([]byte, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte("{}"), 0o644, nil
		}
		return nil, 0, err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return data, mode, nil
}

func planJSONSet(st *State, old *Install, file string, path []string, value any, force bool) (*ConfigEdit, string, error) {
	if file == "" {
		return nil, "MCP config path is unknown for this harness; skipped", nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	data, _, err := readConfigFile(file)
	if err != nil {
		return nil, "", err
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", file, err)
	}
	existing, exists := doc.RawAt(path...)
	oldEdit := findOldEdit(old, file, path)
	if exists && oldEdit == nil && jsonEqual(existing, raw) {
		return nil, fmt.Sprintf("%s is already configured in %s; left unchanged", strings.Join(path, "."), file), nil
	}
	if oldEdit != nil && exists && !jsonEqual(existing, oldEdit.Value) && !force {
		return nil, "", fmt.Errorf("%s in %s was changed since it was installed; use --force to replace it", strings.Join(path, "."), file)
	}
	if exists && oldEdit == nil && !force {
		return nil, "", fmt.Errorf("%s is already configured differently in %s; use --force to replace it (a backup is kept)", strings.Join(path, "."), file)
	}
	e := &ConfigEdit{File: file, Kind: "json-set", Path: path, Value: raw, CreatedFile: !fileExists(file)}
	if oldEdit != nil {
		// Keep the original pre-install value across upgrades.
		e.HadPrev = oldEdit.HadPrev
		e.Previous = oldEdit.Previous
		e.CreatedFile = oldEdit.CreatedFile
	} else if exists {
		e.HadPrev = true
		e.Previous = existing
	}
	return e, "", nil
}

func openCodeMCPPath(doc *JSONC, name string) []string {
	if _, ok := doc.RawAt("mcp", "servers"); ok {
		return []string{"mcp", "servers", name}
	}
	if raw, ok := doc.RawAt("mcp"); ok {
		var m map[string]any
		if json.Unmarshal(raw, &m) == nil {
			for _, v := range m {
				obj, ok := v.(map[string]any)
				if !ok {
					continue
				}
				if _, ok := obj["type"]; ok {
					return []string{"mcp", name}
				}
				if _, ok := obj["command"]; ok {
					return []string{"mcp", name}
				}
				if _, ok := obj["url"]; ok {
					return []string{"mcp", name}
				}
			}
		}
	}
	return []string{"mcp", "servers", name}
}

func planOpenCodeMCP(st *State, old *Install, file, name string, value any, force bool) (*ConfigEdit, string, error) {
	data, _, err := readConfigFile(file)
	if err != nil {
		return nil, "", err
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", file, err)
	}
	path := openCodeMCPPath(doc, name)
	warn := ""
	if len(path) == 2 {
		warn = fmt.Sprintf("%s uses the legacy mcp.<name> shape; the server was written in the same shape", file)
	}
	edit, w, err := planJSONSet(st, old, file, path, value, force)
	if warn != "" && w == "" {
		w = warn
	}
	return edit, w, err
}

func planSettingsValue(st *State, old *Install, file string, path []string, value any, force bool) ([]ConfigEdit, []string, error) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var edits []ConfigEdit
		var warns []string
		for _, k := range keys {
			e, w, err := planSettingsValue(st, old, file, append(append([]string{}, path...), k), v[k], force)
			if err != nil {
				return nil, warns, err
			}
			edits = append(edits, e...)
			warns = append(warns, w...)
		}
		return edits, warns, nil
	case []any:
		var edits []ConfigEdit
		for _, el := range v {
			e, added, err := planJSONAppend(file, path, el, old, force)
			if err != nil {
				return nil, nil, err
			}
			if added {
				edits = append(edits, *e)
			}
		}
		return edits, nil, nil
	case nil:
		return nil, nil, nil
	default:
		e, warn, err := planJSONSet(st, old, file, path, v, force)
		if err != nil {
			return nil, nil, err
		}
		if e == nil {
			if warn != "" {
				return nil, []string{warn}, nil
			}
			return nil, nil, nil
		}
		return []ConfigEdit{*e}, nil, nil
	}
}

func planJSONAppend(file string, path []string, value any, old *Install, force bool) (*ConfigEdit, bool, error) {
	data, _, err := readConfigFile(file)
	if err != nil {
		return nil, false, err
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", file, err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	if t, ok := doc.TypeAt(path...); ok {
		if t != "array" {
			return nil, false, fmt.Errorf("%s is not an array in %s", strings.Join(path, "."), file)
		}
		arrRaw, _ := doc.RawAt(path...)
		var arr []json.RawMessage
		if err := json.Unmarshal(arrRaw, &arr); err == nil {
			for _, el := range arr {
				if !jsonEqual(el, raw) {
					continue
				}
				// An identical group owned by the previous install will be
				// removed before re-applying, so it does not count as a duplicate.
				owned := false
				if old != nil {
					for _, e := range old.Edits {
						if e.File == file && e.Kind == "json-append" && stringSliceEqual(e.Path, path) && jsonEqual(e.Value, raw) {
							owned = true
							break
						}
					}
				}
				if !owned {
					return nil, false, nil
				}
			}
		}
	}
	e := &ConfigEdit{File: file, Kind: "json-append", Path: path, Value: raw, CreatedFile: !fileExists(file)}
	if old != nil {
		for _, oe := range old.Edits {
			if oe.File == file && oe.Kind == "json-append" && stringSliceEqual(oe.Path, path) && jsonEqual(oe.Value, raw) {
				e.CreatedFile = oe.CreatedFile
				break
			}
		}
	}
	return e, true, nil
}

func readMaybe(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

func planTOMLMCP(st *State, old *Install, file, name, block string, force bool) (*ConfigEdit, string, error) {
	if file == "" {
		return nil, "codex config path is unknown; MCP skipped", nil
	}
	data, err := readMaybe(file)
	if err != nil {
		return nil, "", err
	}
	marker := "mcp:" + name
	ours := false
	if old != nil {
		for _, e := range old.Edits {
			if e.File == file && e.Marker == marker {
				ours = true
				break
			}
		}
	}
	if tomlHasMCPServer(data, name) && !ours {
		return nil, "", fmt.Errorf("MCP server %q already exists in %s and is not managed by also; remove it manually or choose another name", name, file)
	}
	if _, err := parseTOMLMap(data); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", file, err)
	}
	e := &ConfigEdit{File: file, Kind: "toml-block", Marker: marker, Block: block, CreatedFile: !fileExists(file)}
	if old != nil {
		for _, oe := range old.Edits {
			if oe.File == file && oe.Kind == "toml-block" && oe.Marker == marker {
				e.CreatedFile = oe.CreatedFile
				break
			}
		}
	}
	return e, "", nil
}

// ---- execution ----

type tx struct {
	snapshots map[string]*fileSnapshot
	backupDir string
}

type fileSnapshot struct {
	data    []byte
	mode    os.FileMode
	existed bool
}

func newTx() *tx {
	return &tx{snapshots: map[string]*fileSnapshot{}, backupDir: filepath.Join(BackupsDir(), time.Now().Format("20060102-150405"))}
}

func (t *tx) snapshot(path string) error {
	if _, ok := t.snapshots[path]; ok {
		return nil
	}
	s := &fileSnapshot{}
	data, err := os.ReadFile(path)
	if err == nil {
		s.data, s.existed = data, true
		if st, err := os.Stat(path); err == nil {
			s.mode = st.Mode().Perm()
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	t.snapshots[path] = s
	return nil
}

func (t *tx) write(path string, data []byte, mode os.FileMode) error {
	if err := t.snapshot(path); err != nil {
		return err
	}
	return atomicWrite(path, data, mode)
}

func (t *tx) remove(path string) error {
	if err := t.snapshot(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (t *tx) backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("backup %s: %w", path, err)
	}
	clean := filepath.Clean(path)
	// A Windows absolute path carries a volume ("C:"), which cannot be a
	// directory name inside the backup tree.
	rel := strings.TrimPrefix(strings.TrimPrefix(clean, filepath.VolumeName(clean)), string(filepath.Separator))
	rel = strings.ReplaceAll(rel, ":", "")
	dest := filepath.Join(t.backupDir, rel)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	return nil
}

func (t *tx) rollback() {
	for path, s := range t.snapshots {
		if s.existed {
			_ = atomicWrite(path, s.data, s.mode.Perm())
		} else {
			_ = os.Remove(path)
		}
	}
}

func ApplyPlan(st *State, plan *Plan, opts InstallOptions) error {
	t := newTx()
	if err := applyPlanTx(t, st, plan, opts); err != nil {
		t.rollback()
		return err
	}
	return nil
}

func applyPlanTx(t *tx, st *State, plan *Plan, opts InstallOptions) error {
	id := plan.Install.ID
	old := st.FindByID(id)

	// 1. Remove previous config edits.
	if old != nil {
		for _, e := range old.Edits {
			check, warn, err := removeConfigEdit(t, e)
			if err != nil {
				return err
			}
			if warn != "" {
				plan.Warnings = append(plan.Warnings, warn)
			}
			if !check {
				plan.Warnings = append(plan.Warnings, "left modified config entry in "+e.File)
			}
		}
	}
	// 2. Remove files that disappeared from the package.
	for _, of := range plan.RemoveFiles {
		if opts.Force {
			if err := t.backup(of.Path); err != nil {
				return err
			}
		}
		if err := t.remove(of.Path); err != nil {
			return err
		}
		pruneEmptyDirs(of.Path, pruneStops(&plan.Install)...)
	}
	// 3. Remove the old instructions block if it moved to another file.
	if plan.RemoveBlock != nil && (plan.Block == nil || plan.RemoveBlock.File != plan.Block.File) {
		if err := removeBlockTx(t, plan.RemoveBlock, opts.Force); err != nil {
			return err
		}
	}
	// 4. Write portable files.
	for _, f := range plan.NewFiles {
		if opts.Force {
			if err := t.backup(f.Dest); err != nil {
				return err
			}
		}
		if err := t.write(f.Dest, f.Data, f.Mode); err != nil {
			return err
		}
	}
	// 5. Instructions block.
	if plan.Block != nil {
		if err := writeBlockTx(t, plan.Block, plan.BlockBody); err != nil {
			return err
		}
	}
	// 6. Config edits.
	for _, e := range plan.Edits {
		if opts.Force {
			if err := t.backup(e.File); err != nil {
				return err
			}
		}
		if err := applyConfigEdit(t, e); err != nil {
			return err
		}
	}
	inst := plan.Install
	inst.Block = plan.Block
	inst.Edits = plan.Edits
	inst.Warnings = UniqueStrings(plan.Warnings)
	inst.Skipped = UniqueStrings(plan.Skipped)
	// 7. Save state last, atomically.
	if err := t.snapshot(StatePath()); err != nil {
		return err
	}
	if plan.OldInstallID != "" && plan.OldInstallID != id {
		st.Remove(plan.OldInstallID)
	}
	st.Remove(id)
	st.Installs = append(st.Installs, inst)
	sort.Slice(st.Installs, func(i, j int) bool {
		if st.Installs[i].Set != st.Installs[j].Set {
			return st.Installs[i].Set < st.Installs[j].Set
		}
		return st.Installs[i].Package < st.Installs[j].Package
	})
	return SaveState(st)
}

func writeBlockTx(t *tx, b *BlockEdit, body []byte) error {
	data, err := os.ReadFile(b.File)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(b.File); err == nil {
		mode = st.Mode().Perm()
	}
	content := string(data)
	if start, _, _, end, found := findBlock(content, b.ID); found {
		content = content[:start] + renderBlock(b.ID, body) + content[end:]
	} else {
		prefix := ""
		if strings.TrimSpace(content) != "" {
			prefix = "\n"
			if !strings.HasSuffix(content, "\n") {
				prefix = "\n\n"
			} else if !strings.HasSuffix(content, "\n\n") {
				prefix = "\n"
			}
		}
		content = content + prefix + renderBlock(b.ID, body)
	}
	return t.write(b.File, []byte(content), mode)
}

func removeBlockTx(t *tx, b *BlockEdit, force bool) error {
	data, err := os.ReadFile(b.File)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content := string(data)
	start, bs, be, end, found := findBlock(content, b.ID)
	if !found {
		return nil
	}
	if blockBodyHash([]byte(content[bs:be])) != b.SHA && !force {
		return fmt.Errorf("instructions block %q in %s was modified; use --force to remove it", b.ID, b.File)
	}
	out := content[:start] + content[end:]
	if strings.TrimSpace(out) == "" && b.CreatedFile {
		return t.remove(b.File)
	}
	out = strings.TrimRight(out, "\n") + "\n"
	mode := os.FileMode(0o644)
	if st, err := os.Stat(b.File); err == nil {
		mode = st.Mode().Perm()
	}
	return t.write(b.File, []byte(out), mode)
}

func applyConfigEdit(t *tx, e ConfigEdit) error {
	switch e.Kind {
	case "json-set":
		data, mode, err := readConfigFile(e.File)
		if err != nil {
			return err
		}
		doc, err := ParseJSONC(data)
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(e.Value, &v); err != nil {
			return err
		}
		out, err := doc.Set(v, e.Path...)
		if err != nil {
			return err
		}
		return t.write(e.File, out, mode)
	case "json-append":
		data, mode, err := readConfigFile(e.File)
		if err != nil {
			return err
		}
		doc, err := ParseJSONC(data)
		if err != nil {
			return err
		}
		var v any
		if err := json.Unmarshal(e.Value, &v); err != nil {
			return err
		}
		out, err := doc.Append(v, e.Path...)
		if err != nil {
			return err
		}
		return t.write(e.File, out, mode)
	case "toml-block":
		data, err := readMaybe(e.File)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if st, err := os.Stat(e.File); err == nil {
			mode = st.Mode().Perm()
		}
		if e.Block == "" {
			return fmt.Errorf("internal: empty TOML block for %s", e.File)
		}
		out := tomlAppendBlock(data, e.Marker, e.Block)
		if _, err := parseTOMLMap(out); err != nil {
			return fmt.Errorf("refusing to write invalid TOML to %s: %w", e.File, err)
		}
		return t.write(e.File, out, mode)
	}
	return fmt.Errorf("unknown config edit kind %q", e.Kind)
}

func isEmptyJSON(data []byte) bool {
	doc, err := ParseJSONC(data)
	if err != nil {
		return false
	}
	var empty func(n *jNode) bool
	empty = func(n *jNode) bool {
		switch n.kind {
		case jObject:
			for _, m := range n.members {
				if !empty(m.val) {
					return false
				}
			}
			return true
		case jArray:
			for _, el := range n.elems {
				if !empty(el) {
					return false
				}
			}
			return true
		}
		return false
	}
	return empty(doc.root)
}

func removeConfigEdit(t *tx, e ConfigEdit) (bool, string, error) {
	data, err := os.ReadFile(e.File)
	if err != nil {
		if os.IsNotExist(err) {
			return true, "", nil
		}
		return false, "", err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(e.File); err == nil {
		mode = st.Mode().Perm()
	}
	switch e.Kind {
	case "json-set":
		doc, err := ParseJSONC(data)
		if err != nil {
			return false, "", err
		}
		cur, ok := doc.RawAt(e.Path...)
		if !ok {
			return true, "", nil
		}
		if !jsonEqual(cur, e.Value) {
			return false, "", nil
		}
		var out []byte
		if e.HadPrev {
			var v any
			if err := json.Unmarshal(e.Previous, &v); err != nil {
				return false, "", err
			}
			out, err = doc.Set(v, e.Path...)
		} else {
			out, _, err = doc.Remove(e.Path...)
		}
		if err != nil {
			return false, "", err
		}
		if e.CreatedFile && isEmptyJSON(out) {
			return true, "", t.remove(e.File)
		}
		return true, "", t.write(e.File, out, mode)
	case "json-append":
		doc, err := ParseJSONC(data)
		if err != nil {
			return false, "", err
		}
		out, found, err := doc.RemoveEqual(e.Value, e.Path...)
		if err != nil {
			return false, "", err
		}
		if !found {
			return false, "", nil
		}
		if e.CreatedFile && isEmptyJSON(out) {
			return true, "", t.remove(e.File)
		}
		return true, "", t.write(e.File, out, mode)
	case "toml-block":
		if e.Block != "" {
			if body, ok := tomlBlockBody(data, e.Marker); ok && strings.TrimSpace(body) != strings.TrimSpace(e.Block) {
				return false, "", nil
			}
		}
		out, found := tomlRemoveBlock(data, e.Marker)
		if !found {
			return false, "", nil
		}
		if e.CreatedFile && strings.TrimSpace(string(out)) == "" {
			return true, "", t.remove(e.File)
		}
		return true, "", t.write(e.File, out, mode)
	}
	return false, "", nil
}
