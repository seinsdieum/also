package also

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AdoptOptions struct {
	Name         string
	Project      string
	Harness      string
	Dest         string
	Force        bool
	AllowSecrets bool
	AllowLarge   bool
}

type AdoptResult struct {
	BundleDir string
	Files     int
	Warnings  []string
}

func isConfigBasename(rest string, h *Harness) bool {
	if strings.Contains(rest, "/") {
		return false
	}
	return h.ConfigNames()[rest]
}

func preserveInLayer(rel string, h *Harness, in Install) bool {
	if strings.HasPrefix(rel, BundleLayerPrefix+"/") {
		return true
	}
	rest := trimRootPrefix(rel, h)
	if isConfigBasename(rest, h) {
		return true
	}
	for _, d := range h.ExtraDirs {
		if rest == d || strings.HasPrefix(rest, d+"/") {
			return true
		}
	}
	if h.Bundle.Hooks != "" && strings.HasPrefix(rest, h.Bundle.Hooks+"/") {
		return true
	}
	for _, s := range in.Skipped {
		if strings.HasSuffix(s, ":"+rel) {
			return true
		}
	}
	for _, c := range h.Components {
		if rest == c.BundleDir || strings.HasPrefix(rest, c.BundleDir+"/") {
			return true
		}
	}
	return false
}

func loadInstalledPackage(cfg *Config, in Install) (*Package, error) {
	if sc, ok := cfg.Sets[in.Set]; ok && sc.Remote == in.SetRemote {
		repo, err := OpenSet(in.Set, sc, false)
		if err != nil {
			return nil, err
		}
		return repo.LoadPackage(in.Package, in.Version)
	}
	repo, err := OpenRemote(in.SetRemote, false)
	if err != nil {
		return nil, err
	}
	return repo.LoadPackage(in.Package, in.Version)
}

// addPreservedLayers records native source-harness data that the target harness
// could not represent (skipped components, native config, harness extras) in
// .also-layers/<harness>/ inside the new bundle.
func addPreservedLayers(tmp, project string, targetH *Harness, write func(string, []byte, fs.FileMode) error) error {
	st, err := LoadState()
	if err != nil || len(st.Installs) == 0 {
		return nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return nil
	}
	target := targetH.TargetPath("project", project)
	layerMap := map[string]map[string]string{}
	for _, in := range st.Installs {
		if in.Scope != "project" || in.Project == "" {
			continue
		}
		if filepath.Clean(in.Project) != filepath.Clean(project) {
			continue
		}
		if in.SourceHarness == targetH.ID {
			continue
		}
		src, ok := ResolveHarness(in.SourceHarness)
		if !ok {
			continue
		}
		pkg, err := loadInstalledPackage(cfg, in)
		if err != nil {
			continue
		}
		// Map translated destinations back to origin paths so the native layer
		// can restore an unmodified file, while a user-edited translated file
		// keeps precedence.
		destToOrigin := map[string]string{}
		lossyOrigins := map[string]bool{}
		if rendered, err := renderPackageMain(pkg, src, target); err == nil {
			for _, rf := range rendered.Files {
				destToOrigin[rf.Dest] = rf.OriginRel
				if rf.Component == "agent" || rf.Component == "command" {
					lossyOrigins[rf.OriginRel] = true
				}
			}
		}
		for _, sf := range in.Files {
			if origin, ok := destToOrigin[sf.Path]; ok {
				if layerMap[in.SourceHarness] == nil {
					layerMap[in.SourceHarness] = map[string]string{}
				}
				layerMap[in.SourceHarness][origin] = sf.SHA
			}
		}
		for _, rel := range pkg.Paths() {
			if !lossyOrigins[rel] && !preserveInLayer(rel, src, in) {
				continue
			}
			destRel := layerPath(in.SourceHarness, rel)
			if strings.HasPrefix(rel, BundleLayerPrefix+"/") {
				destRel = rel
			}
			dest := filepath.Join(tmp, filepath.FromSlash(destRel))
			if existing, err := os.ReadFile(dest); err == nil {
				if string(existing) != string(pkg.Files[rel].Data) {
					// Another package from the same source harness owns this path.
					continue
				}
				continue
			}
			if err := write(destRel, pkg.Files[rel].Data, pkg.Files[rel].Mode); err != nil {
				return err
			}
		}
	}
	if len(layerMap) == 0 {
		return nil
	}
	manifestPath := filepath.Join(tmp, ".also-layers", "manifest.json")
	var manifest struct {
		Layers map[string]map[string]string `json:"layers"`
	}
	if data, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(data, &manifest)
	}
	if manifest.Layers == nil {
		manifest.Layers = map[string]map[string]string{}
	}
	for harness, entries := range layerMap {
		if manifest.Layers[harness] == nil {
			manifest.Layers[harness] = map[string]string{}
		}
		for origin, sha := range entries {
			if _, exists := manifest.Layers[harness][origin]; !exists {
				manifest.Layers[harness][origin] = sha
			}
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return write(".also-layers/manifest.json", data, 0o644)
}

// unwrapAlsoBlocks removes the managed markers but keeps their body: adopt
// captures the effective instructions of a live project, which may consist
// entirely of an installed package's managed block.
func unwrapAlsoBlocks(content string) (string, int) {
	var b strings.Builder
	count := 0
	for _, line := range strings.SplitAfter(content, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, blockBeginPrefix) {
			count++
			continue
		}
		if strings.HasPrefix(t, blockEndPrefix) {
			continue
		}
		b.WriteString(line)
	}
	return b.String(), count
}

// ExtractBundle captures the live project-scoped infrastructure of one harness
// into a portable bundle directory.
func ExtractBundle(opts AdoptOptions) (*AdoptResult, error) {
	project, err := AbsPath(opts.Project)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(project); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("project directory does not exist: %s", project)
	}
	h, ok := ResolveHarness(opts.Harness)
	if !ok {
		return nil, fmt.Errorf("unknown harness %q; available: %v", opts.Harness, HarnessIDs())
	}
	if !ValidName(opts.Name) {
		return nil, fmt.Errorf("invalid package name %q", opts.Name)
	}
	if opts.Dest == "" {
		return nil, fmt.Errorf("no destination for the local bundle; pass --into or set a source directory with: also set <name> --source <dir>")
	}
	dest, err := AbsPath(filepath.Join(opts.Dest, opts.Name))
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(dest); err == nil && !opts.Force {
		if !st.IsDir() {
			return nil, fmt.Errorf("%s exists and is not a directory", dest)
		}
		return nil, fmt.Errorf("bundle already exists: %s (use --force to replace; the old one is kept as a backup)", dest)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dest), ".also-adopt-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	res := &AdoptResult{BundleDir: dest}
	target := h.TargetPath("project", project)

	write := func(rel string, data []byte, mode os.FileMode) error {
		rel = filepath.FromSlash(rel)
		if err := SafeRel(rel); err != nil {
			return err
		}
		p := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, mode); err != nil {
			return err
		}
		res.Files++
		return nil
	}
	copyTree := func(src, bundleRel string) error {
		st, err := os.Stat(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !st.IsDir() {
			return nil
		}
		snap, warns, err := ReadSnapshot(src, SnapshotOptions{AllowSecrets: opts.AllowSecrets, AllowLarge: opts.AllowLarge})
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		res.Warnings = append(res.Warnings, warns...)
		for _, rel := range snap.Paths() {
			f := snap.Files[rel]
			if err := write(filepath.ToSlash(filepath.Join(bundleRel, filepath.FromSlash(rel))), f.Data, f.Mode); err != nil {
				return err
			}
		}
		return nil
	}

	// Instructions: strip also-managed blocks, which belong to other packages.
	if target.Instr != "" {
		if data, err := os.ReadFile(target.Instr); err == nil {
			out, removed := unwrapAlsoBlocks(string(data))
			if removed > 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("unwrapped %d also-managed instruction block(s) from %s", removed, target.Instr))
			}
			out = strings.TrimRight(out, "\n")
			if strings.TrimSpace(out) != "" {
				if err := write(h.InstructionName(), []byte(out+"\n"), 0o644); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, c := range []struct {
		src  string
		dest string
	}{
		{target.Skills, "skills"},
		{target.Commands, "commands"},
		{target.Agents, "agents"},
		{target.Plugins, "plugins"},
		{target.Hooks, "hooks"},
	} {
		if c.src == "" {
			continue
		}
		if err := copyTree(c.src, c.dest); err != nil {
			return nil, err
		}
	}
	for _, c := range h.Components {
		src := target.ComponentDir(c.Kind)
		if src == "" {
			continue
		}
		if err := copyTree(src, c.BundleDir); err != nil {
			return nil, err
		}
	}
	for _, d := range h.ExtraDirs {
		if err := copyTree(filepath.Join(project, h.ExtraBase, d), d); err != nil {
			return nil, err
		}
	}

	// Native config files are preserved under the adapter's bundle names.
	configs := []struct{ src, rel string }{
		{target.Settings, h.Bundle.Settings},
		{target.HooksJSON, h.Bundle.HooksJSON},
	}
	copyConfig := func(src, rel string) error {
		if src == "" || rel == "" {
			return nil
		}
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return write(rel, data, 0o644)
	}
	for _, c := range configs {
		if err := copyConfig(c.src, c.rel); err != nil {
			return nil, err
		}
	}
	if target.MCP != "" && target.MCP != target.Settings {
		rel := h.Bundle.MCP
		if rel == "" {
			rel = filepath.Base(target.MCP)
		}
		if err := copyConfig(target.MCP, rel); err != nil {
			return nil, err
		}
	}
	if err := addPreservedLayers(tmp, project, h, write); err != nil {
		return nil, err
	}
	if res.Files == 0 {
		return nil, fmt.Errorf("no %s infrastructure found in %s; check --harness or the project directory", h.Display, project)
	}
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		backup := fmt.Sprintf("%s.also-backup-%s", dest, time.Now().Format("20060102-150405"))
		if err := os.Rename(dest, backup); err != nil {
			return nil, err
		}
		res.Warnings = append(res.Warnings, "previous bundle moved to "+backup)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, err
	}
	return res, nil
}
