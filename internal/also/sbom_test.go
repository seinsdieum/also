package also

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

type sbomTestDoc struct {
	BomFormat   string `json:"bomFormat"`
	SpecVersion string `json:"specVersion"`
	Version     int    `json:"version"`
	Metadata    struct {
		Timestamp string `json:"timestamp"`
		Tools     []struct {
			Vendor  string `json:"vendor"`
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"tools"`
	} `json:"metadata"`
	Components []struct {
		Type    string `json:"type"`
		BomRef  string `json:"bom-ref"`
		Name    string `json:"name"`
		Version string `json:"version"`
		Hashes  []struct {
			Alg     string `json:"alg"`
			Content string `json:"content"`
		} `json:"hashes"`
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	} `json:"components"`
}

func writeTestSBOM(t *testing.T, components []SBOMComponent) (string, sbomTestDoc) {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteCycloneDX(&buf, components); err != nil {
		t.Fatalf("WriteCycloneDX: %v", err)
	}
	var doc sbomTestDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	return buf.String(), doc
}

func sbomProp(doc sbomTestDoc, comp int, name string) string {
	for _, p := range doc.Components[comp].Properties {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

const (
	sbomDigestLower = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sbomVersionA    = "0123456789abcdef"
	sbomVersionB    = "fedcba9876543210"
)

func TestSBOMDocumentShape(t *testing.T) {
	components := []SBOMComponent{
		{
			Name: "beta", Namespace: "ns", Version: sbomVersionA,
			Digest: strings.ToUpper(sbomDigestLower), Harness: "claude",
			SourceHarness: "codex", Files: 3, Parts: []string{"skill:review", "command"},
		},
		{
			Name: "alpha", Namespace: "ns", Version: sbomVersionB,
			Harness: "opencode", Files: 1,
		},
	}
	raw, doc := writeTestSBOM(t, components)

	if doc.BomFormat != "CycloneDX" {
		t.Errorf("bomFormat = %q", doc.BomFormat)
	}
	if doc.SpecVersion != "1.5" {
		t.Errorf("specVersion = %q", doc.SpecVersion)
	}
	if doc.Version != 1 {
		t.Errorf("version = %d", doc.Version)
	}
	if _, err := time.Parse(time.RFC3339, doc.Metadata.Timestamp); err != nil {
		t.Errorf("timestamp %q is not RFC3339: %v", doc.Metadata.Timestamp, err)
	}
	if len(doc.Metadata.Tools) != 1 || doc.Metadata.Tools[0].Vendor != "also" ||
		doc.Metadata.Tools[0].Name != "also" || doc.Metadata.Tools[0].Version != Version {
		t.Errorf("tools = %+v", doc.Metadata.Tools)
	}
	if !strings.HasSuffix(raw, "\n") {
		t.Error("output does not end with a newline")
	}
	if !strings.Contains(raw, "\n  \"bomFormat\"") {
		t.Error("output is not indented with two spaces")
	}

	if len(doc.Components) != 2 {
		t.Fatalf("components = %d, want 2", len(doc.Components))
	}
	// Sorted by namespace, name, harness, version: alpha first.
	if doc.Components[0].Name != "alpha" || doc.Components[1].Name != "beta" {
		t.Errorf("component order = %q, %q", doc.Components[0].Name, doc.Components[1].Name)
	}
	for _, c := range doc.Components {
		if c.Type != "application" {
			t.Errorf("%s type = %q", c.Name, c.Type)
		}
	}
	if got, want := doc.Components[0].BomRef, "pkg:generic/also/ns/alpha@"+sbomVersionB; got != want {
		t.Errorf("alpha bom-ref = %q, want %q", got, want)
	}
	if got, want := doc.Components[1].BomRef, "pkg:generic/also/ns/beta@"+sbomVersionA; got != want {
		t.Errorf("beta bom-ref = %q, want %q", got, want)
	}

	alpha, beta := doc.Components[0], doc.Components[1]
	if len(alpha.Hashes) != 0 {
		t.Errorf("alpha hashes = %+v, want none for empty digest", alpha.Hashes)
	}
	if got := sbomProp(doc, 0, "also:source_harness"); got != "" {
		t.Errorf("alpha source_harness = %q, want omitted", got)
	}
	if got := sbomProp(doc, 0, "also:parts"); got != "" {
		t.Errorf("alpha parts = %q, want omitted", got)
	}
	if got := sbomProp(doc, 0, "also:files"); got != "1" {
		t.Errorf("alpha files = %q", got)
	}

	if len(beta.Hashes) != 1 {
		t.Fatalf("beta hashes = %+v, want 1", beta.Hashes)
	}
	if beta.Hashes[0].Alg != "SHA-256" {
		t.Errorf("beta hash alg = %q", beta.Hashes[0].Alg)
	}
	if beta.Hashes[0].Content != sbomDigestLower {
		t.Errorf("beta hash = %q, want lowercase %q", beta.Hashes[0].Content, sbomDigestLower)
	}
	wantProps := map[string]string{
		"also:namespace":      "ns",
		"also:harness":        "claude",
		"also:source_harness": "codex",
		"also:files":          "3",
		"also:parts":          "skill:review, command",
	}
	if len(beta.Properties) != len(wantProps) {
		t.Errorf("beta properties = %+v", beta.Properties)
	}
	for name, want := range wantProps {
		if got := sbomProp(doc, 1, name); got != want {
			t.Errorf("beta %s = %q, want %q", name, got, want)
		}
	}
}

func TestSBOMDeterministicOrdering(t *testing.T) {
	components := []SBOMComponent{
		{Name: "c", Namespace: "z", Version: sbomVersionA, Harness: "b"},
		{Name: "a", Namespace: "y", Version: sbomVersionB, Harness: "a"},
		{Name: "a", Namespace: "y", Version: sbomVersionB, Harness: "c"},
		{Name: "a", Namespace: "y", Version: sbomVersionA, Harness: "a"},
		{Name: "b", Namespace: "y", Version: sbomVersionA},
	}

	_, first := writeTestSBOM(t, components)
	_, second := writeTestSBOM(t, components)

	var firstRefs, secondRefs []string
	for _, c := range first.Components {
		firstRefs = append(firstRefs, c.BomRef)
	}
	for _, c := range second.Components {
		secondRefs = append(secondRefs, c.BomRef)
	}
	if !reflect.DeepEqual(firstRefs, secondRefs) {
		t.Fatalf("order changed between runs:\n%v\n%v", firstRefs, secondRefs)
	}
	gotKeys := make([]string, 0, len(first.Components))
	for _, c := range first.Components {
		gotKeys = append(gotKeys, c.Name+"@"+c.Version)
	}
	wantKeys := []string{
		"a@" + sbomVersionA,
		"a@" + sbomVersionB,
		"a@" + sbomVersionB,
		"b@" + sbomVersionA,
		"c@" + sbomVersionA,
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("sorted keys = %v, want %v", gotKeys, wantKeys)
	}
}

func TestSBOMUniqueRefs(t *testing.T) {
	component := SBOMComponent{Name: "dup", Namespace: "ns", Version: sbomVersionA, Harness: "claude"}
	_, doc := writeTestSBOM(t, []SBOMComponent{component, component, component})

	if len(doc.Components) != 3 {
		t.Fatalf("components = %d", len(doc.Components))
	}
	base := "pkg:generic/also/ns/dup@" + sbomVersionA
	want := []string{base, base + "#2", base + "#3"}
	for i, c := range doc.Components {
		if c.BomRef != want[i] {
			t.Errorf("ref[%d] = %q, want %q", i, c.BomRef, want[i])
		}
	}
}

func TestSBOMDigestValidation(t *testing.T) {
	tests := []struct {
		name   string
		digest string
		want   bool
	}{
		{"empty", "", false},
		{"short", sbomDigestLower[:63], false},
		{"long", sbomDigestLower + "0", false},
		{"non-hex", strings.Repeat("g", 64), false},
		{"spaces", strings.Repeat(" ", 64), false},
		{"lowercase", sbomDigestLower, true},
		{"uppercase", strings.ToUpper(sbomDigestLower), true},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, doc := writeTestSBOM(t, []SBOMComponent{{
				Name: "pkg" + string(rune('a'+i)), Namespace: "ns", Version: sbomVersionA, Digest: tt.digest,
			}})
			hashes := doc.Components[0].Hashes
			if tt.want {
				if len(hashes) != 1 || hashes[0].Content != sbomDigestLower {
					t.Fatalf("hashes = %+v, want normalized %q", hashes, sbomDigestLower)
				}
			} else if len(hashes) != 0 {
				t.Fatalf("hashes = %+v, want omitted", hashes)
			}
		})
	}
}

func TestSBOMEmptyComponents(t *testing.T) {
	raw, doc := writeTestSBOM(t, nil)
	if doc.Components == nil || len(doc.Components) != 0 {
		t.Fatalf("components = %v, want empty array", doc.Components)
	}
	if !strings.Contains(raw, `"components": []`) {
		t.Errorf("components not encoded as [], output:\n%s", raw)
	}
}

func TestWriteCycloneDXNilWriter(t *testing.T) {
	if err := WriteCycloneDX(nil, nil); err == nil {
		t.Fatal("WriteCycloneDX(nil, nil) returned nil error")
	}
}
