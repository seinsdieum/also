package also

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SetRepo struct {
	Name string
	Cfg  SetConfig
	Dir  string
}

func OpenSet(name string, cfg SetConfig, refresh bool) (*SetRepo, error) {
	dir := cfg.Path
	if dir == "" {
		dir = SettingSetPath(name)
		cfg.Path = dir
	}
	if !filepath.IsAbs(dir) {
		abs, err := AbsPath(dir)
		if err != nil {
			return nil, err
		}
		dir = abs
		cfg.Path = dir
	}
	if !isGitRepo(dir) {
		if _, err := os.Stat(dir); err == nil {
			return nil, fmt.Errorf("%s exists but is not a git repository", dir)
		}
		if _, err := cloneOrInit(cfg.Remote, dir); err != nil {
			return nil, fmt.Errorf("open set %q: %w", name, err)
		}
	}
	if err := ensureLineEndings(dir); err != nil {
		return nil, err
	}
	ensureLocalExcludes(dir)
	repo := &SetRepo{Name: name, Cfg: cfg, Dir: dir}
	remote, err := runGit(dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("set %q has no origin remote: %w", name, err)
	}
	if strings.TrimSpace(remote) != strings.TrimSpace(cfg.Remote) {
		return nil, fmt.Errorf("set %q points at %s but its remote is %s; refusing to use it", name, cfg.Remote, strings.TrimSpace(remote))
	}
	if refresh {
		if err := repo.Refresh(); err != nil {
			return nil, err
		}
	}
	return repo, nil
}

func OpenRemote(remote string, refresh bool) (*SetRepo, error) {
	dir := SetCachePath(remote)
	cfg := SetConfig{Remote: remote, Path: dir}
	return OpenSet("remote-"+ShortHash([]byte(remote), 6), cfg, refresh)
}

func (s *SetRepo) Refresh() error {
	if !hasCommits(s.Dir) {
		return nil
	}
	dirty, status, err := gitDirty(s.Dir)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("the local also clone is dirty:\n%s\nresolve it in %s", status, s.Dir)
	}
	return pullFF(s.Dir)
}

func (s *SetRepo) PackageDir(pkg, version string) string {
	return filepath.Join(s.Dir, "packages", pkg, version)
}

func ReadPackageMeta(dir string) (PackageMeta, error) {
	var meta PackageMeta
	data, err := os.ReadFile(filepath.Join(dir, PackageManifestFile))
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return meta, err
	}
	if meta.Schema != SchemaPkg {
		return meta, fmt.Errorf("unsupported package schema %q", meta.Schema)
	}
	if !ValidName(meta.Name) {
		return meta, fmt.Errorf("invalid package name %q", meta.Name)
	}
	return meta, nil
}

type VersionInfo struct {
	Version string
	Label   string
	Time    time.Time
	Order   int `json:"-"`
	Harness string
}

type versionIndex struct {
	committed map[string]bool
	times     map[string]time.Time
	order     map[string]int
}

func (s *SetRepo) index() (*versionIndex, error) {
	committed, err := committedVersions(s.Dir)
	if err != nil {
		return nil, err
	}
	times, order, err := versionTimes(s.Dir)
	if err != nil {
		return nil, err
	}
	return &versionIndex{committed: committed, times: times, order: order}, nil
}

func (s *SetRepo) Versions(pkg string) ([]VersionInfo, error) {
	if !ValidName(pkg) {
		return nil, fmt.Errorf("invalid package name %q", pkg)
	}
	idx, err := s.index()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(s.Dir, "packages", pkg)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("package %q not found in set %q", pkg, s.Name)
		}
		return nil, err
	}
	var out []VersionInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := e.Name()
		key := pkg + "/" + ver
		if !idx.committed[key] {
			continue
		}
		meta, err := ReadPackageMeta(filepath.Join(dir, ver))
		if err != nil {
			continue
		}
		out = append(out, VersionInfo{
			Version: ver, Label: meta.Label, Harness: meta.SourceHarness,
			Time: idx.times[key], Order: idx.order[key],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Version > out[j].Version
	})
	return out, nil
}

func (s *SetRepo) Packages() ([]string, error) {
	dir := filepath.Join(s.Dir, "packages")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && ValidName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *SetRepo) Latest(pkg string) (VersionInfo, error) {
	versions, err := s.Versions(pkg)
	if err != nil {
		return VersionInfo{}, err
	}
	if len(versions) == 0 {
		return VersionInfo{}, fmt.Errorf("package %q has no committed versions in set %q", pkg, s.Name)
	}
	return versions[0], nil
}

func (s *SetRepo) ResolveVersion(pkg, spec string) (VersionInfo, error) {
	versions, err := s.Versions(pkg)
	if err != nil {
		return VersionInfo{}, err
	}
	if len(versions) == 0 {
		return VersionInfo{}, fmt.Errorf("package %q has no committed versions", pkg)
	}
	latest := versions[0]
	if spec == "" || spec == "latest" {
		return latest, nil
	}
	if strings.EqualFold(spec, "latest") {
		return latest, nil
	}
	// Label match takes precedence only when no version prefix matches.
	var prefix []VersionInfo
	for _, v := range versions {
		if v.Version == spec || (len(spec) >= 4 && strings.HasPrefix(v.Version, spec)) {
			prefix = append(prefix, v)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	if len(prefix) > 1 {
		return VersionInfo{}, fmt.Errorf("version prefix %q is ambiguous in %q; candidates: %s", spec, pkg, joinVersions(prefix))
	}
	for _, v := range versions {
		if v.Label != "" && v.Label == spec {
			return v, nil
		}
	}
	return VersionInfo{}, fmt.Errorf("version %q not found for %q; available: %s", spec, pkg, joinVersions(versions))
}

func joinVersions(vs []VersionInfo) string {
	parts := make([]string, 0, len(vs))
	for _, v := range vs {
		p := v.Version
		if v.Label != "" {
			p += " (" + v.Label + ")"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

func (s *SetRepo) LoadPackage(pkg, version string) (*Package, error) {
	if !ValidName(pkg) {
		return nil, fmt.Errorf("invalid package name %q", pkg)
	}
	if version == "" || strings.ContainsAny(version, `/\`) || strings.Contains(version, "..") {
		return nil, fmt.Errorf("invalid version %q", version)
	}
	dir := s.PackageDir(pkg, version)
	p, err := LoadPackage(dir)
	if err != nil {
		return nil, fmt.Errorf("load %s@%s: %w", pkg, version, err)
	}
	return p, nil
}

type PushOptions struct {
	Name         string
	Description  string
	Harness      string
	Label        string
	Message      string
	AllowSecrets bool
	AllowLarge   bool
	DryRun       bool
}

type PushResult struct {
	Package    string
	Version    string
	Digest     string
	Label      string
	Harness    string
	Files      int
	Bytes      int64
	Existing   bool
	Warnings   []string
	SetCreated bool
	Committed  bool
	Remote     string
}

func (s *SetRepo) Push(bundleDir string, opts PushOptions) (*PushResult, error) {
	if !ValidName(opts.Name) {
		return nil, fmt.Errorf("invalid package name %q; use lowercase letters, digits, '.', '_' or '-' (max 64)", opts.Name)
	}
	if opts.Label != "" && !ValidName(strings.ToLower(opts.Label)) {
		return nil, fmt.Errorf("invalid label %q; use lowercase letters, digits, '.', '_' or '-'", opts.Label)
	}
	snap, warnings, err := ReadSnapshot(bundleDir, SnapshotOptions{AllowSecrets: opts.AllowSecrets, AllowLarge: opts.AllowLarge})
	if err != nil {
		return nil, err
	}
	harness := opts.Harness
	if harness == "" {
		harness, err = DetectHarness(bundleDir)
		if err != nil {
			return nil, err
		}
	} else {
		h, ok := ResolveHarness(harness)
		if !ok {
			return nil, fmt.Errorf("unknown harness %q; available: %v", harness, HarnessIDs())
		}
		harness = h.ID
	}
	h, _ := ResolveHarness(harness)
	servers, mcpWarnings, err := ExtractBundleMCP(h, snap.Files, h.BundleRoots(bundleDir))
	if err != nil {
		return nil, fmt.Errorf("cannot encode MCP configuration: %w", err)
	}
	warnings = append(warnings, mcpWarnings...)
	if secrets := MCPSuspiciousSecrets(servers); len(secrets) > 0 && !opts.AllowSecrets {
		return nil, fmt.Errorf("refusing to encode literal MCP credentials (use --allow-secrets if intentional):\n  %s", strings.Join(secrets, "\n  "))
	}
	meta := PackageMeta{
		Schema: SchemaPkg, Name: opts.Name, Description: opts.Description,
		SourceHarness: harness, Label: opts.Label, EncodedWith: "also " + Version,
	}
	digest := snap.Digest(meta)
	meta.Digest = digest
	version := VersionFromDigest(digest)
	res := &PushResult{
		Package: opts.Name, Version: version, Digest: digest, Label: opts.Label,
		Harness: harness, Files: len(snap.Files), Bytes: snap.TotalBytes(),
		Warnings: warnings, Remote: s.Cfg.Remote,
	}
	if opts.DryRun {
		return res, nil
	}
	if isVersionCommitted(s.Dir, opts.Name, version) {
		res.Existing = true
		if aheadOfUpstream(s.Dir) {
			if err := pushHead(s.Dir); err != nil {
				return res, err
			}
		}
		return res, nil
	}
	if err := os.RemoveAll(s.PackageDir(opts.Name, version)); err != nil {
		return nil, err
	}
	dirty, status, err := gitDirty(s.Dir)
	if err != nil {
		return nil, err
	}
	if dirty {
		return nil, fmt.Errorf("the local also clone is dirty:\n%s\nresolve it in %s", status, s.Dir)
	}
	if err := pullFF(s.Dir); err != nil {
		return nil, err
	}
	if isVersionCommitted(s.Dir, opts.Name, version) {
		res.Existing = true
		if aheadOfUpstream(s.Dir) {
			if err := pushHead(s.Dir); err != nil {
				return res, err
			}
		}
		return res, nil
	}
	res.Existing = false
	if err := os.RemoveAll(s.PackageDir(opts.Name, version)); err != nil {
		return nil, err
	}

	paths := []string{filepath.Join("packages", opts.Name, version)}
	if _, err := os.Stat(filepath.Join(s.Dir, SetManifestFile)); os.IsNotExist(err) {
		setMeta := map[string]string{"schema": SchemaSet, "name": s.Name}
		data, _ := json.MarshalIndent(setMeta, "", "  ")
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(s.Dir, SetManifestFile), data, 0o644); err != nil {
			return nil, err
		}
		res.SetCreated = true
		paths = append(paths, SetManifestFile)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "packages", opts.Name), 0o755); err != nil {
		return nil, err
	}
	if err := WritePackage(s.PackageDir(opts.Name, version), meta, snap); err != nil {
		return nil, err
	}
	msg := opts.Message
	if msg == "" {
		label := ""
		if opts.Label != "" {
			label = " (" + opts.Label + ")"
		}
		msg = fmt.Sprintf("also: %s@%s%s [%s]", opts.Name, version, label, harness)
	}
	committed, err := commitPaths(s.Dir, msg, paths...)
	if err != nil {
		return res, err
	}
	res.Committed = committed
	if !committed {
		res.Existing = true
		return res, nil
	}
	if err := pushHead(s.Dir); err != nil {
		return res, err
	}
	return res, nil
}
