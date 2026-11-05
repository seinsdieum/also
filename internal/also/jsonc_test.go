package also

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONCSetPreservesComments(t *testing.T) {
	src := []byte(`{
  // top comment
  "mcp": {
    "servers": {
      // keep me
      "old": {"type": "local", "command": ["a"]}
    },
    "timeout": {"startup": 1000}
  }
}`)
	doc, err := ParseJSONC(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.Set(map[string]any{"type": "remote", "url": "https://x"}, "mcp", "servers", "new")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "top comment") || !strings.Contains(string(out), "keep me") {
		t.Fatalf("comments lost:\n%s", out)
	}
	d2, err := ParseJSONC(out)
	if err != nil {
		t.Fatalf("invalid output: %v\n%s", err, out)
	}
	raw, ok := d2.RawAt("mcp", "servers", "new")
	if !ok {
		t.Fatal("new server not found")
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil || v["url"] != "https://x" {
		t.Fatalf("bad value: %s", raw)
	}
	if _, ok := d2.RawAt("mcp", "servers", "old"); !ok {
		t.Fatal("old server disappeared")
	}
}

func TestJSONCSetEmptyObject(t *testing.T) {
	doc, err := ParseJSONC([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.Set("v", "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ParseJSONC(out)
	if err != nil {
		t.Fatalf("invalid: %v\n%s", err, out)
	}
	raw, ok := d2.RawAt("a", "b")
	if !ok || string(raw) != `"v"` {
		t.Fatalf("unexpected: %s\n%s", raw, out)
	}
}

func TestJSONCRemove(t *testing.T) {
	src := []byte(`{
  "a": 1,
  "b": {
    "c": 2
  },
  "d": 3
}`)
	doc, _ := ParseJSONC(src)
	out, removed, err := doc.Remove("b")
	if err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	d2, err := ParseJSONC(out)
	if err != nil {
		t.Fatalf("invalid: %v\n%s", err, out)
	}
	if _, ok := d2.RawAt("b"); ok {
		t.Fatal("b still present")
	}
	if _, ok := d2.RawAt("a"); !ok {
		t.Fatal("a disappeared")
	}
	if _, ok := d2.RawAt("d"); !ok {
		t.Fatal("d disappeared")
	}
}

func TestJSONCAppendRemoveEqual(t *testing.T) {
	doc, err := ParseJSONC([]byte(`{"hooks": {"Stop": []}}`))
	if err != nil {
		t.Fatal(err)
	}
	g1 := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo 1"}}}
	g2 := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo 2"}}}
	out, err := doc.Append(g1, "hooks", "Stop")
	if err != nil {
		t.Fatal(err)
	}
	d2, _ := ParseJSONC(out)
	out, err = d2.Append(g2, "hooks", "Stop")
	if err != nil {
		t.Fatal(err)
	}
	d3, err := ParseJSONC(out)
	if err != nil {
		t.Fatalf("invalid: %v\n%s", err, out)
	}
	raw, _ := d3.RawAt("hooks", "Stop")
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 2 {
		t.Fatalf("expected 2 groups: %s", raw)
	}
	first, _ := json.Marshal(g1)
	out, found, err := d3.RemoveEqual(first, "hooks", "Stop")
	if err != nil || !found {
		t.Fatalf("remove equal: %v %v", found, err)
	}
	d4, err := ParseJSONC(out)
	if err != nil {
		t.Fatalf("invalid: %v\n%s", err, out)
	}
	raw, _ = d4.RawAt("hooks", "Stop")
	arr = nil
	_ = json.Unmarshal(raw, &arr)
	if len(arr) != 1 {
		t.Fatalf("expected 1 group: %s", raw)
	}
}

func TestJSONCCommentsInStrings(t *testing.T) {
	src := []byte(`{"url": "https://x", "note": "// not a comment"}`)
	doc, err := ParseJSONC(src)
	if err != nil {
		t.Fatal(err)
	}
	out, err := doc.Set("y", "extra")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "// not a comment") {
		t.Fatalf("string content changed: %s", out)
	}
}

func TestParseJSONCRejectsGarbage(t *testing.T) {
	if _, err := ParseJSONC([]byte("{oops}")); err == nil {
		t.Fatal("expected an error")
	}
}
