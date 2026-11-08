package also

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	PolicyFile   = "also.policy.yml"
	SchemaPolicy = "also.policy/v1"
)

// Policy is a project-level install guard read from also.policy.yml.
type Policy struct {
	Schema      string            `yaml:"schema" json:"schema"`
	Namespaces  PolicyNamespaces  `yaml:"namespaces" json:"namespaces"`
	Packages    PolicyPackages    `yaml:"packages" json:"packages"`
	Executables PolicyExecutables `yaml:"executables" json:"executables"`
	Limits      PolicyLimits      `yaml:"limits" json:"limits"`
	Governance  *PolicyGovernance `yaml:"governance,omitempty" json:"governance,omitempty"`
}

type PolicyGovernance struct {
	RequireSignature  bool             `yaml:"require_signature" json:"require_signature"`
	RequiredApprovals int              `yaml:"required_approvals" json:"required_approvals"`
	RequiredRoles     []string         `yaml:"required_roles,omitempty" json:"required_roles,omitempty"`
	Approvers         []PolicyApprover `yaml:"approvers" json:"approvers"`
}

type PolicyApprover struct {
	Name string `yaml:"name" json:"name"`
	Role string `yaml:"role" json:"role"`
	Key  string `yaml:"key" json:"key"` // base64 Ed25519 public key
}

type PolicyNamespaces struct {
	Allow []string `yaml:"allow" json:"allow,omitempty"`
	Deny  []string `yaml:"deny" json:"deny,omitempty"`
}

type PolicyPackages struct {
	Allow []string `yaml:"allow" json:"allow,omitempty"`
	Deny  []string `yaml:"deny" json:"deny,omitempty"`
}

type PolicyExecutables struct {
	AllowHooks    *bool `yaml:"allow_hooks" json:"allow_hooks,omitempty"`
	AllowMCP      *bool `yaml:"allow_mcp" json:"allow_mcp,omitempty"`
	AllowPlugins  *bool `yaml:"allow_plugins" json:"allow_plugins,omitempty"`
	AllowCommands *bool `yaml:"allow_commands" json:"allow_commands,omitempty"`
}

type PolicyLimits struct {
	MaxPackageBytes     int64 `yaml:"max_package_bytes" json:"max_package_bytes"`
	AllowLiteralSecrets bool  `yaml:"allow_literal_secrets" json:"allow_literal_secrets"`
}

// LoadPolicy reads project/also.policy.yml. A missing file is not an error.
func LoadPolicy(project string) (*Policy, error) {
	files := PolicyFiles(project)
	if len(files) == 0 {
		return nil, nil
	}
	var merged *Policy
	for _, file := range files {
		p, err := LoadPolicyFile(file)
		if err != nil {
			return nil, err
		}
		merged = mergePolicy(merged, p)
	}
	return merged, nil
}

// PolicyFiles returns every also.policy.yml from the project up to the git
// root, nearest first. Every file is enforced: a nested policy can add
// restrictions but cannot disable a root deny, executable gate or signature
// requirement.
func PolicyFiles(project string) []string {
	abs, err := AbsPath(project)
	if err != nil {
		return nil
	}
	var out []string
	for cur := abs; ; cur = filepath.Dir(cur) {
		path := filepath.Join(cur, PolicyFile)
		if _, statErr := os.Stat(path); statErr == nil {
			out = append(out, path)
		}
		if _, statErr := os.Stat(filepath.Join(cur, ".git")); statErr == nil && isGitRoot(cur) {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
	}
	return out
}

// isGitRoot confirms that dir is a real repository root, not merely a
// directory that happens to contain an empty .git entry.
func isGitRoot(dir string) bool {
	out, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	abs, err := AbsPath(dir)
	if err != nil {
		return false
	}
	return PathKey(strings.TrimSpace(out)) == PathKey(abs)
}

// PolicyPath is the nearest policy file, used for display and signing.
func PolicyPath(project string) string {
	if files := PolicyFiles(project); len(files) > 0 {
		return files[0]
	}
	abs, err := AbsPath(project)
	if err != nil {
		return filepath.Join(project, PolicyFile)
	}
	return filepath.Join(abs, PolicyFile)
}

func mergePolicy(base, next *Policy) *Policy {
	if base == nil {
		return next
	}
	if next == nil {
		return base
	}
	out := *base
	out.Namespaces = PolicyNamespaces{
		Allow: intersectStrings(base.Namespaces.Allow, next.Namespaces.Allow),
		Deny:  UniqueStrings(append(append([]string{}, base.Namespaces.Deny...), next.Namespaces.Deny...)),
	}
	out.Packages = PolicyPackages{
		Allow: intersectStrings(base.Packages.Allow, next.Packages.Allow),
		Deny:  UniqueStrings(append(append([]string{}, base.Packages.Deny...), next.Packages.Deny...)),
	}
	out.Executables = PolicyExecutables{
		AllowHooks:    andBoolPtr(base.Executables.AllowHooks, next.Executables.AllowHooks),
		AllowMCP:      andBoolPtr(base.Executables.AllowMCP, next.Executables.AllowMCP),
		AllowPlugins:  andBoolPtr(base.Executables.AllowPlugins, next.Executables.AllowPlugins),
		AllowCommands: andBoolPtr(base.Executables.AllowCommands, next.Executables.AllowCommands),
	}
	out.Limits = PolicyLimits{
		MaxPackageBytes:     minPositive(base.Limits.MaxPackageBytes, next.Limits.MaxPackageBytes),
		AllowLiteralSecrets: base.Limits.AllowLiteralSecrets && next.Limits.AllowLiteralSecrets,
	}
	out.Governance = mergeGovernance(base.Governance, next.Governance)
	return &out
}

func intersectStrings(a, b []string) []string {
	if len(a) == 0 {
		return append([]string{}, b...)
	}
	if len(b) == 0 {
		return append([]string{}, a...)
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	var out []string
	for _, v := range b {
		if set[v] {
			out = append(out, v)
		}
	}
	return UniqueStrings(out)
}

func andBoolPtr(a, b *bool) *bool {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	v := *a && *b
	return &v
}

func minPositive(a, b int64) int64 {
	if a <= 0 {
		return b
	}
	if b <= 0 || a < b {
		return a
	}
	return b
}

func mergeGovernance(a, b *PolicyGovernance) *PolicyGovernance {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	out := *a
	out.RequireSignature = a.RequireSignature || b.RequireSignature
	if b.RequiredApprovals > out.RequiredApprovals {
		out.RequiredApprovals = b.RequiredApprovals
	}
	out.RequiredRoles = UniqueStrings(append(append([]string{}, a.RequiredRoles...), b.RequiredRoles...))
	out.Approvers = append(append([]PolicyApprover{}, a.Approvers...), b.Approvers...)
	return &out
}

// LoadPolicyFile parses a policy file. A missing file is not an error; any
// other read or parse failure is.
func LoadPolicyFile(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read policy %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy %s: %w", path, err)
	}
	if p.Schema != "" && p.Schema != SchemaPolicy {
		return nil, fmt.Errorf("policy %s: unsupported schema %q (want %s)", path, p.Schema, SchemaPolicy)
	}
	if p.Limits.MaxPackageBytes <= 0 {
		p.Limits.MaxPackageBytes = maxPackageBytes
	}
	return &p, nil
}

// NamespaceAllowed reports whether name passes the namespace allow/deny lists.
func (p *Policy) NamespaceAllowed(name string) bool {
	if p == nil {
		return true
	}
	for _, deny := range p.Namespaces.Deny {
		if deny == name {
			return false
		}
	}
	if len(p.Namespaces.Allow) == 0 {
		return true
	}
	for _, allow := range p.Namespaces.Allow {
		if allow == name {
			return true
		}
	}
	return false
}

// PackageAllowed reports whether name passes the package allow/deny lists.
// Entries may be path.Match globs; deny wins.
func (p *Policy) PackageAllowed(name string) bool {
	if p == nil {
		return true
	}
	if matchAny(p.Packages.Deny, name) {
		return false
	}
	if len(p.Packages.Allow) == 0 {
		return true
	}
	return matchAny(p.Packages.Allow, name)
}

// ExecutablesAllowed reports whether every kind is permitted. An omitted
// executable switch defaults to allowed.
func (p *Policy) ExecutablesAllowed(kinds []string) bool {
	if p == nil {
		return true
	}
	for _, kind := range kinds {
		var allowed *bool
		switch kind {
		case "hook":
			allowed = p.Executables.AllowHooks
		case "mcp":
			allowed = p.Executables.AllowMCP
		case "plugin":
			allowed = p.Executables.AllowPlugins
		case "command":
			allowed = p.Executables.AllowCommands
		default:
			return false
		}
		if allowed != nil && !*allowed {
			return false
		}
	}
	return true
}

// CheckPackageFiles returns policy findings for a package's files and MCP servers.
func (p *Policy) CheckPackageFiles(files map[string]*SnapshotFile, servers map[string]MCPServer) []string {
	if p == nil {
		return nil
	}
	var out []string
	var total int64
	for _, f := range files {
		if f != nil {
			total += int64(len(f.Data))
		}
	}
	limit := p.Limits.MaxPackageBytes
	if limit <= 0 {
		limit = maxPackageBytes
	}
	if total > limit {
		out = append(out, fmt.Sprintf("package exceeds policy limit (%s)", humanBytes(limit)))
	}
	if !p.Limits.AllowLiteralSecrets {
		for _, s := range MCPSuspiciousSecrets(servers) {
			out = append(out, "literal secret: "+s)
		}
		for _, s := range ScanConfigSecrets(files) {
			out = append(out, "literal secret: "+s)
		}
	}
	return UniqueStrings(out)
}

func matchAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if pattern == name {
			return true
		}
		if ok, err := path.Match(pattern, name); err == nil && ok {
			return true
		}
	}
	return false
}
