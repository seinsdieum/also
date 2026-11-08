package also

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SBOMComponent describes one installed also package for CycloneDX output.
type SBOMComponent struct {
	Name          string
	Namespace     string
	Version       string // content version (16 hex)
	Digest        string // full SHA-256 hex, may be empty
	Harness       string
	SourceHarness string
	Files         int
	Parts         []string
}

type cyclonedxDoc struct {
	BomFormat   string            `json:"bomFormat"`
	SpecVersion string            `json:"specVersion"`
	Version     int               `json:"version"`
	Metadata    cyclonedxMetadata `json:"metadata"`
	Components  []cyclonedxComp   `json:"components"`
}

type cyclonedxMetadata struct {
	Timestamp string          `json:"timestamp"`
	Tools     []cyclonedxTool `json:"tools"`
}

type cyclonedxTool struct {
	Vendor  string `json:"vendor"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type cyclonedxComp struct {
	Type       string          `json:"type"`
	BomRef     string          `json:"bom-ref"`
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Hashes     []cyclonedxHash `json:"hashes,omitempty"`
	Properties []cyclonedxProp `json:"properties,omitempty"`
}

type cyclonedxHash struct {
	Alg     string `json:"alg"`
	Content string `json:"content"`
}

type cyclonedxProp struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// WriteCycloneDX writes a deterministic CycloneDX 1.5 JSON document.
func WriteCycloneDX(w io.Writer, components []SBOMComponent) error {
	if w == nil {
		return errors.New("also: nil writer")
	}

	sorted := make([]SBOMComponent, len(components))
	copy(sorted, components)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Harness != b.Harness {
			return a.Harness < b.Harness
		}
		return a.Version < b.Version
	})

	refs := make(map[string]int, len(sorted))
	out := make([]cyclonedxComp, 0, len(sorted))
	for _, c := range sorted {
		ref := "pkg:generic/also/" + c.Namespace + "/" + c.Name + "@" + c.Version
		refs[ref]++
		if n := refs[ref]; n > 1 {
			ref += fmt.Sprintf("#%d", n)
		}

		comp := cyclonedxComp{
			Type:    "application",
			BomRef:  ref,
			Name:    c.Name,
			Version: c.Version,
		}
		if d, ok := normalizeDigest(c.Digest); ok {
			comp.Hashes = []cyclonedxHash{{Alg: "SHA-256", Content: d}}
		}

		if c.Namespace != "" {
			comp.Properties = append(comp.Properties, cyclonedxProp{"also:namespace", c.Namespace})
		}
		if c.Harness != "" {
			comp.Properties = append(comp.Properties, cyclonedxProp{"also:harness", c.Harness})
		}
		if c.SourceHarness != "" {
			comp.Properties = append(comp.Properties, cyclonedxProp{"also:source_harness", c.SourceHarness})
		}
		comp.Properties = append(comp.Properties, cyclonedxProp{"also:files", strconv.Itoa(c.Files)})
		if len(c.Parts) > 0 {
			comp.Properties = append(comp.Properties, cyclonedxProp{"also:parts", strings.Join(c.Parts, ", ")})
		}
		out = append(out, comp)
	}

	doc := cyclonedxDoc{
		BomFormat:   "CycloneDX",
		SpecVersion: "1.5",
		Version:     1,
		Metadata: cyclonedxMetadata{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Tools:     []cyclonedxTool{{Vendor: "also", Name: "also", Version: Version}},
		},
		Components: out,
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func normalizeDigest(s string) (string, bool) {
	if len(s) != 64 {
		return "", false
	}
	if _, err := hex.DecodeString(s); err != nil {
		return "", false
	}
	return strings.ToLower(s), true
}
