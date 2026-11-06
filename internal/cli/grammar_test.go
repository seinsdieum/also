package cli

import (
	"strings"
	"testing"

	"also/internal/also"
)

func testConfig() *also.Config {
	return &also.Config{
		Schema: also.SchemaConfig,
		Sets: map[string]also.SetConfig{
			"personal": {Remote: "git@example.com:me/infra.git"},
			"infra":    {Remote: "git@example.com:me/other.git"},
		},
	}
}

func TestParsePkgRefNamespaceAndVersion(t *testing.T) {
	cfg := testConfig()
	ref, err := parsePkgRef("personal@infra", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Namespace != "personal" || ref.Package != "infra" || ref.Version != "" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	ref, err = parsePkgRef("personal@infra@v1", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Namespace != "personal" || ref.Package != "infra" || ref.Version != "v1" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	ref, err = parsePkgRef("infra@v1", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Namespace != "" || ref.Package != "infra" || ref.Version != "v1" {
		t.Fatalf("unexpected ref: %+v", ref)
	}
	ref, err = parsePkgRef("infra@main", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Namespace != "infra" || ref.Package != "main" || ref.Version != "" {
		t.Fatalf("unexpected namespace ref: %+v", ref)
	}
}

func TestParseInstallNaturalForms(t *testing.T) {
	cfg := testConfig()
	cases := []struct {
		args    []string
		pkg     string
		ns      string
		harness string
		parts   []string
	}{
		{[]string{"awesome-design", "for", "opencode"}, "awesome-design", "", "opencode", nil},
		{[]string{"review", "skill", "from", "personal@infra", "for", "opencode"}, "infra", "personal", "opencode", []string{"skills:review"}},
		{[]string{"code-review", "from", "personal"}, "code-review", "personal", "", nil},
		{[]string{"skill", "from", "infra"}, "infra", "", "", []string{"skills"}},
		{[]string{"hook", "guard", "from", "infra"}, "infra", "", "", []string{"hooks:guard"}},
		{[]string{"mcp", "github", "from", "infra"}, "infra", "", "", []string{"mcp:github"}},
	}
	for _, tc := range cases {
		got, err := parseInstallArgs(tc.args, cfg)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got.Ref.Package != tc.pkg || got.Ref.Namespace != tc.ns || got.Harness != tc.harness {
			t.Fatalf("%v: unexpected spec %+v", tc.args, got)
		}
		if strings.Join(got.Parts, ",") != strings.Join(tc.parts, ",") {
			t.Fatalf("%v: parts %v, want %v", tc.args, got.Parts, tc.parts)
		}
	}
}

func TestInstallAsksForMissingHarness(t *testing.T) {
	_, err := parseInstallArgs([]string{"awesome-design", "for"}, testConfig())
	if err == nil || !strings.Contains(err.Error(), "Please clarify") || !strings.Contains(err.Error(), "claude") {
		t.Fatalf("expected a natural clarification, got: %v", err)
	}
}

func TestParseIgnoreNaturalForms(t *testing.T) {
	spec, err := parseIgnoreArgs([]string{"infra", "for", "claude,codex", "in", "/tmp/project"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != also.IgnoreInfra || spec.Dir != "/tmp/project" || strings.Join(spec.Harnesses, ",") != "claude,codex" {
		t.Fatalf("unexpected ignore spec: %+v", spec)
	}
	if _, err := parseIgnoreArgs([]string{"you", "for", "claude"}); err == nil {
		t.Fatal("'you' must not accept a harness")
	}
}
