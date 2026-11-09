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
	// Exclude removes kinds or named components from an otherwise complete
	// install, for example "instructions" or "skill:review".
	Exclude []string
	// OnlyParts replaces the existing selection instead of adding to it.
	OnlyParts bool
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
	// RetainedEdits are old config edits that Reconcile deliberately kept.
	// They are not applied again, but they stay owned by the new install so an
	// upgrade does not delete a value the warning reported as kept.
	RetainedEdits []ConfigEdit
	Warnings      []string
	Skipped       []string
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
	if old == nil {
		// Adopt a slot whose path was recorded before paths were canonicalized.
		for idx := range st.Installs {
			candidate := &st.Installs[idx]
			if candidate.Set == set.Name && candidate.Package == pkg.Meta.Name &&
				candidate.Harness == targetH.ID && candidate.Scope == scope &&
				PathKey(candidate.Project) == PathKey(project) {
				old = candidate
				oldID = candidate.ID
				break
			}
		}
	}
	parts, excludes := opts.Parts, opts.Exclude
	if old != nil && !opts.OnlyParts && (len(parts) > 0 || len(excludes) > 0) {
		switch {
		case len(old.Parts) == 0:
			// The previous install was complete (possibly with exclusions).
			// Adding includes cannot narrow it; only exclusions can.
			parts = nil
			excludes = UniqueStrings(append(append([]string{}, old.Exclude...), excludes...))
		default:
			parts = UniqueStrings(append(append([]string{}, old.Parts...), parts...))
			excludes = UniqueStrings(append(append([]string{}, old.Exclude...), excludes...))
		}
	}
	render, err := RenderPackageSelection(pkg, source, target, parts, excludes)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Target: target, Render: render}
	if old != nil && old.ID != id {
		plan.OldInstallID = old.ID
	}
	plan.Warnings = append(plan.Warnings, render.Warnings...)
	plan.Skipped = append(plan.Skipped, render.Skipped...)

	inst := Install{
		ID: id, Set: set.Name, SetRemote: set.Cfg.Remote, Package: pkg.Meta.Name,
		Version: version.Version, Digest: pkg.Meta.Digest, Label: version.Label,
		SourceHarness: source.ID, Harness: targetH.ID, Scope: scope, Project: project,
		Root: target.Root, WithConfig: opts.WithConfig,
		Parts: UniqueStrings(parts), Exclude: UniqueStrings(excludes),
		InstalledAt: time.Now().UTC(),
	}
	if sem, ok := HighestSemverString(version.Semver); ok {
		inst.Semver = sem.String()
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
		if err := checkFileConflict(st, f.Dest, opts.Force, f.Data); err != nil {
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
			sameBody := blockBodyHash(oldBody) == blockBodyHash(body)
			if oldHash == "" {
				if !sameBody && !opts.Force {
					return nil, fmt.Errorf("instructions block %q in %s was modified by you; use --force to replace it", bid, render.Instr.File)
				}
			} else if blockBodyHash(oldBody) != oldHash && !opts.Force {
				return nil, fmt.Errorf("instructions block %q in %s was modified by you; use --force to replace it", bid, render.Instr.File)
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
						edit, added, err := planJSONAppend(st, file, path, group, old, opts.Force)
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
		declared := render.SettingsDeclared
		declaredPermissions := declared["permission"] || declared["permissions"]
		translatedAttempted := source.ID != targetH.ID && targetH.Settings.Translate != nil
		retainOnly := translatedAttempted && declaredPermissions && len(render.Settings) == 0
		if len(render.Settings) > 0 || retainOnly {
			file := SettingsTargetFile(target)
			if file == "" {
				plan.Warnings = append(plan.Warnings, "settings keys were preserved in the package but this harness has no mergeable settings file")
			} else {
				if !configPathAllowed(file, target) {
					return nil, fmt.Errorf("adapter %s points settings outside the project root: %s", targetH.ID, file)
				}
				settings := render.Settings
				translated := render.SettingsTranslated
				reconciled := len(translated) > 0 && targetH.Settings.Reconcile != nil
				if reconciled {
					subset := map[string]any{}
					for key := range translated {
						if value, ok := settings[key]; ok {
							subset[key] = value
						}
					}
					filtered, warns, err := targetH.Settings.Reconcile(file, subset)
					if err != nil {
						return nil, fmt.Errorf("reconcile settings for %s: %w", targetH.ID, err)
					}
					plan.Warnings = append(plan.Warnings, warns...)
					merged := map[string]any{}
					for key, value := range settings {
						if !translated[key] {
							merged[key] = value
						}
					}
					for key, value := range filtered {
						merged[key] = value
					}
					settings = merged
				}
				for _, key := range sortedKeys(settings) {
					force := opts.Force
					if reconciled && translated[key] {
						// Reconcile already kept or strengthened every value,
						// so the remaining translated keys are safe to own.
						force = true
					}
					path := []string{key}
					var edits []ConfigEdit
					var warns []string
					var err error
					if reconciled && translated[key] && isPermissionName(key) {
						edits, warns, err = planTranslatedPermission(st, old, file, path, settings[key], force)
					} else {
						edits, warns, err = planSettingsValue(st, old, file, path, settings[key], force)
					}
					if err != nil {
						return nil, err
					}
					plan.Warnings = append(plan.Warnings, warns...)
					plan.Edits = append(plan.Edits, edits...)
				}
				oldSettingsEdits := 0
				if old != nil && translatedAttempted {
					keys := settingsRetentionKeys(translated, declared)
					if len(keys) > 0 {
						for _, e := range old.Edits {
							if e.File == file && len(e.Path) > 0 && keys[e.Path[0]] {
								oldSettingsEdits++
							}
						}
						plan.RetainedEdits = retainUnplannedSettings(plan.Edits, old, file, keys)
					}
				}
				if retainOnly {
					warning := fmt.Sprintf("declared permission rules produced no translatable output for %s", targetH.Display)
					switch {
					case len(plan.RetainedEdits) > 0:
						warning += "; previous permission edits were retained"
					case oldSettingsEdits > 0:
						warning += "; previous permission edits were removed"
					default:
						warning += "; no previous permission edits existed"
					}
					plan.Warnings = append(plan.Warnings, warning)
				}
			}
		}
	}
	inst.Executables = collectExecutables(render, opts.WithConfig)
	inst.Warnings = UniqueStrings(plan.Warnings)
	inst.Skipped = UniqueStrings(plan.Skipped)
	plan.Install = inst
	return plan, nil
}

// collectExecutables records the installed surface that can run code or start a
// process, so audit and SBOM do not have to re-download the package.
func collectExecutables(render *RenderPlan, withConfig bool) []Executable {
	byKey := map[string]Executable{}
	add := func(e Executable) {
		if e.Name == "" {
			return
		}
		byKey[e.Kind+"\x00"+e.Name] = e
	}
	for _, f := range render.Files {
		switch f.Component {
		case "plugin", "command", "hook":
			add(Executable{Kind: f.Component, Name: filepath.Base(f.Dest), Detail: f.Dest})
		}
	}
	if withConfig {
		for name, s := range render.MCP {
			detail := s.Transport
			if len(s.Command) > 0 {
				detail = strings.TrimSpace(detail + " " + strings.Join(s.Command, " "))
			}
			if s.URL != "" {
				detail = strings.TrimSpace(detail + " " + s.URL)
			}
			add(Executable{Kind: "mcp", Name: name, Detail: detail})
		}
		for event, groups := range render.Hooks {
			add(Executable{Kind: "hook", Name: event, Detail: fmt.Sprintf("%d group(s)", len(groups))})
		}
	}
	out := make([]Executable, 0, len(byKey))
	for _, e := range byKey {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
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

func checkFileConflict(st *State, dest string, force bool, want []byte) error {
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
	// A byte-identical file is safe to adopt even when this machine lost its
	// state (backup restore, copied tree, or a committed infra tree that audit
	// will flag). It cannot hide a different payload.
	if bytes.Equal(data, want) {
		return nil
	}
	cur := ShortHash(data, 64)
	owner := st.ownerOf(dest)
	if owner != nil {
		for _, of := range owner.Files {
			if PathKey(of.Path) == PathKey(dest) && of.SHA == cur {
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
	for _, owner := range st.ownersOf(of.Path) {
		if owner.ID != id && owner.ID != oldID {
			return false, fmt.Sprintf("%s is also owned by another install; left in place", of.Path), nil
		}
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

// mergeJSONValue folds a package's desired value into the value already at the
// path. Wanted object keys are added, nested object keys are merged, and
// unrelated keys survive. It reports whether a wanted leaf held a different
// value, which the caller may treat as a conflict unless --force was given.
func mergeJSONValue(existing, desired json.RawMessage) (json.RawMessage, bool) {
	got, gotErr := decodeJSONValue(existing)
	want, wantErr := decodeJSONValue(desired)
	if gotErr != nil || wantErr != nil {
		return desired, !jsonEqual(existing, desired)
	}
	gotObj, gotIsObj := got.(map[string]any)
	wantObj, wantIsObj := want.(map[string]any)
	if !gotIsObj || !wantIsObj {
		return desired, !jsonValueEqual(got, want)
	}
	conflict := mergeJSONObjects(gotObj, wantObj)
	out, err := json.Marshal(gotObj)
	if err != nil {
		return desired, conflict
	}
	return out, conflict
}

// mergeJSONObjects merges want into got in place and reports whether a
// conflicting leaf was replaced.
func mergeJSONObjects(got, want map[string]any) bool {
	conflict := false
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			got[k] = wv
			continue
		}
		gotObj, gotIsObj := gv.(map[string]any)
		wantObj, wantIsObj := wv.(map[string]any)
		if gotIsObj && wantIsObj {
			if mergeJSONObjects(gotObj, wantObj) {
				conflict = true
			}
			continue
		}
		if !jsonValueEqual(gv, wv) {
			conflict = true
			got[k] = wv
		}
	}
	return conflict
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
	base, _, hasOwner := st.configBase(old, file, path)
	if exists && oldEdit == nil && jsonEqual(existing, raw) {
		// Two packages may want the same value. The second one still has to
		// record ownership, otherwise uninstalling the first removes a value
		// the second depends on.
		e := &ConfigEdit{File: file, Kind: "json-set", Path: path, Value: raw,
			HadPrev: true, Previous: existing,
			CreatedFile: !fileExists(file) || st.fileCreatedByAlso(file)}
		if hasOwner {
			e.Base, e.BaseSet = base, true
		} else {
			e.Base, e.BaseSet = existing, true
		}
		return e, fmt.Sprintf("%s is already configured in %s; shared ownership recorded", strings.Join(path, "."), file), nil
	}
	if oldEdit != nil && exists && !jsonEqual(existing, oldEdit.Value) && !force {
		return nil, "", fmt.Errorf("%s in %s was changed since it was installed; use --force to replace it", strings.Join(path, "."), file)
	}
	knownOrigin := false
	if st != nil && st.knownOrigins != nil {
		if origin, ok := st.knownOrigins[originKey(file, path)]; ok {
			knownOrigin = jsonEqual(existing, origin.Managed) ||
				(origin.BaseSet && !origin.Absent && jsonEqual(existing, origin.Value))
		}
	}
	switch {
	case oldEdit != nil:
		// Merge into the pre-install value, not the current one: keys that
		// the previous package version added but this one no longer declares
		// must disappear, while keys the user had before the first install
		// stay.
		var pre json.RawMessage
		switch {
		case oldEdit.BaseSet:
			pre = oldEdit.Base
		case oldEdit.HadPrev:
			pre = oldEdit.Previous
		}
		if len(pre) > 0 && !jsonEqual(pre, raw) {
			raw, _ = mergeJSONValue(pre, raw)
		}
	case exists && !jsonEqual(existing, raw):
		merged, conflict := mergeJSONValue(existing, raw)
		if conflict && !force && !knownOrigin {
			return nil, "", fmt.Errorf("%s is already configured differently in %s; use --force to replace it (a backup is kept)", strings.Join(path, "."), file)
		}
		raw = merged
	}
	e := &ConfigEdit{File: file, Kind: "json-set", Path: path, Value: raw, CreatedFile: !fileExists(file)}
	if oldEdit != nil {
		// Keep the original pre-install value across upgrades.
		e.HadPrev = oldEdit.HadPrev
		e.Previous = oldEdit.Previous
		e.CreatedFile = oldEdit.CreatedFile
		switch {
		case oldEdit.BaseSet:
			e.Base, e.BaseSet = oldEdit.Base, true
		case oldEdit.HadPrev:
			e.Base, e.BaseSet = oldEdit.Previous, true
		default:
			// The key was created by also; it had no user value. Do not
			// promote the previous package value to the base.
			e.Base, e.BaseSet = nil, true
		}
	} else if hasOwner {
		e.Base, e.BaseSet = base, true
	} else if exists {
		e.HadPrev = true
		e.Previous = existing
		e.Base, e.BaseSet = existing, true
	} else {
		e.Base, e.BaseSet = nil, true
	}
	if !e.CreatedFile && st.fileCreatedByAlso(file) {
		e.CreatedFile = true
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

func isPermissionName(key string) bool {
	return key == "permission" || key == "permissions"
}

// planTranslatedPermission keeps a translated permission object under one
// owner. If the previous install wrote the whole object, it plans one whole
// object again instead of splitting it into per-leaf edits.
func planTranslatedPermission(st *State, old *Install, file string, path []string, value any, force bool) ([]ConfigEdit, []string, error) {
	if current, ok := value.(map[string]any); ok {
		if edits, warns, handled, err := planWholeMapUpgrade(st, old, file, path, current, force); handled || err != nil {
			return edits, warns, err
		}
	}
	return planSettingsValue(st, old, file, path, value, force)
}

// planWholeMapUpgrade merges a new permission map over the previous package map
// while preserving user keys. It returns handled=false when the old install did
// not own the whole value.
func planWholeMapUpgrade(st *State, old *Install, file string, path []string, value map[string]any, force bool) ([]ConfigEdit, []string, bool, error) {
	if old == nil {
		return nil, nil, false, nil
	}
	oldEdit := findOldEdit(old, file, path)
	if oldEdit == nil || oldEdit.Kind != "json-set" {
		return nil, nil, false, nil
	}
	var oldMap map[string]any
	if err := json.Unmarshal(oldEdit.Value, &oldMap); err != nil || oldMap == nil {
		return nil, nil, false, nil
	}
	current, ok := readSettingsMap(file, path)
	if !ok {
		return nil, nil, false, nil
	}
	effective := map[string]any{}
	for key, value := range current {
		effective[key] = value
	}
	var warns []string
	for _, key := range sortedKeys(value) {
		desired := value[key]
		got, present := current[key]
		previous, owned := oldMap[key]
		if !present {
			effective[key] = desired
			continue
		}
		if !owned {
			// A user key that the new package now also declares.
			if !jsonValueEqual(got, desired) && !force {
				warns = append(warns, fmt.Sprintf("permission key %q in %s is user-owned; the existing value was kept", key, file))
				continue
			}
			effective[key] = desired
			continue
		}
		if !jsonValueEqual(got, previous) && !force {
			warns = append(warns, fmt.Sprintf("permission key %q in %s was changed by you; the existing value was kept", key, file))
			continue
		}
		effective[key] = desired
	}
	_, userStar := jsonStringValue(oldEdit.Base)
	// Package keys the new translation no longer declares are stale, but a
	// user change or a restriction is not stale.
	for _, key := range sortedKeys(oldMap) {
		if _, ok := value[key]; ok {
			continue
		}
		if key == "*" && userStar {
			// The target started as a global string; its wildcard belongs to
			// the user unless the new package declares "*" itself.
			continue
		}
		got, present := current[key]
		if !present {
			delete(effective, key)
			continue
		}
		previous := oldMap[key]
		if !jsonValueEqual(got, previous) {
			warns = append(warns, fmt.Sprintf("permission key %q in %s was changed by you; the existing value was kept", key, file))
			continue
		}
		if stalePermissionAllowed(got) {
			delete(effective, key)
			continue
		}
		if !stalePermissionRestricts(got) {
			if _, scoped := got.(map[string]any); scoped {
				warns = append(warns, fmt.Sprintf("permission key %q in %s keeps a scoped permission map; the existing policy was kept", key, file))
			} else {
				warns = append(warns, fmt.Sprintf("permission key %q in %s has an unsupported shape; the existing value was kept", key, file))
			}
		}
	}
	e, warn, err := planJSONSet(st, old, file, path, effective, true)
	if err != nil {
		return nil, nil, true, err
	}
	if warn != "" {
		warns = append(warns, warn)
	}
	if e == nil {
		return nil, warns, true, nil
	}
	return []ConfigEdit{*e}, warns, true, nil
}

// stalePermissionAllowed reports whether a stale package value is an allow
// grant that should be removed.
func stalePermissionAllowed(value any) bool {
	effect, ok := value.(string)
	if !ok {
		return false
	}
	normalized, ok := normalizeEffect(effect)
	return ok && normalized == "allow"
}

// stalePermissionRestricts reports whether a stale package value is a known
// deny/ask restriction that must not be deleted.
func stalePermissionRestricts(value any) bool {
	effect, ok := value.(string)
	if !ok {
		return false
	}
	normalized, ok := normalizeEffect(effect)
	return ok && (normalized == "deny" || normalized == "ask")
}

// readSettingsMap returns the current object at path. An absent path is an
// empty object; a different shape is reported as not readable so the caller
// falls back to the normal planner.
func readSettingsMap(file string, path []string) (map[string]any, bool) {
	data, _, err := readConfigFile(file)
	if err != nil {
		return nil, false
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return nil, false
	}
	raw, ok := doc.RawAt(path...)
	if !ok {
		return map[string]any{}, true
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return nil, false
	}
	return out, true
}

// configPathIsObject reports whether merging map keys into path is possible.
// An absent path is treated as an object because the planner creates it.
func configPathIsObject(file string, path []string) bool {
	data, _, err := readConfigFile(file)
	if err != nil {
		return true
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return true
	}
	kind, ok := doc.TypeAt(path...)
	return !ok || kind == "object"
}

func planSettingsValue(st *State, old *Install, file string, path []string, value any, force bool) ([]ConfigEdit, []string, error) {
	switch v := value.(type) {
	case map[string]any:
		if !configPathIsObject(file, path) {
			// A scalar or array currently occupies the path; nested keys
			// cannot be merged into it, so replace the whole value.
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
			e, added, err := planJSONAppend(st, file, path, el, old, force)
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

func planJSONAppend(st *State, file string, path []string, value any, old *Install, force bool) (*ConfigEdit, bool, error) {
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
				shared := false
				if st != nil {
					for idx := range st.Installs {
						other := &st.Installs[idx]
						if old != nil && other.ID == old.ID {
							continue
						}
						if old != nil && !sameConfigScope(old, other) {
							continue
						}
						for _, oe := range other.Edits {
							if oe.File == file && oe.Kind == "json-append" && stringSliceEqual(oe.Path, path) && jsonEqual(oe.Value, raw) {
								shared = true
								break
							}
						}
						if shared {
							break
						}
					}
				}
				if !owned && !shared {
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
	if body, ok := tomlBlockBody(data, marker); ok {
		// The managed marker is present; this is our block even after state
		// loss. A modified body still needs an explicit --force.
		if strings.TrimSpace(body) != strings.TrimSpace(block) && !force {
			return nil, "", fmt.Errorf("MCP block %q in %s was modified; use --force to replace it", marker, file)
		}
		ours = true
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
	oldID := id
	if plan.OldInstallID != "" {
		// A reshape moves the same package to a new harness ID; the edits and
		// files still belong to the old slot.
		oldID = plan.OldInstallID
	}
	old := st.FindByID(oldID)
	retained := make(map[string]bool, len(plan.RetainedEdits))
	for _, e := range plan.RetainedEdits {
		retained[configEditKey(e)] = true
	}

	// 1. Remove previous config edits, except those Reconcile kept.
	if old != nil {
		for _, e := range old.Edits {
			if retained[configEditKey(e)] {
				continue
			}
			check, warn, err := removeConfigEdit(t, st, old, e)
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
	inst.Edits = append(append([]ConfigEdit{}, plan.Edits...), plan.RetainedEdits...)
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
	if err := SaveState(st); err != nil {
		return err
	}
	if old != nil && old.Harness == plan.Install.Harness {
		// Keep origins across a harness reshape: the other harness may be
		// switched back later. Uninstall cleans them when the slot is gone.
		_ = CleanupConfigOrigins(plan.Install.Project, st)
	}
	return nil
}

// settingsRetentionKeys is the union of translated and declared settings keys.
// A permission declaration covers both target spellings because the source and
// target harnesses name the key differently.
func settingsRetentionKeys(translated, declared map[string]bool) map[string]bool {
	keys := map[string]bool{}
	for key := range translated {
		keys[key] = true
	}
	permissions := false
	for key := range declared {
		keys[key] = true
		if key == "permission" || key == "permissions" {
			permissions = true
		}
	}
	if permissions {
		keys["permission"] = true
		keys["permissions"] = true
	}
	return keys
}

// keepOldSettingEdit drops stale allow grants from retained state. An upgrade
// that removes or narrows an allow must not leave the target more permissive;
// deny/ask restrictions and unsupported shapes are kept.
func keepOldSettingEdit(e ConfigEdit) bool {
	if len(e.Path) == 0 {
		return true
	}
	switch e.Path[0] {
	case "permissions":
		if len(e.Path) > 1 && e.Path[1] == "allow" {
			return false
		}
	case "permission":
		var effect string
		if err := json.Unmarshal(e.Value, &effect); err == nil {
			if normalized, ok := normalizeEffect(effect); ok && normalized == "allow" {
				return false
			}
		}
	}
	return true
}

// retainUnplannedSettings keeps an old settings edit the new plan does not
// cover, so an upgrade cannot delete a restriction that is still in force.
// Only translated top-level keys are considered; a key the new package no
// longer declares is a normal package removal.
func retainUnplannedSettings(planned []ConfigEdit, old *Install, file string, translated map[string]bool) []ConfigEdit {
	var retained []ConfigEdit
	seen := map[string]bool{}
	for _, e := range old.Edits {
		if e.File != file || len(e.Path) == 0 || !translated[e.Path[0]] {
			continue
		}
		if settingsEditCovered(planned, e) {
			continue
		}
		if !keepOldSettingEdit(e) {
			continue
		}
		key := configEditKey(e)
		if seen[key] {
			continue
		}
		seen[key] = true
		retained = append(retained, e)
	}
	return retained
}

// settingsEditCovered reports whether the new plan owns the old edit. Array
// elements are identified by path and value; other edits by path and kind.
func settingsEditCovered(planned []ConfigEdit, old ConfigEdit) bool {
	for _, n := range planned {
		if n.File != old.File {
			continue
		}
		if n.Kind == "json-set" && pathStartsWith(old.Path, n.Path) {
			// A whole-value set owns every leaf below it.
			return true
		}
		if !stringSliceEqual(n.Path, old.Path) {
			continue
		}
		switch old.Kind {
		case "json-append":
			if n.Kind == "json-append" && jsonEqual(n.Value, old.Value) {
				return true
			}
		case "toml-block":
			if n.Kind == "toml-block" && n.Marker == old.Marker {
				return true
			}
		default:
			if n.Kind == old.Kind {
				return true
			}
		}
	}
	return false
}

func pathStartsWith(path, prefix []string) bool {
	if len(path) < len(prefix) {
		return false
	}
	for i := range prefix {
		if path[i] != prefix[i] {
			return false
		}
	}
	return true
}

// configEditKey identifies one edit across plan and state. The value and TOML
// block are part of the identity because two appends can share one path.
func configEditKey(e ConfigEdit) string {
	return e.File + "\x00" + e.Kind + "\x00" + strings.Join(e.Path, "\x00") + "\x00" +
		e.Marker + "\x00" + string(e.Value) + "\x00" + e.Block
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
		v, err := decodeJSONValue(e.Value)
		if err != nil {
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
		if raw, ok := doc.RawAt(e.Path...); ok {
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) == nil {
				for _, el := range arr {
					if jsonEqual(el, e.Value) {
						// Another package already contributed this exact
						// element; this edit only records shared ownership.
						return nil
					}
				}
			}
		}
		v, err := decodeJSONValue(e.Value)
		if err != nil {
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
		base, _ := tomlRemoveBlock(data, e.Marker)
		out := tomlAppendBlock(base, e.Marker, e.Block)
		if _, err := parseTOMLMap(out); err != nil {
			return fmt.Errorf("refusing to write invalid TOML to %s: %w", e.File, err)
		}
		return t.write(e.File, out, mode)
	}
	return fmt.Errorf("unknown config edit kind %q", e.Kind)
}

// pruneEmptyJSONParents removes container objects/arrays that became empty
// after also removed their last managed leaf. It stops at the first non-empty
// parent, so a user-owned sibling always keeps the container alive.
func pruneEmptyJSONParents(data []byte, path []string) []byte {
	for i := len(path) - 1; i >= 1; i-- {
		parent := path[:i]
		doc, err := ParseJSONC(data)
		if err != nil {
			return data
		}
		raw, ok := doc.RawAt(parent...)
		if !ok || !isEmptyJSON(raw) {
			return data
		}
		next, _, err := doc.Remove(parent...)
		if err != nil {
			return data
		}
		data = next
	}
	return data
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

func removeConfigEdit(t *tx, st *State, in *Install, e ConfigEdit) (bool, string, error) {
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
	removeIfEmpty := func(out []byte) error {
		if !isEmptyJSON(out) || st.fileHasOtherConfigEdits(in, e.File) || !st.fileCreatedByAlso(e.File) {
			return t.write(e.File, out, mode)
		}
		return t.remove(e.File)
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
		// Another installed package still owns this key: keep its value
		// instead of restoring a value that belonged to this install.
		if other := st.otherConfigEdit(in, e.File, e.Path); other != nil {
			if jsonEqual(cur, other.Value) {
				return true, "", nil
			}
			v, err := decodeJSONValue(other.Value)
			if err != nil {
				return false, "", err
			}
			out, err := doc.Set(v, e.Path...)
			if err != nil {
				return false, "", err
			}
			return true, "", t.write(e.File, out, mode)
		}
		// The edit did not add anything to the user's value: leave the bytes
		// untouched instead of rewriting an equivalent base.
		if e.BaseSet && len(e.Base) > 0 && jsonEqual(cur, e.Base) {
			return true, "", nil
		}
		var out []byte
		switch {
		case e.BaseSet && len(e.Base) == 0:
			out, _, err = doc.Remove(e.Path...)
			if err == nil {
				out = pruneEmptyJSONParents(out, e.Path)
			}
		case e.BaseSet:
			v, err := decodeJSONValue(e.Base)
			if err != nil {
				return false, "", err
			}
			out, err = doc.Set(v, e.Path...)
		case e.HadPrev:
			v, err := decodeJSONValue(e.Previous)
			if err != nil {
				return false, "", err
			}
			out, err = doc.Set(v, e.Path...)
		default:
			out, _, err = doc.Remove(e.Path...)
			if err == nil {
				out = pruneEmptyJSONParents(out, e.Path)
			}
		}
		if err != nil {
			return false, "", err
		}
		return true, "", removeIfEmpty(out)
	case "json-append":
		doc, err := ParseJSONC(data)
		if err != nil {
			return false, "", err
		}
		for idx := range st.Installs {
			other := &st.Installs[idx]
			if other.ID == in.ID || !sameConfigScope(in, other) {
				continue
			}
			for _, oe := range other.Edits {
				if oe.File == e.File && oe.Kind == "json-append" && stringSliceEqual(oe.Path, e.Path) && jsonEqual(oe.Value, e.Value) {
					return true, "", nil
				}
			}
		}
		out, found, err := doc.RemoveEqual(e.Value, e.Path...)
		if err != nil {
			return false, "", err
		}
		if !found {
			return false, "", nil
		}
		return true, "", removeIfEmpty(out)
	case "toml-block":
		if e.Block != "" {
			if body, ok := tomlBlockBody(data, e.Marker); ok && strings.TrimSpace(body) != strings.TrimSpace(e.Block) {
				return false, "", nil
			}
		}
		for idx := range st.Installs {
			other := &st.Installs[idx]
			if other.ID == in.ID || !sameConfigScope(in, other) {
				continue
			}
			for _, oe := range other.Edits {
				if oe.File == e.File && oe.Kind == "toml-block" && oe.Marker == e.Marker {
					return true, "", nil
				}
			}
		}
		out, found := tomlRemoveBlock(data, e.Marker)
		if !found {
			return false, "", nil
		}
		if strings.TrimSpace(string(out)) == "" && !st.fileHasOtherConfigEdits(in, e.File) && st.fileCreatedByAlso(e.File) {
			return true, "", t.remove(e.File)
		}
		return true, "", t.write(e.File, out, mode)
	}
	return false, "", nil
}
