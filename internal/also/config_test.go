package also

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMigratesV1ToRemoteOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ALSO_HOME", filepath.Join(tmp, "also"))
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{
  "schema": "also.config/v1",
  "default_set": "personal",
  "default_harness": "opencode",
  "default_scope": "global",
  "sets": {
    "personal": {
      "remote": "git@example.com:me/infra.git",
      "path": "/tmp/old-clone",
      "source": "/tmp/old-source"
    }
  }
}`
	if err := os.WriteFile(ConfigPath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Schema != SchemaConfig {
		t.Fatalf("schema = %q, want %q", cfg.Schema, SchemaConfig)
	}
	if cfg.DefaultNamespace != "personal" || cfg.DefaultHarness != "opencode" {
		t.Fatalf("defaults were not preserved: %+v", cfg)
	}
	ns, ok := cfg.Namespaces["personal"]
	if !ok || ns.Remote != "git@example.com:me/infra.git" {
		t.Fatalf("namespace was not migrated: %+v", cfg.Namespaces)
	}
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, `"also.config/v2"`) || !strings.Contains(got, `"namespaces"`) {
		t.Fatalf("config was not rewritten as v2:\n%s", got)
	}
	if strings.Contains(got, `"path"`) || strings.Contains(got, `"source"`) || strings.Contains(got, `"sets"`) {
		t.Fatalf("v1 fields survived migration:\n%s", got)
	}
}

func TestConfigRoundTripWithoutClones(t *testing.T) {
	t.Setenv("ALSO_HOME", filepath.Join(t.TempDir(), "also"))
	cfg := defaultConfig()
	cfg.DefaultNamespace = "work"
	cfg.Namespaces["work"] = NamespaceConfig{Remote: "https://example.com/infra.git"}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultNamespace != "work" || loaded.Namespaces["work"].Remote != cfg.Namespaces["work"].Remote {
		t.Fatalf("config did not round-trip: %+v", loaded)
	}
	if strings.Contains(string(readConfigBytesForTest(t, ConfigPath())), "clones") {
		t.Fatal("unexpected clone path in config")
	}
}

func readConfigBytesForTest(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCorruptConfigAndStateAreRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ALSO_HOME", filepath.Join(tmp, "also"))
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(); err == nil {
		t.Fatal("corrupt config was accepted")
	}
	if err := os.Remove(ConfigPath()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(StatePath(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(); err == nil {
		t.Fatal("corrupt state was accepted")
	}
}
