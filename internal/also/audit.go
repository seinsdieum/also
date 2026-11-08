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

// Audit is the inventory layer: what is installed, where it came from, what can
// execute, and whether the files on disk still match the managed state. It never
// downloads a package; the recorded digest is the evidence.
const SchemaAudit = "also.audit/v1"

type AuditFinding struct {
	Severity string `json:"severity"` // fail | warn | info
	Code     string `json:"code"`
	Package  string `json:"package,omitempty"`
	Harness  string `json:"harness,omitempty"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type AuditPackage struct {
	Package       string       `json:"package"`
	Namespace     string       `json:"namespace"`
	Version       string       `json:"version"`
	Digest        string       `json:"digest,omitempty"`
	Label         string       `json:"label,omitempty"`
	Harness       string       `json:"harness"`
	SourceHarness string       `json:"source_harness"`
	Project       string       `json:"project,omitempty"`
	Parts         []string     `json:"parts,omitempty"`
	Exclude       []string     `json:"exclude,omitempty"`
	InstalledAt   time.Time    `json:"installed_at"`
	Files         int          `json:"files"`
	Missing       int          `json:"missing"`
	Modified      int          `json:"modified"`
	Shared        int          `json:"shared"`
	Block         string       `json:"block,omitempty"`
	Executables   []Executable `json:"executables,omitempty"`
}

type AuditSummary struct {
	Packages     int `json:"packages"`
	Files        int `json:"files"`
	Missing      int `json:"missing"`
	Modified     int `json:"modified"`
	Shared       int `json:"shared"`
	Executables  int `json:"executables"`
	LockEntries  int `json:"lock_entries"`
	LockVerified int `json:"lock_verified"`
	Findings     int `json:"findings"`
	Failures     int `json:"failures"`
}

// AuditLock describes the project lockfile as seen by audit. The lockfile is
// the committed root of trust; state is only the local cache of what was
// written on this machine.
type AuditLock struct {
	File     string `json:"file"`
	Exists   bool   `json:"exists"`
	InGit    bool   `json:"in_git,omitempty"`
	Tracked  bool   `json:"tracked,omitempty"`
	Entries  int    `json:"entries"`
	Verified int    `json:"verified"`
}

type AuditReport struct {
	Schema      string         `json:"schema"`
	Project     string         `json:"project,omitempty"`
	All         bool           `json:"all,omitempty"`
	GeneratedAt time.Time      `json:"generated_at"`
	Packages    []AuditPackage `json:"packages"`
	Findings    []AuditFinding `json:"findings"`
	Summary     AuditSummary   `json:"summary"`
	Lock        *AuditLock     `json:"lock,omitempty"`
	PolicyFile  string         `json:"policy_file,omitempty"`
}

// Audit builds the report for one project (or every tracked install with all).
// requireLock makes a missing, untracked or unverifiable lockfile a failure;
// "audit --ci" sets it.
// gitTrackedPaths returns the tracked paths of one project, keyed by PathKey.
// A project outside git has no tracked paths.
func gitTrackedPaths(project string) map[string]bool {
	root, err := GitProjectRoot(project)
	if err != nil {
		return nil
	}
	out, err := runGitBytes(root, "ls-files", "-z")
	if err != nil {
		return nil
	}
	tracked := map[string]bool{}
	for _, rel := range bytes.Split(out, []byte{0}) {
		if len(rel) == 0 {
			continue
		}
		tracked[PathKey(filepath.Join(root, string(rel)))] = true
	}
	return tracked
}

// trackedInfraFindings reports managed files, instruction blocks and config
// edits that ended up in git. This is the explicit guard behind
// "also exclude infra": an installed file is infrastructure, not product code.
func trackedInfraFindings(in *Install, tracked map[string]bool, requireLock bool) []AuditFinding {
	if len(tracked) == 0 {
		return nil
	}
	severity := "warn"
	if requireLock {
		severity = "fail"
	}
	var out []AuditFinding
	seen := map[string]bool{}
	add := func(code, path, detail string) {
		key := code + "\x00" + path
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, AuditFinding{
			Severity: severity, Code: code, Package: in.Package, Harness: in.Harness,
			Path: path, Detail: detail,
		})
	}
	for _, f := range in.Files {
		if tracked[PathKey(f.Path)] {
			add("infra-tracked", f.Path, "installed infrastructure file is tracked by git; run also exclude infra for "+in.Harness+" and untrack it")
		}
	}
	if in.Block != nil && tracked[PathKey(in.Block.File)] {
		add("block-tracked", in.Block.File, "managed instruction block is in a tracked file; keep the harness instructions out of the product history")
	}
	for _, e := range in.Edits {
		if tracked[PathKey(e.File)] {
			add("config-tracked", e.File, "managed config edit is in a tracked file; keep generated harness config out of the product history")
		}
	}
	return out
}

func harnessRootNames() map[string]bool {
	roots := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSuffix(filepath.ToSlash(p), "/")
		if p == "" || p == "." {
			return
		}
		if i := strings.IndexByte(p, '/'); i > 0 {
			p = p[:i]
		}
		if p != "" && p != "." {
			roots[p] = true
		}
	}
	for _, h := range Harnesses() {
		add(h.ExtraBase)
		add(h.Project.Skills)
		add(h.Project.Commands)
		add(h.Project.Agents)
		add(h.Project.Plugins)
		add(h.Project.Hooks)
		for _, c := range h.Components {
			add(c.ProjectDir)
		}
	}
	return roots
}

func instructionNames() map[string]bool {
	out := map[string]bool{}
	for _, h := range Harnesses() {
		if base := filepath.Base(h.Project.Instr); base != "" && base != "." {
			out[base] = true
		}
	}
	return out
}

// harnessConfigPaths lists every native config file an adapter can read. The
// audit must use the adapter's full config list, not only the primary path:
// OpenCode, for example, accepts both opencode.json and opencode.jsonc.
func harnessConfigPaths(h *Harness) map[string]bool {
	out := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSuffix(filepath.ToSlash(p), "/")
		if p == "" || p == "." || filepath.IsAbs(p) {
			return
		}
		out[p] = true
	}
	add(h.Project.MCP)
	add(h.Project.Settings)
	add(h.Project.HooksJSON)
	for _, c := range h.MCP.Configs {
		add(c)
	}
	for _, c := range h.Settings.Configs {
		add(c)
	}
	for _, c := range h.Hooks.Configs {
		add(c)
	}
	return out
}

// configHasExecutable reports whether a tracked config file defines an
// executable surface: an MCP server, hook, plugin or command. A user-owned
// config with only cosmetic settings is not infrastructure and stays quiet.
func configHasExecutable(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if strings.EqualFold(filepath.Ext(path), ".toml") {
		body := string(data)
		for _, key := range []string{"mcp_servers", "[hooks", "[[hooks", "plugins"} {
			if strings.Contains(body, key) {
				return true
			}
		}
		return false
	}
	doc, err := ParseJSONC(data)
	if err != nil {
		return false
	}
	for _, path := range [][]string{
		{"mcp", "servers"}, {"mcpServers"}, {"mcp_servers"}, {"mcp"},
		{"hooks"}, {"plugins"}, {"commands"},
	} {
		raw, ok := doc.RawAt(path...)
		if !ok {
			continue
		}
		if jsonEqual(raw, []byte("{}")) || jsonEqual(raw, []byte("[]")) || string(raw) == "null" {
			continue
		}
		return true
	}
	return false
}

// staleInfraFindings catches tracked files under a harness component
// directory that no current install owns. They are leftovers from a previous
// harness or a manual copy, and the artifact-free contract says they must not
// be in the product history.
func staleInfraFindings(st *State, root string, tracked map[string]bool, requireLock bool) []AuditFinding {
	if len(tracked) == 0 {
		return nil
	}
	severity := "warn"
	if requireLock {
		severity = "fail"
	}
	roots := harnessRootNames()
	instructions := instructionNames()
	bases := []string{root}
	for idx := range st.Installs {
		in := &st.Installs[idx]
		if in.Project == "" {
			continue
		}
		rel, err := filepath.Rel(root, in.Project)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			bases = append(bases, in.Project)
		}
	}
	var out []AuditFinding
	seen := map[string]bool{}
	add := func(code, path, detail string) {
		key := code + "\x00" + path
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, AuditFinding{Severity: severity, Code: code, Path: path, Detail: detail})
	}
	managed := func(path string) bool {
		key := PathKey(path)
		for idx := range st.Installs {
			in := &st.Installs[idx]
			for _, f := range in.Files {
				if PathKey(f.Path) == key {
					return true
				}
			}
			if in.Block != nil && PathKey(in.Block.File) == key {
				return true
			}
			for _, e := range in.Edits {
				if PathKey(e.File) == key {
					return true
				}
			}
		}
		return false
	}
	for path := range tracked {
		if managed(path) {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		for _, base := range bases {
			baseRel, berr := filepath.Rel(base, path)
			if berr != nil || baseRel == ".." || strings.HasPrefix(baseRel, ".."+string(filepath.Separator)) {
				continue
			}
			relSlash := filepath.ToSlash(baseRel)
			for _, h := range Harnesses() {
				if !harnessConfigPaths(h)[relSlash] {
					continue
				}
				if configHasExecutable(path) {
					add("stale-infra", path, "tracked config for "+h.ID+" defines executable components but is not owned by an install; remove it or untrack it (git rm --cached)")
				}
				break
			}
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		// Any tracked file below a known harness root (including a nested
		// project or another harness than the one currently installed) is
		// infrastructure, not product code.
		for i := 0; i+1 < len(parts); i++ {
			if roots[parts[i]] {
				add("stale-infra", path, "tracked file is under a harness directory but is not owned by an install; remove it or untrack it (git rm --cached)")
				break
			}
		}
		if instructions[filepath.Base(path)] {
			if data, rerr := os.ReadFile(path); rerr == nil && bytes.Contains(data, []byte(blockBeginPrefix)) {
				add("block-tracked", path, "managed instruction block is tracked but not owned by an install; remove the block or untrack the file (git rm --cached)")
			}
		}
	}
	return out
}

func Audit(st *State, project string, all, requireLock bool) (*AuditReport, error) {
	if st == nil {
		return nil, fmt.Errorf("no install state")
	}
	abs, err := AbsPath(project)
	if err != nil {
		return nil, err
	}
	report := &AuditReport{Schema: SchemaAudit, Project: abs, All: all, GeneratedAt: time.Now().UTC(),
		Packages: []AuditPackage{}, Findings: []AuditFinding{}}
	lock, lockExists, lockInGit, lockTracked, err := loadAuditLock(abs)
	if err != nil {
		return nil, err
	}
	report.Lock = &AuditLock{
		File: LockfilePath(abs), Exists: lockExists, InGit: lockInGit,
		Tracked: lockTracked, Entries: len(lock.Packages),
	}
	if !lockExists {
		severity := "warn"
		if requireLock {
			severity = "fail"
		}
		report.Findings = append(report.Findings, AuditFinding{
			Severity: severity, Code: "lock-missing",
			Detail: "also.lock is the committed source of truth; run also install to create it and commit it",
		})
	} else if lockInGit && !lockTracked {
		severity := "warn"
		if requireLock {
			severity = "fail"
		}
		report.Findings = append(report.Findings, AuditFinding{
			Severity: severity, Code: "lock-untracked",
			Detail: "also.lock exists but is not tracked by git; commit it so CI and other machines share the pins",
		})
	}
	type lockKey struct{ namespace, pkg, harness string }
	locked := map[lockKey]LockEntry{}
	for _, entry := range lock.Packages {
		locked[lockKey{entry.Namespace, entry.Package, entry.Harness}] = entry
	}
	projectInstalls := map[lockKey]*Install{}
	policies := map[string]*Policy{}
	policyStatus := map[string]*PolicyVerification{}
	trackedCache := map[string]map[string]bool{}
	for idx := range st.Installs {
		in := &st.Installs[idx]
		if !all {
			if in.Scope == "global" || PathKey(in.Project) != PathKey(abs) {
				continue
			}
		}
		ap := AuditPackage{
			Package: in.Package, Namespace: in.Set, Version: in.Version, Digest: in.Digest,
			Label: in.Label, Harness: in.Harness, SourceHarness: in.SourceHarness,
			Project: in.Project, Parts: in.Parts, Exclude: in.Exclude, InstalledAt: in.InstalledAt,
			Files: len(in.Files), Executables: in.Executables,
		}
		if PathKey(in.Project) == PathKey(abs) {
			projectInstalls[lockKey{in.Set, in.Package, in.Harness}] = in
		}
		add := func(severity, code, path, detail string) {
			report.Findings = append(report.Findings, AuditFinding{
				Severity: severity, Code: code, Package: in.Package, Harness: in.Harness,
				Path: path, Detail: detail,
			})
		}
		if in.Scope == "global" {
			add("warn", "legacy-global", "", "old global install; remove it with: also uninstall "+in.Package+" --legacy-global")
		}
		if in.Project != "" {
			root, rerr := GitProjectRoot(in.Project)
			if rerr == nil {
				tracked, ok := trackedCache[root]
				if !ok {
					tracked = gitTrackedPaths(root)
					trackedCache[root] = tracked
				}
				report.Findings = append(report.Findings, trackedInfraFindings(in, tracked, requireLock)...)
			}
		}
		if in.Digest == "" {
			add("warn", "digest-missing", "", "state predates recorded digests; reinstall to make the inventory verifiable")
		}
		for _, f := range in.Files {
			if len(st.ownersOf(f.Path)) > 1 {
				ap.Shared++
				add("info", "shared-file", f.Path, "also owned by another install")
			}
			data, err := os.ReadFile(f.Path)
			if err != nil {
				if os.IsNotExist(err) {
					ap.Missing++
					add("fail", "file-missing", f.Path, "recorded file is gone")
					continue
				}
				add("fail", "file-unreadable", f.Path, err.Error())
				continue
			}
			if ShortHash(data, 64) != f.SHA {
				ap.Modified++
				add("warn", "file-modified", f.Path, "content differs from the installed version")
			}
		}
		if in.Block != nil {
			state, detail := auditBlock(in.Block)
			ap.Block = state
			switch state {
			case "missing":
				add("fail", "block-missing", in.Block.File, detail)
			case "modified":
				add("warn", "block-modified", in.Block.File, detail)
			}
			if st.sharedBlock(in.Block, in.ID) {
				add("info", "shared-block", in.Block.File, "managed block is shared with another install")
			}
		}
		for _, e := range in.Edits {
			state, detail := auditConfigEdit(e)
			if state == "ok" {
				continue
			}
			severity := "warn"
			code := "config-modified"
			if state == "missing" {
				severity, code = "fail", "config-missing"
			}
			add(severity, code, e.File, detail)
			if st.sharedConfigEdit(e, in.ID) {
				add("info", "shared-config", e.File, "config entry is shared with another install")
			}
		}
		if in.Project != "" {
			p, ok := policies[in.Project]
			if !ok {
				p, err = LoadPolicy(in.Project)
				if err != nil {
					return nil, err
				}
				policies[in.Project] = p
			}
			if p != nil {
				report.PolicyFile = filepath.Join(in.Project, PolicyFile)
				status, ok := policyStatus[in.Project]
				if !ok {
					var verr error
					status, verr = VerifyPolicy(in.Project)
					if verr != nil {
						add("fail", "policy-signature-invalid", "", verr.Error())
					}
					policyStatus[in.Project] = status
				}
				if status == nil || !status.Signed {
					severity := "warn"
					if requireLock {
						severity = "fail"
					}
					add(severity, "policy-unsigned", "", "policy is not signed; run also policy sign")
				} else if !status.Valid {
					add("fail", "policy-signature-invalid", "", status.Error)
				}
				if !p.NamespaceAllowed(in.Set) {
					add("fail", "policy-namespace", "", "namespace is not allowed by "+PolicyFile)
				}
				if !p.PackageAllowed(in.Package) {
					add("fail", "policy-package", "", "package is not allowed by "+PolicyFile)
				}
				if !p.ExecutablesAllowed(executableKinds(in.Executables)) {
					add("fail", "policy-executable", "", "installed executable kind is not allowed by "+PolicyFile)
				}
			}
		}
		report.Packages = append(report.Packages, ap)
	}
	if lockExists {
		if _, ok := trackedCache[abs]; !ok {
			trackedCache[abs] = gitTrackedPaths(abs)
		}
	}
	for root, tracked := range trackedCache {
		report.Findings = append(report.Findings, staleInfraFindings(st, root, tracked, requireLock)...)
	}
	if lockExists {
		addLockFinding := func(severity, code, pkg, harness, detail string) {
			report.Findings = append(report.Findings, AuditFinding{
				Severity: severity, Code: code, Package: pkg, Harness: harness, Detail: detail,
			})
		}
		for key, entry := range locked {
			if err := entry.CheckEntry(); err != nil {
				addLockFinding("fail", "lock-invalid", entry.Package, entry.Harness, err.Error())
				continue
			}
			if entry.Semver != "" && entry.Constraint != "" {
				if v, ok := ParseSemver(entry.Semver); ok {
					if rng, ok := ParseSemverRange(entry.Constraint); ok && !rng.Match(v) {
						severity := "warn"
						if requireLock {
							severity = "fail"
						}
						addLockFinding(severity, "lock-constraint-mismatch", entry.Package, entry.Harness,
							"also.lock pins "+entry.Semver+" but the constraint "+entry.Constraint+" does not match it")
					}
				}
			}
			in := projectInstalls[key]
			if in == nil {
				addLockFinding("fail", "lock-not-installed", entry.Package, entry.Harness,
					"lockfile pins this package but it is not installed in this directory; run: also install --frozen")
				continue
			}
			matches := true
			if in.Version != entry.Version {
				matches = false
				addLockFinding("fail", "lock-version-mismatch", entry.Package, entry.Harness,
					"installed "+in.Version+" but also.lock pins "+entry.Version)
			}
			if entry.Digest == "" {
				severity := "warn"
				if requireLock {
					severity = "fail"
				}
				matches = false
				addLockFinding(severity, "lock-digest-missing", entry.Package, entry.Harness,
					"also.lock has no full digest; reinstall to record it")
			} else if in.Digest != entry.Digest {
				matches = false
				addLockFinding("fail", "lock-digest-mismatch", entry.Package, entry.Harness,
					"installed digest "+in.Digest+" but also.lock pins "+entry.Digest)
			}
			if in.Set != entry.Namespace {
				matches = false
				addLockFinding("fail", "lock-namespace-mismatch", entry.Package, entry.Harness,
					"installed from "+in.Set+" but also.lock pins "+entry.Namespace)
			}
			if !sameStringSet(in.Parts, entry.Parts) || !sameStringSet(in.Exclude, entry.Exclude) || in.WithConfig != entry.WithConfig {
				addLockFinding("warn", "lock-config-mismatch", entry.Package, entry.Harness,
					"installed parts or config selection differ from also.lock")
			}
			if matches {
				report.Lock.Verified++
			}
		}
		for key, in := range projectInstalls {
			if _, ok := locked[key]; ok {
				continue
			}
			severity := "warn"
			if requireLock {
				severity = "fail"
			}
			addLockFinding(severity, "lock-extra", in.Package, in.Harness,
				"installed but not pinned in also.lock; run also install to record it")
		}
	}
	// A path can be reported by both the install loop and the stale scan.
	seenFindings := map[string]bool{}
	deduped := report.Findings[:0]
	for _, f := range report.Findings {
		key := f.Code + "\x00" + f.Path
		if f.Path == "" {
			key += "\x00" + f.Package + "\x00" + f.Harness + "\x00" + f.Detail
		}
		if seenFindings[key] {
			continue
		}
		seenFindings[key] = true
		deduped = append(deduped, f)
	}
	report.Findings = deduped
	sort.Slice(report.Packages, func(i, j int) bool {
		a, b := report.Packages[i], report.Packages[j]
		if a.Project != b.Project {
			return a.Project < b.Project
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		return a.Harness < b.Harness
	})
	sort.Slice(report.Findings, func(i, j int) bool {
		a, b := report.Findings[i], report.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Path < b.Path
	})
	report.Summarize()
	return report, nil
}

// loadAuditLock reports whether the lock exists and whether git tracks it.
// A project outside git still has a lock; it simply cannot be "tracked".
func loadAuditLock(project string) (*Lockfile, bool, bool, bool, error) {
	path := LockfilePath(project)
	exists := false
	if _, err := os.Stat(path); err == nil {
		exists = true
	} else if !os.IsNotExist(err) {
		return nil, false, false, false, err
	}
	lock, err := LoadLock(project)
	if err != nil {
		return nil, false, false, false, err
	}
	inGit, tracked := false, false
	if _, err := GitProjectRoot(project); err == nil {
		inGit = true
		if _, err := runGit(project, "ls-files", "--error-unmatch", "--", LockFile); err == nil {
			tracked = true
		}
	}
	return lock, exists, inGit, tracked, nil
}

func (r *AuditReport) Summarize() {
	r.Summary = AuditSummary{}
	if r.Lock != nil {
		r.Summary.LockEntries = r.Lock.Entries
		r.Summary.LockVerified = r.Lock.Verified
	}
	for _, p := range r.Packages {
		r.Summary.Packages++
		r.Summary.Files += p.Files
		r.Summary.Missing += p.Missing
		r.Summary.Modified += p.Modified
		r.Summary.Shared += p.Shared
		r.Summary.Executables += len(p.Executables)
	}
	r.Summary.Findings = len(r.Findings)
	for _, f := range r.Findings {
		if f.Severity == "fail" {
			r.Summary.Failures++
		}
	}
}

func (r *AuditReport) Components() []SBOMComponent {
	out := make([]SBOMComponent, 0, len(r.Packages))
	for _, p := range r.Packages {
		out = append(out, SBOMComponent{
			Name: p.Package, Namespace: p.Namespace, Version: p.Version, Digest: p.Digest,
			Harness: p.Harness, SourceHarness: p.SourceHarness, Files: p.Files, Parts: p.Parts,
		})
	}
	return out
}

func sameStringSet(a, b []string) bool {
	as, bs := map[string]bool{}, map[string]bool{}
	for _, s := range a {
		as[s] = true
	}
	for _, s := range b {
		bs[s] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for s := range as {
		if !bs[s] {
			return false
		}
	}
	return true
}

func executableKinds(execs []Executable) []string {
	var out []string
	for _, e := range execs {
		out = append(out, e.Kind)
	}
	return UniqueStrings(out)
}

func auditBlock(b *BlockEdit) (string, string) {
	data, err := os.ReadFile(b.File)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", "instructions file is gone"
		}
		return "unreadable", err.Error()
	}
	_, bs, be, _, found := findBlock(string(data), b.ID)
	if !found {
		return "missing", "managed block " + b.ID + " is gone"
	}
	if blockBodyHash(data[bs:be]) != b.SHA {
		return "modified", "managed block " + b.ID + " was edited"
	}
	return "ok", ""
}

func auditConfigEdit(e ConfigEdit) (string, string) {
	data, err := os.ReadFile(e.File)
	if err != nil {
		if os.IsNotExist(err) {
			return "missing", "config file is gone"
		}
		return "unreadable", err.Error()
	}
	switch e.Kind {
	case "json-set":
		doc, err := ParseJSONC(data)
		if err != nil {
			return "unreadable", err.Error()
		}
		raw, ok := doc.RawAt(e.Path...)
		if !ok {
			return "missing", strings.Join(e.Path, ".") + " is gone"
		}
		if !jsonEqual(raw, e.Value) {
			return "modified", strings.Join(e.Path, ".") + " was changed"
		}
		return "ok", ""
	case "json-append":
		doc, err := ParseJSONC(data)
		if err != nil {
			return "unreadable", err.Error()
		}
		raw, ok := doc.RawAt(e.Path...)
		if !ok {
			return "missing", strings.Join(e.Path, ".") + " is gone"
		}
		var arr []json.RawMessage
		if json.Unmarshal(raw, &arr) != nil {
			return "modified", strings.Join(e.Path, ".") + " is not an array"
		}
		for _, el := range arr {
			if jsonEqual(el, e.Value) {
				return "ok", ""
			}
		}
		return "missing", strings.Join(e.Path, ".") + " entry is gone"
	case "toml-block":
		body, ok := tomlBlockBody(data, e.Marker)
		if !ok {
			return "missing", "managed block " + e.Marker + " is gone"
		}
		if strings.TrimSpace(body) != strings.TrimSpace(e.Block) {
			return "modified", "managed block " + e.Marker + " was edited"
		}
		return "ok", ""
	default:
		return "unknown", "unsupported config edit kind " + e.Kind
	}
}
