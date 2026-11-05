package also

import (
	"strings"
	"testing"
)

func TestConvertCommandQwenRoundTrip(t *testing.T) {
	in := []byte("---\ndescription: d\n---\nReview $ARGUMENTS\n\n!`git diff`\n")
	q, warns := ConvertCommand(in, "claude", "qwen", "review.md")
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	qs := string(q)
	if !strings.Contains(qs, "{{args}}") || strings.Contains(qs, "$ARGUMENTS") {
		t.Fatalf("arguments not translated: %s", qs)
	}
	if !strings.Contains(qs, "!{git diff}") || strings.Contains(qs, "!`") {
		t.Fatalf("shell block not translated: %s", qs)
	}
	back, _ := ConvertCommand(q, "qwen", "claude", "review.md")
	if string(back) != string(in) {
		t.Fatalf("round trip mismatch:\n--- in\n%s\n--- back\n%s", in, back)
	}
}

func TestConvertCommandWarnsOnPositionalArgs(t *testing.T) {
	_, warns := ConvertCommand([]byte("Check $1 and $2\n"), "claude", "qwen", "c.md")
	if len(warns) == 0 {
		t.Fatal("expected a warning for positional args")
	}
}
