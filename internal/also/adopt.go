package also

import (
	"fmt"
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

func stripAlsoBlocks(content string) (string, int) {
	removed := 0
	for {
		bi := strings.Index(content, blockBeginPrefix)
		if bi < 0 {
			break
		}
		ei := strings.Index(content[bi:], blockEndPrefix)
		if ei < 0 {
			break
		}
		ei += bi
		start := bi
		if ls := strings.LastIndexByte(content[:bi], '\n'); ls >= 0 {
			start = ls + 1
		}
		end := ei
		if nl := strings.IndexByte(content[end:], '\n'); nl >= 0 {
			end += nl + 1
		}
		content = content[:start] + content[end:]
		removed++
	}
	return content, removed
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
			out, removed := stripAlsoBlocks(string(data))
			if removed > 0 {
				res.Warnings = append(res.Warnings, fmt.Sprintf("removed %d also-managed instruction block(s) from %s", removed, target.Instr))
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

	// Native config files are preserved so MCP/hooks can be translated later.
	configs := []struct{ src, rel string }{
		{target.Settings, "settings.json"},
		{target.HooksJSON, "hooks.json"},
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
	switch h.MCPKind {
	case "claude", "qwen", "opencode":
		if target.MCP != "" && target.MCP != target.Settings {
			if err := copyConfig(target.MCP, filepath.Base(target.MCP)); err != nil {
				return nil, err
			}
		}
	case "codex":
		if target.MCP != "" {
			if err := copyConfig(target.MCP, "config.toml"); err != nil {
				return nil, err
			}
		}
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
