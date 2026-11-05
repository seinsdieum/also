package also

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
)

var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidName reports whether a set or package name is safe to use as a path segment.
func ValidName(name string) bool { return validName.MatchString(name) }

type SetConfig struct {
	Remote string `json:"remote"`
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
}

type Config struct {
	Schema         string               `json:"schema"`
	DefaultSet     string               `json:"default_set,omitempty"`
	DefaultHarness string               `json:"default_harness,omitempty"`
	DefaultScope   string               `json:"default_scope,omitempty"`
	Sets           map[string]SetConfig `json:"sets"`
}

func defaultConfig() *Config {
	return &Config{Schema: SchemaConfig, DefaultScope: "project", Sets: map[string]SetConfig{}}
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
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", ConfigPath(), err)
	}
	if cfg.Schema != SchemaConfig {
		return nil, fmt.Errorf("unsupported config schema %q in %s", cfg.Schema, ConfigPath())
	}
	if cfg.Sets == nil {
		cfg.Sets = map[string]SetConfig{}
	}
	if cfg.DefaultScope == "" {
		cfg.DefaultScope = "project"
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(ConfigPath(), data, 0o600)
}

func (c *Config) SetNames() []string {
	names := make([]string, 0, len(c.Sets))
	for name := range c.Sets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Config) ResolveSet(name string) (string, SetConfig, error) {
	if name == "" {
		name = c.DefaultSet
	}
	if name == "" {
		if len(c.Sets) == 1 {
			for n, s := range c.Sets {
				return n, s, nil
			}
		}
		if len(c.Sets) == 0 {
			return "", SetConfig{}, fmt.Errorf("no set configured; run: also set <name> --remote <git-url>")
		}
		return "", SetConfig{}, fmt.Errorf("default set is not set; pass --set or run: also config set default_set <name>")
	}
	s, ok := c.Sets[name]
	if !ok {
		return "", SetConfig{}, fmt.Errorf("unknown set %q; configured: %v", name, c.SetNames())
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

func (c *Config) Scope(requested string) (string, error) {
	s := requested
	if s == "" {
		s = c.DefaultScope
	}
	if s == "" {
		s = "project"
	}
	if s != "global" && s != "project" {
		return "", fmt.Errorf("unknown scope %q; use global or project", s)
	}
	return s, nil
}
