package also

import (
	"bytes"
	"strings"
	"testing"
)

// FuzzParseJSONC checks that a config edit keeps the file parseable and does
// not lose the value it wrote.
func FuzzParseJSONC(f *testing.F) {
	for _, s := range []string{
		`{}`, `{"a":1}`, "// comment\n{\"a\":[1,2,3]}", `{"a":1,}`,
		`{"mcp":{"servers":{"x":{"command":"a"}}}}`, `{"a":"// not a comment"}`,
		"not json", "", `[1,2,3]`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := ParseJSONC(data)
		if err != nil {
			return
		}
		out, err := doc.Set(map[string]any{"n": 1}, "also_fuzz")
		if err != nil {
			return
		}
		doc2, err := ParseJSONC(out)
		if err != nil {
			t.Fatalf("Set produced unparsable JSON: %v\n%s", err, out)
		}
		raw, ok := doc2.RawAt("also_fuzz", "n")
		if !ok || !bytes.Equal(bytes.TrimSpace(raw), []byte("1")) {
			t.Fatalf("Set lost its value: %s", out)
		}
		if _, _, err := doc2.Remove("also_fuzz"); err != nil {
			t.Fatalf("Remove failed: %v", err)
		}
	})
}

// FuzzTOMLBlocks checks the managed block append/read/remove round trip.
func FuzzTOMLBlocks(f *testing.F) {
	for _, s := range []string{
		"", "[mcp_servers.x]\ncommand = \"a\"\n", "# a comment\n",
		"# also:begin other\nfoo = 1\n# also:end other\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		const marker = "also_fuzz_marker"
		if bytes.Contains(data, []byte(marker)) {
			t.Skip()
		}
		out := tomlAppendBlock(data, marker, "command = \"also\"\n")
		body, ok := tomlBlockBody(out, marker)
		if !ok || !strings.Contains(body, "also") {
			t.Fatalf("appended block is unreadable: %q", out)
		}
		removed, found := tomlRemoveBlock(out, marker)
		if !found {
			t.Fatal("removed block was not found")
		}
		if bytes.Contains(removed, []byte(markerBegin(marker))) {
			t.Fatal("marker survived removal")
		}
	})
}

// FuzzCommandConversion checks both directions of the placeholder translator.
func FuzzCommandConversion(f *testing.F) {
	for _, s := range []string{
		"", "Hello $ARGUMENTS", "!`echo hi` $1", "---\nname: x\n---\nbody",
		"!{pwd} and $ARGUMENTS and $1",
	} {
		f.Add([]byte(s), "claude", "qwen")
		f.Add([]byte(s), "qwen", "claude")
	}
	f.Fuzz(func(t *testing.T, data []byte, from, to string) {
		out, _ := ConvertCommand(data, from, to, "fuzz")
		_, _ = ConvertCommand(out, to, from, "fuzz")
	})
}

// FuzzAgentFrontmatter checks that rendered frontmatter parses back with the
// same body.
func FuzzAgentFrontmatter(f *testing.F) {
	for _, s := range []string{
		"---\nname: x\ntools: a, b\n---\nbody\n",
		"---\nname: x\n---\n", "no frontmatter", "---\n:bad\n---\nbody",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fm, body, ok, err := splitFrontmatter(data)
		if err != nil || !ok {
			return
		}
		out, err := renderFrontmatter(fm, body)
		if err != nil {
			return
		}
		_, body2, ok2, err2 := splitFrontmatter(out)
		if err2 != nil || !ok2 {
			t.Fatalf("rendered frontmatter is unparsable: %s", out)
		}
		if !bytes.Equal(body, body2) {
			t.Fatalf("body changed during round trip: %q -> %q", body, body2)
		}
	})
}
