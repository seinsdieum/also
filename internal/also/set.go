package also

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type SetRepo struct {
	Name string
	Cfg  NamespaceConfig
	Dir  string

	versionOnce sync.Once
	commits     map[string]commitInfo
	legacyTimes map[string]time.Time
	legacyOrder map[string]int
	versionErr  error
}

func OpenSet(name string, cfg NamespaceConfig, refresh bool) (*SetRepo, error) {
	return openSet(name, cfg, refresh, false)
}

func openSet(name string, cfg NamespaceConfig, refresh, offline bool) (*SetRepo, error) {
	dir := SetCachePath(cfg.Remote)
	if !isGitRepo(dir) {
		if _, err := os.Stat(dir); err == nil {
			return nil, fmt.Errorf("%s exists but is not a git repository", dir)
		}
		if offline {
			return nil, fmt.Errorf("offline: no cached clone for namespace %q (%s)", name, cfg.Remote)
		}
		if _, err := cloneOrInit(cfg.Remote, dir); err != nil {
			return nil, fmt.Errorf("open namespace %q: %w", name, err)
		}
	}
	if err := ensureLineEndings(dir); err != nil {
		return nil, err
	}
	ensureLocalExcludes(dir)
	repo := &SetRepo{Name: name, Cfg: cfg, Dir: dir}
	remote, err := runGit(dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, fmt.Errorf("namespace %q has no origin remote: %w", name, err)
	}
	if strings.TrimSpace(remote) != strings.TrimSpace(cfg.Remote) {
		return nil, fmt.Errorf("namespace %q points at %s but its remote is %s; refusing to use it", name, cfg.Remote, strings.TrimSpace(remote))
	}
	if refresh {
		if err := repo.Refresh(); err != nil {
			return nil, err
		}
	}
	return repo, nil
}

func OpenRemote(remote string, refresh bool) (*SetRepo, error) {
	return openRemote(remote, refresh, false)
}

func openRemote(remote string, refresh, offline bool) (*SetRepo, error) {
	cfg := NamespaceConfig{Remote: remote}
	return openSet("remote-"+ShortHash([]byte(remote), 6), cfg, refresh, offline)
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
	if err := pullFF(s.Dir); err != nil {
		return err
	}
	s.invalidateVersionData()
	return nil
}

func (s *SetRepo) versionData() (map[string]commitInfo, map[string]time.Time, map[string]int, error) {
	s.versionOnce.Do(func() {
		s.commits, s.versionErr = commitTimes(s.Dir)
		if s.versionErr == nil {
			s.legacyTimes, s.legacyOrder, s.versionErr = versionTimes(s.Dir)
		}
	})
	return s.commits, s.legacyTimes, s.legacyOrder, s.versionErr
}

func (s *SetRepo) invalidateVersionData() {
	s.versionOnce = sync.Once{}
	s.commits = nil
	s.legacyTimes = nil
	s.legacyOrder = nil
	s.versionErr = nil
}

func (s *SetRepo) PackageTreeDir(pkg string) string {
	return filepath.Join(s.Dir, "packages", pkg)
}

type VersionInfo struct {
	Version string
	Semver  []string
	Label   string
	Time    time.Time
	Order   int `json:"-"`
	Harness string
}

func (s *SetRepo) Versions(pkg string) ([]VersionInfo, error) {
	if !ValidName(pkg) {
		return nil, fmt.Errorf("invalid package name %q", pkg)
	}
	tags, err := listVersionTags(s.Dir, pkg)
	if err != nil {
		return nil, err
	}
	liveMeta, hasLive := readManifestAtCommit(s.Dir, pkg, "", "HEAD")
	legacy := map[string]bool{}
	if !hasLive {
		// Live packages never keep legacy version directories, so scanning
		// every origin file is wasted work for the common case.
		legacy, err = legacyVersionDirs(s.Dir, pkg)
		if err != nil {
			return nil, err
		}
	}
	if len(tags) == 0 && len(legacy) == 0 {
		// A crash between commit and tag creation must not hide the package.
		if hasLive && liveMeta.Digest != "" {
			if !validDigest(liveMeta.Digest) {
				return nil, fmt.Errorf("package %q has a malformed digest %q", pkg, liveMeta.Digest)
			}
			version := VersionFromDigest(liveMeta.Digest)
			return []VersionInfo{{
				Version: version, Label: liveMeta.Label, Harness: liveMeta.SourceHarness,
				Time: liveMeta.CreatedAt,
			}}, nil
		}
		return nil, fmt.Errorf("package %q not found in namespace %q", pkg, s.Name)
	}
	metas, batchErr := readManifestBlobs(s.Dir, pkg, tags)
	if batchErr != nil {
		metas = map[string]PackageMeta{}
		for version, commit := range tags {
			if meta, ok := readManifestAtCommit(s.Dir, pkg, version, commit); ok {
				metas[version] = meta
			}
		}
	}
	tagTimes := tagCreatedTimes(s.Dir, pkg)
	var info map[string]commitInfo
	var legacyTimes map[string]time.Time
	var legacyOrder map[string]int
	// Only manifests without a creation time need the full git history; legacy
	// version directories always need it for their per-version timestamps.
	needHistory := len(legacy) > 0
	for version := range tags {
		if needHistory {
			break
		}
		meta, ok := metas[version]
		if !ok || (meta.CreatedAt.IsZero() && tagTimes[version].IsZero()) {
			needHistory = true
			break
		}
	}
	if needHistory {
		var err error
		info, legacyTimes, legacyOrder, err = s.versionData()
		if err != nil {
			return nil, err
		}
	}
	var out []VersionInfo
	seen := map[string]bool{}
	for version, commit := range tags {
		meta, ok := metas[version]
		if !ok {
			return nil, fmt.Errorf("version tag %s has no package manifest", versionTagName(pkg, version))
		}
		if err := checkVersionDigest(pkg, version, meta); err != nil {
			return nil, err
		}
		ci := info[commit]
		vi := VersionInfo{
			Version: version, Label: meta.Label, Harness: meta.SourceHarness,
			Time: ci.Time, Order: ci.Order,
		}
		if !meta.CreatedAt.IsZero() {
			vi.Time = meta.CreatedAt
		} else if t := tagTimes[version]; !t.IsZero() {
			vi.Time = t
		} else if t := legacyTimes[pkg+"/"+version]; !t.IsZero() {
			vi.Time = t
			vi.Order = legacyOrder[pkg+"/"+version]
		}
		out = append(out, vi)
		seen[version] = true
	}
	for version := range legacy {
		if seen[version] {
			continue
		}
		meta, ok := readManifestAtCommit(s.Dir, pkg, version, "HEAD")
		if !ok {
			continue
		}
		if err := checkVersionDigest(pkg, version, meta); err != nil {
			return nil, err
		}
		key := pkg + "/" + version
		out = append(out, VersionInfo{
			Version: version, Label: meta.Label, Harness: meta.SourceHarness,
			Time: legacyTimes[key], Order: legacyOrder[key],
		})
	}
	if aliases, aerr := s.SemverAliases(pkg); aerr == nil {
		byVersion := map[string][]string{}
		for semver, content := range aliases {
			byVersion[content] = append(byVersion[content], semver)
		}
		for i := range out {
			out[i].Semver = byVersion[out[i].Version]
			sort.Strings(out[i].Semver)
		}
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

// PackageMeta returns the manifest of the live tree, falling back to the
// newest tagged version. It is the cheap lookup used by search.
func (s *SetRepo) PackageMeta(pkg string) (PackageMeta, error) {
	if meta, ok := readManifestAtCommit(s.Dir, pkg, "", "HEAD"); ok {
		return meta, nil
	}
	latest, err := s.Latest(pkg)
	if err != nil {
		return PackageMeta{}, err
	}
	p, err := s.LoadPackage(pkg, latest.Version)
	if err != nil {
		return PackageMeta{}, err
	}
	return p.Meta, nil
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
		return VersionInfo{}, fmt.Errorf("package %q has no committed versions in namespace %q", pkg, s.Name)
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
	// A semantic-version range resolves through aliases, so the immutable
	// content hash stays the version and "^2.1" is still expressible.
	if looksLikeSemverSpec(spec) {
		if aliases, aerr := s.SemverAliases(pkg); aerr == nil && len(aliases) > 0 {
			if r, ok := ParseSemverRange(spec); ok {
				var candidates []Semver
				bySemver := map[string]string{}
				for alias, content := range aliases {
					v, ok := ParseSemver(alias)
					if !ok || !r.Match(v) {
						continue
					}
					candidates = append(candidates, v)
					bySemver[v.String()] = content
				}
				if len(candidates) > 0 {
					best, _ := HighestSemver(candidates)
					content := bySemver[best.String()]
					for _, v := range versions {
						if v.Version == content {
							return v, nil
						}
					}
				}
				if strings.ContainsAny(spec, "^~><*xX") {
					var available []string
					for alias := range aliases {
						available = append(available, alias)
					}
					sort.Strings(available)
					return VersionInfo{}, fmt.Errorf("no version of %q satisfies %q; semver aliases: %s", pkg, spec, strings.Join(available, ", "))
				}
			}
		}
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

// checkVersionDigest rejects a tag or legacy directory that points at another
// version's manifest. Orphan tags and retagged histories are corruption.
func checkVersionDigest(pkg, version string, meta PackageMeta) error {
	if meta.Digest == "" {
		return fmt.Errorf("package %q version %s has no digest", pkg, version)
	}
	if !validDigest(meta.Digest) {
		return fmt.Errorf("package %q version %s has a malformed digest %q", pkg, version, meta.Digest)
	}
	if got := VersionFromDigest(meta.Digest); got != version {
		return fmt.Errorf("version %s of %q points at manifest digest %s", version, pkg, got)
	}
	return nil
}

func looksLikeSemverSpec(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	switch s[0] {
	case '^', '~', '>', '<', '*', 'v', 'V', 'x', 'X':
		return true
	}
	return s[0] >= '0' && s[0] <= '9'
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
	if !validVersion(version) {
		return nil, fmt.Errorf("invalid version %q", version)
	}
	if commit, ok := s.tagCommit(pkg, version); ok {
		p, err := s.loadPackageFromCommit(pkg, version, commit)
		if err != nil {
			return nil, fmt.Errorf("load %s@%s: %w", pkg, version, err)
		}
		if err := checkVersionDigest(pkg, version, p.Meta); err != nil {
			return nil, err
		}
		return p, nil
	}
	// Legacy layout (no tag yet): packages/<pkg>/<version>/.
	if _, ok := readManifestAtCommit(s.Dir, pkg, version, "HEAD"); ok {
		p, err := s.loadPackageFromCommit(pkg, version, "HEAD")
		if err != nil {
			return nil, fmt.Errorf("load %s@%s: %w", pkg, version, err)
		}
		if err := checkVersionDigest(pkg, version, p.Meta); err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, fmt.Errorf("version %q of %q not found", version, pkg)
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
	// AllFiles allows files outside the source harness layout into the package.
	AllFiles bool
	// AllowEmpty publishes a package with no installable components. The
	// default protects against a .gitignore that silently hides the layout.
	AllowEmpty bool
	// Semver tags this push as an exact semantic version. Bump derives the next
	// one from the highest existing alias instead.
	Semver string
	Bump   string
}

type PushResult struct {
	Package   string   `json:"package"`
	Version   string   `json:"version"`
	Digest    string   `json:"digest"`
	Label     string   `json:"label,omitempty"`
	Harness   string   `json:"harness"`
	Files     int      `json:"files"`
	Paths     []string `json:"paths,omitempty"`
	Bytes     int64    `json:"bytes"`
	Existing  bool     `json:"existing"`
	Semver    string   `json:"semver,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
	Remote    string   `json:"remote"`
	Namespace string   `json:"namespace"`
}

func (s *SetRepo) Push(bundleDir string, opts PushOptions) (*PushResult, error) {
	if !ValidName(opts.Name) {
		return nil, fmt.Errorf("invalid package name %q; use lowercase letters, digits, '.', '_' or '-' (max 64)", opts.Name)
	}
	if opts.Label != "" && !ValidName(strings.ToLower(opts.Label)) {
		return nil, fmt.Errorf("invalid label %q; use lowercase letters, digits, '.', '_' or '-'", opts.Label)
	}
	snap, warnings, err := ReadSnapshot(bundleDir, SnapshotOptions{
		AllowSecrets: opts.AllowSecrets, AllowLarge: opts.AllowLarge, GitIgnore: true,
	})
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
	classified, err := ClassifyPackage(&Package{
		Meta:  PackageMeta{Schema: SchemaPkg, Name: opts.Name, SourceHarness: h.ID},
		Files: snap.Files,
	}, h)
	if err != nil {
		return nil, err
	}
	if len(classified.Other) > 0 && !opts.AllFiles {
		return nil, fmt.Errorf("refusing to publish files outside the %s layout (use --all-files if this is intentional):\n  %s",
			h.ID, strings.Join(classified.Other, "\n  "))
	}
	if classified.Empty() && !opts.AllowEmpty {
		return nil, fmt.Errorf("package %s has no installable components for %s; check that .gitignore does not hide the layout, or pass --allow-empty for a metadata-only package", opts.Name, h.ID)
	}
	servers, mcpWarnings, err := ExtractBundleMCP(h, snap.Files, h.BundleRoots(bundleDir))
	if err != nil {
		return nil, fmt.Errorf("cannot encode MCP configuration: %w", err)
	}
	warnings = append(warnings, mcpWarnings...)
	secrets := append(MCPSuspiciousSecrets(servers), ScanConfigSecrets(snap.Files)...)
	if len(secrets) > 0 && !opts.AllowSecrets {
		return nil, fmt.Errorf("refusing to encode literal credentials (use --allow-secrets if intentional):\n  %s", strings.Join(UniqueStrings(secrets), "\n  "))
	}
	meta := PackageMeta{
		Schema: SchemaPkg, Name: opts.Name, Description: opts.Description,
		SourceHarness: harness, Label: opts.Label, EncodedWith: "also " + Version,
		CreatedAt: time.Now().UTC(),
	}
	digest := snap.Digest(meta)
	meta.Digest = digest
	version := VersionFromDigest(digest)
	res := &PushResult{
		Package: opts.Name, Version: version, Digest: digest, Label: opts.Label,
		Harness: harness, Files: len(snap.Files), Paths: snap.Paths(), Bytes: snap.TotalBytes(),
		Warnings: warnings, Remote: s.Cfg.Remote, Namespace: s.Name,
	}
	if opts.DryRun {
		semver, err := s.nextSemver(opts)
		if err != nil {
			return nil, err
		}
		res.Semver = semver
		return res, nil
	}
	s.invalidateVersionData()
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
	semver, err := s.nextSemver(opts)
	if err != nil {
		return nil, err
	}
	res.Semver = semver
	// Tag the live tree and any legacy version dirs before they are replaced.
	if err := s.ensureTagsForCurrentTree(opts.Name); err != nil {
		return nil, err
	}
	if _, ok := s.tagCommit(opts.Name, version); ok {
		res.Existing = true
		if err := s.pushExistingVersion(opts.Name, version); err != nil {
			return res, err
		}
		if err := s.pushVersionTags(opts.Name, version); err != nil {
			return res, err
		}
		if semver != "" {
			if err := s.SetSemver(opts.Name, semver, version, false); err != nil {
				return res, err
			}
		}
		return res, nil
	}

	paths := []string{filepath.Join("packages", opts.Name)}
	if _, err := os.Stat(filepath.Join(s.Dir, SetManifestFile)); os.IsNotExist(err) {
		setMeta := map[string]string{"schema": SchemaSet, "name": s.Name}
		data, _ := json.MarshalIndent(setMeta, "", "  ")
		data = append(data, '\n')
		if err := os.WriteFile(filepath.Join(s.Dir, SetManifestFile), data, 0o644); err != nil {
			return nil, err
		}
		paths = append(paths, SetManifestFile)
	}
	if err := os.MkdirAll(filepath.Join(s.Dir, "packages"), 0o755); err != nil {
		return nil, err
	}
	if err := s.removePackageTree(opts.Name); err != nil {
		return nil, err
	}
	if err := WritePackage(s.PackageTreeDir(opts.Name), meta, snap); err != nil {
		s.restorePaths(paths...)
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
		s.restorePaths(paths...)
		return res, err
	}
	if err := s.createTag(opts.Name, version, "HEAD"); err != nil {
		return res, err
	}
	s.invalidateVersionData()
	if !committed {
		res.Existing = true
	}
	if err := s.pushExistingVersion(opts.Name, version); err != nil {
		return res, err
	}
	if err := s.pushVersionTags(opts.Name, version); err != nil {
		return res, err
	}
	if semver != "" {
		if err := s.SetSemver(opts.Name, semver, version, false); err != nil {
			return res, err
		}
	}
	return res, nil
}

// nextSemver returns the alias requested by --semver or derived by --bump.
func (s *SetRepo) nextSemver(opts PushOptions) (string, error) {
	if opts.Bump == "" {
		if opts.Semver == "" {
			return "", nil
		}
		v, ok := ParseSemver(opts.Semver)
		if !ok {
			return "", fmt.Errorf("invalid --semver %q; use MAJOR.MINOR.PATCH", opts.Semver)
		}
		return v.String(), nil
	}
	switch opts.Bump {
	case "major", "minor", "patch":
	default:
		return "", fmt.Errorf("invalid --bump %q; use major, minor or patch", opts.Bump)
	}
	aliases, err := s.SemverAliases(opts.Name)
	if err != nil {
		return "", err
	}
	var known []Semver
	for alias := range aliases {
		if v, ok := ParseSemver(alias); ok {
			known = append(known, v)
		}
	}
	var next Semver
	if len(known) > 0 {
		base, _ := HighestSemver(known)
		switch opts.Bump {
		case "major":
			next = Semver{Major: base.Major + 1}
		case "minor":
			next = Semver{Major: base.Major, Minor: base.Minor + 1}
		case "patch":
			next = Semver{Major: base.Major, Minor: base.Minor, Patch: base.Patch + 1}
		}
	} else {
		switch opts.Bump {
		case "major":
			next = Semver{Major: 1}
		case "minor":
			next = Semver{Minor: 1}
		case "patch":
			next = Semver{Patch: 1}
		}
	}
	return next.String(), nil
}

// restorePaths puts a set clone back after a write or commit failure. The
// clone was checked clean before Push, so a hard reset is safe here.
func (s *SetRepo) restorePaths(paths ...string) {
	_, _ = runGit(s.Dir, "reset", "--hard", "--quiet")
	args := append([]string{"clean", "-fd", "--quiet", "--"}, paths...)
	_, _ = runGit(s.Dir, args...)
	s.invalidateVersionData()
}

// pushVersionTags pushes every local version tag. This repairs tags created by
// an earlier push or migration whose tag push failed.
func (s *SetRepo) pushVersionTags(pkg, skip string) error {
	tags, err := listVersionTags(s.Dir, pkg)
	if err != nil {
		return err
	}
	for version := range tags {
		if version == skip {
			continue
		}
		if err := pushTag(s.Dir, versionTagName(pkg, version)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SetRepo) pushExistingVersion(pkg, version string) error {
	if aheadOfUpstream(s.Dir) {
		if err := pushHead(s.Dir); err != nil {
			return err
		}
	}
	return pushTag(s.Dir, versionTagName(pkg, version))
}

// Migration describes a package converted from the legacy per-version layout.
type Migration struct {
	Package string
	Version string
}

// Migrate converts packages/<pkg>/<version>/ directories into one live tree
// per package plus immutable version tags. Old snapshots stay readable because
// tags point at the pre-migration commits.
func (s *SetRepo) Migrate() ([]Migration, error) {
	s.invalidateVersionData()
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
	packages, err := s.Packages()
	if err != nil {
		return nil, err
	}
	var migrated []Migration
	var latestTags = map[string]string{}
	for _, pkg := range packages {
		legacy, err := legacyVersionDirs(s.Dir, pkg)
		if err != nil {
			return nil, err
		}
		if len(legacy) == 0 {
			continue
		}
		// Choose the latest before creating tags, while versionTimes still has
		// per-version timestamps.
		versions, err := s.Versions(pkg)
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			continue
		}
		latest := versions[0]
		times := map[string]time.Time{}
		for _, v := range versions {
			times[v.Version] = v.Time
		}
		for version := range legacy {
			if _, exists := s.tagCommit(pkg, version); exists {
				continue
			}
			if err := s.createTagAt(pkg, version, "HEAD", times[version]); err != nil {
				return nil, err
			}
		}
		p, err := s.LoadPackage(pkg, latest.Version)
		if err != nil {
			return nil, fmt.Errorf("migrate %s@%s: %w", pkg, latest.Version, err)
		}
		p.Meta.CreatedAt = times[latest.Version]
		if p.Meta.CreatedAt.IsZero() {
			p.Meta.CreatedAt = time.Now().UTC()
		}
		if err := s.removePackageTree(pkg); err != nil {
			return nil, err
		}
		if err := WritePackage(s.PackageTreeDir(pkg), p.Meta, &Snapshot{Files: p.Files}); err != nil {
			return nil, err
		}
		migrated = append(migrated, Migration{Package: pkg, Version: latest.Version})
		latestTags[pkg] = latest.Version
	}
	if len(migrated) > 0 {
		if _, err := commitPaths(s.Dir, "also: migrate set to tree+tags layout", "packages"); err != nil {
			return nil, err
		}
		for pkg, version := range latestTags {
			if err := s.createTag(pkg, version, "HEAD"); err != nil {
				return nil, err
			}
		}
		s.invalidateVersionData()
		if aheadOfUpstream(s.Dir) {
			if err := pushHead(s.Dir); err != nil {
				return nil, err
			}
		}
	}
	// Push all tags, including ones left local by a previous failed migration.
	for _, pkg := range packages {
		if err := s.pushVersionTags(pkg, ""); err != nil {
			return nil, err
		}
	}
	return migrated, nil
}

// RepairNamespace deletes the internal clone and fetches it again. Only the
// cache is touched; user bundles and installed files stay where they are.
func RepairNamespace(name string, cfg NamespaceConfig) (string, error) {
	if cfg.Remote == "" {
		return "", fmt.Errorf("namespace %q has no remote", name)
	}
	dir := SetCachePath(cfg.Remote)
	if _, err := os.Stat(dir); err == nil {
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("remove cache %s: %w", dir, err)
		}
	}
	repo, err := openSet(name, cfg, false, false)
	if err != nil {
		return "", err
	}
	return repo.Dir, nil
}
