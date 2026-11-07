package also

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidName reports whether a namespace or package name is safe to use as a path segment.
func ValidName(name string) bool { return validName.MatchString(name) }

// SchemaConfigV1 is accepted only so old configs can be migrated in place.
const SchemaConfigV1 = "also.config/v1"

// NamespaceConfig is a remote git repository that stores also packages. The
// local clone is an internal cache, never part of the user-facing model.
type NamespaceConfig struct {
	Remote string `json:"remote"`
}

type Config struct {
	Schema           string                     `json:"schema"`
	DefaultNamespace string                     `json:"default_namespace,omitempty"`
	DefaultHarness   string                     `json:"default_harness,omitempty"`
	Namespaces       map[string]NamespaceConfig `json:"namespaces"`
}

// configFile also accepts the v1 layout so the switch to remote-only
// namespaces does not require the user to edit config by hand.
type configFile struct {
	Schema           string                     `json:"schema"`
	DefaultSet       string                     `json:"default_set,omitempty"`
	DefaultNamespace string                     `json:"default_namespace,omitempty"`
	DefaultHarness   string                     `json:"default_harness,omitempty"`
	Sets             map[string]NamespaceConfig `json:"sets"`
	Namespaces       map[string]NamespaceConfig `json:"namespaces"`
}

func defaultConfig() *Config {
	return &Config{Schema: SchemaConfig, Namespaces: map[string]NamespaceConfig{}}
}

func LoadConfig() (*Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var raw configFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigPath(), err)
	}
	if raw.Schema != SchemaConfig && raw.Schema != SchemaConfigV1 {
		return nil, fmt.Errorf("unsupported config schema %q in %s", raw.Schema, ConfigPath())
	}
	cfg.DefaultNamespace = raw.DefaultNamespace
	if cfg.DefaultNamespace == "" {
		cfg.DefaultNamespace = raw.DefaultSet
	}
	cfg.DefaultHarness = raw.DefaultHarness
	if raw.Namespaces != nil {
		cfg.Namespaces = raw.Namespaces
	} else if raw.Sets != nil {
		cfg.Namespaces = raw.Sets
	}
	if cfg.Namespaces == nil {
		cfg.Namespaces = map[string]NamespaceConfig{}
	}
	if raw.Schema == SchemaConfigV1 {
		if err := SaveConfig(cfg); err != nil {
			return nil, fmt.Errorf("migrate config to %s: %w", SchemaConfig, err)
		}
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	cfg.Schema = SchemaConfig
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(ConfigPath(), data, 0o600)
}

func (c *Config) NamespaceNames() []string {
	names := make([]string, 0, len(c.Namespaces))
	for name := range c.Namespaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) ResolveSet(name string) (string, NamespaceConfig, error) {
	if name == "" {
		name = c.DefaultNamespace
	}
	if name == "" {
		if len(c.Namespaces) == 1 {
			for n, s := range c.Namespaces {
				return n, s, nil
			}
		}
		if len(c.Namespaces) == 0 {
			return "", NamespaceConfig{}, fmt.Errorf("no namespace configured; add one with: also connect <name> <git-url>")
		}
		return "", NamespaceConfig{}, fmt.Errorf("no active namespace; run: also use <namespace> (configured: %v)", c.NamespaceNames())
	}
	s, ok := c.Namespaces[name]
	if !ok {
		return "", NamespaceConfig{}, fmt.Errorf("unknown namespace %q; configured: %v", name, c.NamespaceNames())
	}
	return name, s, nil
}

func (c *Config) Harness(requested string) (string, error) {
	h := requested
	if h == "" {
		h = c.DefaultHarness
	}
	if h == "" {
		return "", fmt.Errorf("no harness selected; pass -h <harness> or run: also config set default_harness <harness>\navailable: %v", HarnessIDs())
	}
	hh, ok := ResolveHarness(h)
	if !ok {
		return "", fmt.Errorf("unknown harness %q; available: %v", h, HarnessIDs())
	}
	return hh.ID, nil
}

// Scope keeps the internal install model project-local. The user-facing
// project/global split was removed, so a global request is rejected instead
// of silently writing to the home directory.
func (c *Config) Scope(requested string) (string, error) {
	if requested != "" && requested != "project" {
		return "", fmt.Errorf("scope %q is no longer supported; also installs into the current project", requested)
	}
	return "project", nil
}
