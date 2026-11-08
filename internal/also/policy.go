package also

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

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
	return LoadPolicyFile(filepath.Join(project, PolicyFile))
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
