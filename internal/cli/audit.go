package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seinsdieum/also/internal/also"

	"github.com/spf13/cobra"
)

func auditProject(dir string) (string, error) {
	if dir != "" {
		return also.AbsPath(dir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return also.FindProjectRoot(cwd)
}

func newAuditCmd() *cobra.Command {
	var (
		all        bool
		jsonOut    bool
		ci         bool
		strict     bool
		sbom       string
		dir        string
		namespace  string
		drift      bool
		verify     bool
		showPolicy bool
		refresh    bool
	)
	c := newCmd("audit", "Show what is installed, where it came from and what changed")
	c.Long = `Reports what is installed in this directory (or everywhere with --all):
the namespace and content version it came from, every recorded file, managed
instruction block and config edit, and every component that can execute code:
MCP servers, hooks, plugins and commands.

Three focused modes use the same command:
  --drift    only the files, blocks and config entries that differ
  --verify   namespace contents, version tags and install references
  --policy   the effective also.policy.yml and its signature status

Exit code is 0 unless --ci is set and the report contains a failure. A
modified user file is a warning; a missing file, block or config entry is a
failure. --sbom writes a CycloneDX 1.5 JSON SBOM for the inventory.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return fmt.Errorf("usage: also audit [--all] [--json] [--ci] [--strict] [--drift|--verify|--policy] [--sbom <path>]")
		}
		modes := 0
		for _, on := range []bool{drift, verify, showPolicy} {
			if on {
				modes++
			}
		}
		if modes > 1 {
			return fmt.Errorf("choose one of --drift, --verify or --policy")
		}
		project, err := auditProject(dir)
		if err != nil {
			return err
		}
		if showPolicy {
			if err := printPolicy(project, jsonOut); err != nil {
				return err
			}
			if ci {
				status, err := also.VerifyPolicy(project)
				if err != nil {
					return err
				}
				if status == nil {
					return fmt.Errorf("no policy at %s", filepath.Join(project, also.PolicyFile))
				}
				if !status.Valid {
					return fmt.Errorf("policy verification failed")
				}
			}
			return nil
		}
		st, err := also.LoadState()
		if err != nil {
			return err
		}
		if verify {
			cfg, err := also.LoadConfig()
			if err != nil {
				return err
			}
			checks := also.Verify(cfg, st, namespace, refresh)
			if err := printChecks(checks, jsonOut); err != nil {
				return err
			}
			if also.HasFailures(checks) {
				return fmt.Errorf("verify found failures")
			}
			return nil
		}
		report, err := also.Audit(st, project, all, ci)
		if err != nil {
			return err
		}
		if drift {
			return printDrift(report, jsonOut)
		}
		if sbom != "" {
			if sbom == "-" && jsonOut {
				return fmt.Errorf("--sbom - and --json cannot both write to stdout")
			}
			if err := writeSBOM(sbom, report); err != nil {
				return err
			}
		}
		if sbom == "-" {
			if ci && report.Summary.Failures > 0 {
				return fmt.Errorf("audit found %d failure(s)", report.Summary.Failures)
			}
			if strict {
				if n := actionableFindings(report); n > 0 {
					return fmt.Errorf("audit found %d finding(s)", n)
				}
			}
			return nil
		}
		if jsonOut {
			if err := printJSON(report); err != nil {
				return err
			}
		} else {
			printAudit(report)
		}
		if ci && report.Summary.Failures > 0 {
			return fmt.Errorf("audit found %d failure(s)", report.Summary.Failures)
		}
		if strict {
			if n := actionableFindings(report); n > 0 {
				return fmt.Errorf("audit found %d finding(s)", n)
			}
		}
		return nil
	}
	c.Flags().BoolVar(&all, "all", false, "include installs from every directory and legacy global installs")
	c.Flags().BoolVar(&jsonOut, "json", false, "print JSON")
	c.Flags().BoolVar(&ci, "ci", false, "exit 1 when the report contains a failure")
	c.Flags().BoolVar(&strict, "strict", false, "exit 1 on any finding, including user edits")
	c.Flags().BoolVar(&drift, "drift", false, "show only files, blocks and config entries that differ")
	c.Flags().BoolVar(&verify, "verify", false, "verify namespace contents, tags and install references")
	c.Flags().BoolVar(&showPolicy, "policy", false, "show the effective also.policy.yml")
	c.Flags().StringVar(&namespace, "namespace", "", "namespace to verify (with --verify)")
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch namespaces before verifying")
	c.Flags().StringVar(&sbom, "sbom", "", "write a CycloneDX SBOM to this path (- for stdout)")
	c.Flags().StringVarP(&dir, "dir", "p", "", "directory to audit (default: current directory)")
	return c
}

func printDrift(report *also.AuditReport, jsonOut bool) error {
	if jsonOut {
		if err := printJSON(map[string]any{
			"schema": report.Schema, "project": report.Project,
			"generated_at": report.GeneratedAt, "findings": report.Findings,
			"summary": report.Summary,
		}); err != nil {
			return err
		}
	} else if len(report.Findings) == 0 {
		fmt.Printf("no drift: %d package(s) match the recorded state\n", report.Summary.Packages)
	} else {
		printFindings(report.Findings)
	}
	if report.Summary.Failures > 0 {
		return fmt.Errorf("drift: %d failure(s)", report.Summary.Failures)
	}
	return nil
}

func writeSBOM(path string, report *also.AuditReport) error {
	if path == "-" {
		return also.WriteCycloneDX(os.Stdout, report.Components())
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return also.WriteCycloneDX(f, report.Components())
}

// actionableFindings counts warnings and failures; shared identical resources
// are reported as info and do not fail --strict.
func actionableFindings(report *also.AuditReport) int {
	n := 0
	for _, f := range report.Findings {
		if f.Severity != "info" {
			n++
		}
	}
	return n
}

func printAudit(report *also.AuditReport) {
	scope := report.Project
	if report.All {
		scope = "all tracked installs"
	}
	fmt.Printf("inventory: %s\n", scope)
	if report.Lock != nil {
		switch {
		case !report.Lock.Exists:
			fmt.Println("lock:      missing (also.lock is the committed source of truth)")
		case report.Lock.InGit && !report.Lock.Tracked:
			fmt.Printf("lock:      %d entries, %d verified, not tracked by git\n", report.Lock.Entries, report.Lock.Verified)
		default:
			fmt.Printf("lock:      %d entries, %d verified\n", report.Lock.Entries, report.Lock.Verified)
		}
	}
	if report.Summary.Packages == 0 {
		fmt.Println("no packages installed")
		return
	}
	w := newTabWriter()
	if report.All {
		fmt.Fprintf(w, "PROJECT\tPACKAGE\tVERSION\tHARNESS\tSOURCE\tFILES\tDRIFT\tEXEC\n")
	} else {
		fmt.Fprintf(w, "PACKAGE\tVERSION\tHARNESS\tSOURCE\tFILES\tDRIFT\tEXEC\n")
	}
	for _, p := range report.Packages {
		drift := "-"
		switch {
		case p.Missing > 0:
			drift = fmt.Sprintf("missing:%d", p.Missing)
		case p.Modified > 0:
			drift = fmt.Sprintf("modified:%d", p.Modified)
		}
		if report.All {
			project := p.Project
			if project == "" {
				project = "(global)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\n",
				project, p.Package, short(p.Version), p.Harness, p.SourceHarness, p.Files, drift, len(p.Executables))
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%d\n",
			p.Package, short(p.Version), p.Harness, orDash(p.SourceHarness), p.Files, drift, len(p.Executables))
	}
	_ = w.Flush()
	if len(report.Findings) > 0 {
		printFindings(report.Findings)
	}
	fmt.Printf("%d package(s), %d file(s), %d executable component(s), %d finding(s), %d failure(s)\n",
		report.Summary.Packages, report.Summary.Files, report.Summary.Executables,
		report.Summary.Findings, report.Summary.Failures)
}

func printFindings(findings []also.AuditFinding) {
	fmt.Println("findings:")
	for _, f := range findings {
		where := f.Path
		if where == "" {
			where = f.Package + "/" + f.Harness
		}
		fmt.Printf("  %-4s %-18s %s: %s\n", f.Severity, f.Code, where, f.Detail)
	}
}

func printPolicy(project string, jsonOut bool) error {
	p, err := also.LoadPolicy(project)
	if err != nil {
		return err
	}
	path := filepath.Join(project, also.PolicyFile)
	if p == nil {
		if jsonOut {
			return printJSON(map[string]any{"file": path, "exists": false})
		}
		fmt.Printf("no policy at %s\n", path)
		return nil
	}
	status, err := also.VerifyPolicy(project)
	if err != nil {
		return err
	}
	if jsonOut {
		return printJSON(map[string]any{
			"file": path, "exists": true, "namespaces": p.Namespaces,
			"packages": p.Packages, "executables": p.Executables, "limits": p.Limits,
			"governance": p.Governance, "signature": status,
		})
	}
	fmt.Printf("policy: %s\n", path)
	fmt.Printf("namespaces: allow=%v deny=%v\n", p.Namespaces.Allow, p.Namespaces.Deny)
	fmt.Printf("packages:   allow=%v deny=%v\n", p.Packages.Allow, p.Packages.Deny)
	fmt.Printf("executables: hooks=%s mcp=%s plugins=%s commands=%s\n",
		policyBool(p.Executables.AllowHooks), policyBool(p.Executables.AllowMCP),
		policyBool(p.Executables.AllowPlugins), policyBool(p.Executables.AllowCommands))
	fmt.Printf("limits:     max_package_bytes=%d allow_literal_secrets=%t\n",
		p.Limits.MaxPackageBytes, p.Limits.AllowLiteralSecrets)
	printPolicySignature(p, status)
	return nil
}

// newPolicyCmd is the advanced governance group. The read-only view is
// "also audit --policy"; this group only signs and manages trust anchors.
func newPolicyCmd() *cobra.Command {
	c := newCmd("policy <keygen|trust|sign|verify>", "Manage signed policy governance")
	c.Long = `Signs also.policy.yml and manages the trust anchor outside it. The effective
policy is shown by "also audit --policy".`
	c.Hidden = true
	c.AddCommand(newPolicyKeygenCmd(), newPolicyTrustCmd(), newPolicySignCmd(), newPolicyVerifyCmd())
	return c
}

func printPolicySignature(p *also.Policy, status *also.PolicyVerification) {
	if p.Governance == nil {
		fmt.Println("governance: none (legacy unsigned policy)")
		return
	}
	fmt.Printf("governance: require_signature=%t approvals=%d roles=%v\n",
		p.Governance.RequireSignature, p.Governance.RequiredApprovals, p.Governance.RequiredRoles)
	for _, a := range p.Governance.Approvers {
		fmt.Printf("  approver: %s role=%s key=%s\n", a.Name, orDash(a.Role), short(a.Key))
	}
	if status == nil {
		fmt.Println("signature:  none")
		return
	}
	fmt.Printf("signature:  signed=%t valid=%t count=%d/%d roles=%v\n",
		status.Signed, status.Valid, status.Signatures, status.Required, status.Roles)
	if status.Error != "" {
		fmt.Printf("            %s\n", status.Error)
	}
}

func policyBool(v *bool) string {
	if v == nil {
		return "allowed (default)"
	}
	if *v {
		return "allowed"
	}
	return "denied"
}

type searchHit struct {
	Namespace   string           `json:"namespace"`
	Package     string           `json:"package"`
	Version     also.VersionInfo `json:"version"`
	Description string           `json:"description,omitempty"`
	Remote      string           `json:"remote"`
	Error       string           `json:"error,omitempty"`
}

// searchPackages is the "list <query>" mode. It searches every connected
// namespace unless a namespace or remote is given.
func searchPackages(setFlag, from string, refresh, jsonOut bool, query string) error {
	cfg, err := also.LoadConfig()
	if err != nil {
		return err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	var names []string
	if setFlag != "" || from != "" {
		name, sc, err := requireSet(nil, cfg, setFlag, from)
		if err != nil {
			return err
		}
		cfg.Namespaces[name] = sc
		names = []string{name}
	} else {
		names = cfg.NamespaceNames()
	}
	if len(names) == 0 {
		return fmt.Errorf("no namespace configured; add one with: also connect <name> <git-url>")
	}
	var hits []searchHit
	for _, name := range names {
		sc := cfg.Namespaces[name]
		repo, err := also.OpenSet(name, sc, refresh)
		if err != nil {
			hits = append(hits, searchHit{Namespace: name, Remote: sc.Remote, Error: err.Error()})
			continue
		}
		pkgs, err := repo.Packages()
		if err != nil {
			hits = append(hits, searchHit{Namespace: name, Remote: sc.Remote, Error: err.Error()})
			continue
		}
		for _, pkg := range pkgs {
			vs, err := repo.Versions(pkg)
			if err != nil || len(vs) == 0 {
				continue
			}
			meta, _ := repo.PackageMeta(pkg)
			latest := vs[0]
			haystack := strings.ToLower(strings.Join([]string{
				pkg, meta.Description, latest.Label, latest.Harness, strings.Join(latest.Semver, " "),
			}, "\n"))
			if query != "" && !strings.Contains(haystack, query) {
				continue
			}
			hits = append(hits, searchHit{
				Namespace: name, Package: pkg, Version: latest,
				Description: meta.Description, Remote: sc.Remote,
			})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Namespace != hits[j].Namespace {
			return hits[i].Namespace < hits[j].Namespace
		}
		return hits[i].Package < hits[j].Package
	})
	if jsonOut {
		return printJSON(map[string]any{"query": query, "results": hits})
	}
	var failed []string
	for _, h := range hits {
		if h.Error != "" {
			failed = append(failed, h.Namespace+": "+h.Error)
		}
	}
	printWarnings("unreadable namespaces", failed)
	if len(hits) == len(failed) && len(failed) > 0 {
		return fmt.Errorf("no namespace could be searched")
	}
	var found []searchHit
	for _, h := range hits {
		if h.Error == "" {
			found = append(found, h)
		}
	}
	if len(found) == 0 {
		fmt.Println("no matching packages")
		return nil
	}
	w := newTabWriter()
	fmt.Fprintf(w, "NAMESPACE\tPACKAGE\tVERSION\tSEMVER\tDESCRIPTION\n")
	for _, h := range found {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", h.Namespace, h.Package, short(h.Version.Version),
			orDash(strings.Join(h.Version.Semver, ", ")), orDash(h.Description))
	}
	return w.Flush()
}

func newTagCmd() *cobra.Command {
	var (
		setFlag string
		from    string
		refresh bool
		move    bool
	)
	c := newCmd("tag <package> <semver> [version]", "Point a semantic-version alias at a content version")
	c.Long = `Semantic versions are immutable aliases over content-addressed versions:
"also tag code-review 1.2.0" makes code-review@^1.2 resolvable while the
package version remains its digest. Use --move only when retargeting an
existing alias; the push is leased and refuses an unseen remote change.`
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("usage: also tag <package> <semver> [version] [--move]")
		}
		pkg, semver := args[0], args[1]
		if !also.ValidName(pkg) {
			return fmt.Errorf("invalid package name %q", pkg)
		}
		cfg, err := also.LoadConfig()
		if err != nil {
			return err
		}
		name, sc, err := requireSet(cmd, cfg, setFlag, from)
		if err != nil {
			return err
		}
		return withLock(func() error {
			repo, err := also.OpenSet(name, sc, refresh)
			if err != nil {
				return err
			}
			spec := ""
			if len(args) == 3 {
				spec = args[2]
			}
			v, err := repo.ResolveVersion(pkg, spec)
			if err != nil {
				return err
			}
			if err := repo.SetSemver(pkg, semver, v.Version, move); err != nil {
				return err
			}
			fmt.Printf("tagged %s %s -> %s@%s\n", pkg, semver, pkg, short(v.Version))
			return nil
		})
	}
	addNamespaceFlag(c, &setFlag)
	c.Flags().StringVar(&from, "from", "", "use a remote git URL without configuring a namespace")
	c.Flags().BoolVar(&refresh, "refresh", false, "fetch the remote before tagging")
	c.Flags().BoolVar(&move, "move", false, "move an existing alias; refuses if the remote changed since the last fetch")
	return c
}
