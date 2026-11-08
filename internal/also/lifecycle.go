package also

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ParsePkgSpec(spec string) (pkg, version string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", fmt.Errorf("package name is empty")
	}
	if i := strings.LastIndex(spec, "@"); i >= 0 {
		pkg, version = spec[:i], spec[i+1:]
	} else {
		pkg = spec
	}
	if !ValidName(pkg) {
		return "", "", fmt.Errorf("invalid package name %q; use lowercase letters, digits, '.', '_' or '-'", pkg)
	}
	if version != "" && strings.ContainsAny(version, `/\`) {
		return "", "", fmt.Errorf("invalid version %q", version)
	}
	return pkg, version, nil
}

type InstallRequest struct {
	Package        string
	Harness        string
	Scope          string
	Project        string
	Parts          []string
	Force          bool
	WithConfig     bool
	Offline        bool
	ExpectedDigest string
}

func ResolveSetForInstall(cfg *Config, setName, remote string, offline bool) (*SetRepo, error) {
	if remote != "" {
		return openRemote(remote, !offline, offline)
	}
	name, sc, err := cfg.ResolveSet(setName)
	if err != nil {
		return nil, err
	}
	return openSet(name, sc, !offline, offline)
}

func InstallPackage(cfg *Config, req InstallRequest, setName, remote string) (*Plan, *SetRepo, error) {
	pkgName, verSpec, err := ParsePkgSpec(req.Package)
	if err != nil {
		return nil, nil, err
	}
	set, err := ResolveSetForInstall(cfg, setName, remote, req.Offline)
	if err != nil {
		return nil, nil, err
	}
	version, err := set.ResolveVersion(pkgName, verSpec)
	if err != nil {
		return nil, set, err
	}
	pkg, err := set.LoadPackage(pkgName, version.Version)
	if err != nil {
		return nil, set, err
	}
	if req.ExpectedDigest != "" && pkg.Meta.Digest != req.ExpectedDigest {
		return nil, set, fmt.Errorf("package %s@%s digest %s does not match the lockfile digest %s",
			pkgName, version.Version, pkg.Meta.Digest, req.ExpectedDigest)
	}
	scope, err := cfg.Scope(req.Scope)
	if err != nil {
		return nil, set, err
	}
	project := req.Project
	if project == "" {
		project, err = os.Getwd()
		if err != nil {
			return nil, set, err
		}
	}
	policy, err := LoadPolicy(project)
	if err != nil {
		return nil, set, err
	}
	if err := checkPolicyPackage(policy, set.Name, pkg); err != nil {
		return nil, set, err
	}
	st, err := LoadState()
	if err != nil {
		return nil, set, err
	}
	opts := InstallOptions{Force: req.Force, WithConfig: req.WithConfig, Parts: req.Parts}
	plan, err := BuildPlan(st, set, pkg, version, req.Harness, scope, project, opts)
	if err != nil {
		return nil, set, err
	}
	if err := checkPolicyExecutables(policy, plan.Install.Executables); err != nil {
		return nil, set, err
	}
	if !planHasChanges(plan) {
		if len(req.Parts) > 0 {
			var available []string
			if source, ok := ResolveHarness(pkg.Meta.SourceHarness); ok {
				available = AvailableParts(pkg, source)
			}
			return nil, set, fmt.Errorf("no installable changes for the requested parts in %s: %s\navailable parts: %s",
				pkg.Meta.Name, strings.Join(req.Parts, ", "), strings.Join(available, ", "))
		}
		return nil, set, fmt.Errorf("package %s has no components installable for %s", pkg.Meta.Name, req.Harness)
	}
	if err := ApplyPlan(st, plan, opts); err != nil {
		return nil, set, err
	}
	return plan, set, nil
}

func checkPolicyPackage(p *Policy, namespace string, pkg *Package) error {
	if p == nil {
		return nil
	}
	if !p.NamespaceAllowed(namespace) {
		return fmt.Errorf("policy: namespace %q is not allowed; edit %s", namespace, PolicyFile)
	}
	if !p.PackageAllowed(pkg.Meta.Name) {
		return fmt.Errorf("policy: package %q is not allowed; edit %s", pkg.Meta.Name, PolicyFile)
	}
	source, ok := ResolveHarness(pkg.Meta.SourceHarness)
	if !ok {
		return fmt.Errorf("policy: unknown source harness %q", pkg.Meta.SourceHarness)
	}
	servers, _, _ := ExtractBundleMCP(source, pkg.Files, source.BundleRootsFromFiles(pkg.Files))
	if findings := p.CheckPackageFiles(pkg.Files, servers); len(findings) > 0 {
		return fmt.Errorf("policy: %s", strings.Join(findings, "; "))
	}
	return nil
}

func checkPolicyExecutables(p *Policy, execs []Executable) error {
	if p == nil || len(execs) == 0 {
		return nil
	}
	var kinds []string
	for _, e := range execs {
		kinds = append(kinds, e.Kind)
	}
	if !p.ExecutablesAllowed(kinds) {
		return fmt.Errorf("policy: installed components include a disallowed executable kind (%s); edit %s",
			strings.Join(UniqueStrings(kinds), ", "), PolicyFile)
	}
	return nil
}

func planHasChanges(plan *Plan) bool {
	return len(plan.NewFiles) > 0 || plan.Block != nil || len(plan.Edits) > 0
}

type UpgradeResult struct {
	Install Install
	Old     string
	New     string
	Status  string
	Err     error
}

func UpgradeInstalls(cfg *Config, st *State, pkgs []string, force, check bool) ([]UpgradeResult, error) {
	installs := filterInstalls(st.InstallsOf(""), pkgs)
	if len(installs) == 0 {
		return nil, fmt.Errorf("no matching installs")
	}
	return upgradeInstalls(cfg, st, installs, force, check)
}

// UpgradeInstallsInProject upgrades only the packages installed in one
// directory, which is what the "also upgrade" command targets. Legacy global
// installs are reported, not rewritten: their files live in the home dir.
func UpgradeInstallsInProject(cfg *Config, st *State, project string, pkgs []string, force, check, includeGlobal bool) ([]UpgradeResult, error) {
	abs, err := AbsPath(project)
	if err != nil {
		return nil, err
	}
	var installs []Install
	var legacy []UpgradeResult
	for _, in := range filterInstalls(st.InstallsOf(""), pkgs) {
		if in.Scope == "global" {
			if includeGlobal {
				legacy = append(legacy, UpgradeResult{
					Install: in, Old: in.Version, New: in.Version,
					Status: "legacy global; remove it with: also uninstall " + in.Package + " --legacy-global",
				})
			}
			continue
		}
		if filepath.Clean(in.Project) == filepath.Clean(abs) {
			installs = append(installs, in)
		}
	}
	if len(installs) == 0 && len(legacy) == 0 {
		return nil, fmt.Errorf("no packages installed in %s; run: also install <package> for <harness>", abs)
	}
	results, err := upgradeInstalls(cfg, st, installs, force, check)
	if err != nil {
		return nil, err
	}
	return append(results, legacy...), nil
}

func upgradeInstalls(cfg *Config, st *State, installs []Install, force, check bool) ([]UpgradeResult, error) {
	repos := map[string]*SetRepo{}
	var out []UpgradeResult
	for _, in := range installs {
		res := UpgradeResult{Install: in, Old: in.Version}
		repo, err := upgradeRepo(cfg, &in, repos)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		latest, err := repo.Latest(in.Package)
		if lock, lerr := LoadLock(in.Project); lerr == nil {
			if entry := lock.Find(in.Package, in.Harness); entry != nil && entry.Constraint != "" {
				latest, err = repo.ResolveVersion(in.Package, entry.Constraint)
			}
		}
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.New = latest.Version
		if latest.Version == in.Version {
			res.Status = "up to date"
			out = append(out, res)
			continue
		}
		if check {
			res.Status = "update available"
			out = append(out, res)
			continue
		}
		pkg, err := repo.LoadPackage(in.Package, latest.Version)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		policy, err := LoadPolicy(in.Project)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		if err := checkPolicyPackage(policy, in.Set, pkg); err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		opts := InstallOptions{Force: force, WithConfig: in.WithConfig, Parts: in.Parts}
		plan, err := BuildPlan(st, repo, pkg, latest, in.Harness, in.Scope, in.Project, opts)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		if err := checkPolicyExecutables(policy, plan.Install.Executables); err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		if !planHasChanges(plan) {
			res.Err = fmt.Errorf("version %s has no installable components for %s; kept the previous install", latest.Version, in.Harness)
			out = append(out, res)
			continue
		}
		if err := ApplyPlan(st, plan, opts); err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Status = "upgraded"
		out = append(out, res)
	}
	return out, nil
}

func upgradeRepo(cfg *Config, in *Install, cache map[string]*SetRepo) (*SetRepo, error) {
	key := in.Set + "\x00" + in.SetRemote
	if r, ok := cache[key]; ok {
		return r, nil
	}
	var r *SetRepo
	var err error
	if sc, ok := cfg.Namespaces[in.Set]; ok && sc.Remote == in.SetRemote {
		r, err = OpenSet(in.Set, sc, true)
	} else {
		r, err = OpenRemote(in.SetRemote, true)
	}
	if err == nil {
		cache[key] = r
	}
	return r, err
}

func filterInstalls(in []*Install, pkgs []string) []Install {
	want := map[string]bool{}
	for _, p := range pkgs {
		want[p] = true
	}
	var out []Install
	for _, i := range in {
		if len(pkgs) == 0 || want[i.Package] {
			out = append(out, *i)
		}
	}
	return out
}

func Uninstall(st *State, in *Install, force bool) ([]string, error) {
	// A resource still used by another install must survive this uninstall.
	sharedFiles := map[string]bool{}
	for _, f := range in.Files {
		for _, owner := range st.ownersOf(f.Path) {
			if owner.ID != in.ID {
				sharedFiles[f.Path] = true
				break
			}
		}
	}
	sharedBlock := st.sharedBlock(in.Block, in.ID)
	// Preflight: refuse to silently discard user changes.
	for _, f := range in.Files {
		if sharedFiles[f.Path] {
			continue
		}
		fi, err := os.Lstat(f.Path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if fi.IsDir() {
			continue
		}
		data, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		if ShortHash(data, 64) != f.SHA && !force {
			return nil, fmt.Errorf("%s was modified; use --force to remove it", f.Path)
		}
	}
	if in.Block != nil && !sharedBlock && !force {
		if data, err := os.ReadFile(in.Block.File); err == nil {
			if _, bs, be, _, found := findBlock(string(data), in.Block.ID); found {
				if blockBodyHash([]byte(data[bs:be])) != in.Block.SHA {
					return nil, fmt.Errorf("instructions block %q in %s was modified; use --force", in.Block.ID, in.Block.File)
				}
			}
		}
	}
	t := newTx()
	var warnings []string
	rollback := func(err error) ([]string, error) {
		t.rollback()
		return nil, err
	}
	for _, e := range in.Edits {
		if st.sharedConfigEdit(e, in.ID) {
			warnings = append(warnings, fmt.Sprintf("left shared config entry in %s (%s)", e.File, strings.Join(e.Path, ".")))
			continue
		}
		ok, warn, err := removeConfigEdit(t, e)
		if err != nil {
			return rollback(err)
		}
		if !ok {
			warnings = append(warnings, fmt.Sprintf("left modified config entry in %s (%s)", e.File, strings.Join(e.Path, ".")))
		}
		_ = warn
	}
	if in.Block != nil && !sharedBlock {
		if err := removeBlockTx(t, in.Block, force); err != nil {
			return rollback(err)
		}
	} else if sharedBlock {
		warnings = append(warnings, "left shared instructions block in "+in.Block.File)
	}
	for _, f := range in.Files {
		if sharedFiles[f.Path] {
			warnings = append(warnings, "left shared file "+f.Path)
			continue
		}
		if force {
			if err := t.backup(f.Path); err != nil {
				return rollback(err)
			}
		}
		if err := t.remove(f.Path); err != nil {
			return rollback(err)
		}
		pruneEmptyDirs(f.Path, pruneStops(in)...)
	}
	if err := t.snapshot(StatePath()); err != nil {
		return rollback(err)
	}
	st.Remove(in.ID)
	if err := SaveState(st); err != nil {
		return rollback(err)
	}
	return warnings, nil
}

type ReshapeResult struct {
	Install Install
	To      string
	Status  string
	Err     error
}

func Reshape(cfg *Config, st *State, to string, pkgs []string, force bool) ([]ReshapeResult, error) {
	target, ok := ResolveHarness(to)
	if !ok {
		return nil, fmt.Errorf("unknown harness %q; available: %v", to, HarnessIDs())
	}
	installs := filterInstalls(st.InstallsOf(""), pkgs)
	if len(installs) == 0 {
		return nil, fmt.Errorf("no matching installs")
	}
	repos := map[string]*SetRepo{}
	var out []ReshapeResult
	for _, in := range installs {
		res := ReshapeResult{Install: in, To: target.ID}
		if in.Harness == target.ID {
			res.Status = "already " + target.ID
			out = append(out, res)
			continue
		}
		repo, err := upgradeRepo(cfg, &in, repos)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		pkg, err := repo.LoadPackage(in.Package, in.Version)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		version := VersionInfo{Version: in.Version, Label: in.Label}
		opts := InstallOptions{Force: force, WithConfig: in.WithConfig, Parts: in.Parts, OldHarness: in.Harness}
		plan, err := BuildPlan(st, repo, pkg, version, target.ID, in.Scope, in.Project, opts)
		if err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		if !planHasChanges(plan) {
			res.Err = fmt.Errorf("package has no installable components for %s; kept the previous install", target.Display)
			out = append(out, res)
			continue
		}
		if err := ApplyPlan(st, plan, opts); err != nil {
			res.Err = err
			out = append(out, res)
			continue
		}
		res.Status = "reshaped"
		out = append(out, res)
	}
	return out, nil
}

type TreeFile struct {
	Path     string `json:"path"`
	Modified bool   `json:"modified"`
	Exists   bool   `json:"exists"`
}

type TreeInstall struct {
	Install Install    `json:"install"`
	Files   []TreeFile `json:"files"`
}

func BuildTree(st *State) []TreeInstall {
	var out []TreeInstall
	for _, in := range st.Installs {
		ti := TreeInstall{Install: in}
		for _, f := range in.Files {
			tf := TreeFile{Path: f.Path, Exists: true}
			data, err := os.ReadFile(f.Path)
			if err != nil {
				tf.Exists = false
				tf.Modified = true
			} else if ShortHash(data, 64) != f.SHA {
				tf.Modified = true
			}
			ti.Files = append(ti.Files, tf)
		}
		out = append(out, ti)
	}
	return out
}

func pruneEmptyDirs(path string, stops ...string) {
	dir := filepath.Dir(path)
	for {
		if dir == "" || dir == "." || dir == string(filepath.Separator) {
			return
		}
		stop := false
		for _, s := range stops {
			if s != "" && dir == s {
				stop = true
				break
			}
		}
		home, _ := os.UserHomeDir()
		if stop || dir == home {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func pruneStops(in *Install) []string {
	home, _ := os.UserHomeDir()
	return []string{in.Root, in.Project, home, HomeDir()}
}

func InstalledAt(in Install) string {
	if in.InstalledAt.IsZero() {
		return "unknown"
	}
	return in.InstalledAt.Local().Format("2006-01-02 15:04")
}
