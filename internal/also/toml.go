package also

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

func parseTOMLMap(data []byte) (map[string]any, error) {
	var m map[string]any
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

var bareTOMLKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func tomlKey(k string) string {
	if bareTOMLKey.MatchString(k) {
		return k
	}
	return tomlString(k)
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04X`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func tomlStringArray(vals []string) string {
	parts := make([]string, 0, len(vals))
	for _, v := range vals {
		parts = append(parts, tomlString(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func tomlInlineTable(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, tomlKey(k)+" = "+tomlString(m[k]))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func markerBegin(marker string) string { return "# also:begin " + marker }
func markerEnd(marker string) string   { return "# also:end " + marker }

func tomlAppendBlock(src []byte, marker, block string) []byte {
	var b bytes.Buffer
	b.Write(src)
	if len(src) > 0 && src[len(src)-1] != '\n' {
		b.WriteByte('\n')
	}
	if len(src) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(markerBegin(marker) + "\n")
	b.WriteString(block)
	if !strings.HasSuffix(block, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(markerEnd(marker) + "\n")
	return b.Bytes()
}

func tomlRemoveBlock(src []byte, marker string) ([]byte, bool) {
	begin := markerBegin(marker)
	end := markerEnd(marker)
	lines := strings.SplitAfter(string(src), "\n")
	start, stop := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == begin && start < 0 {
			start = i
		}
		if t == end && start >= 0 {
			stop = i
			break
		}
	}
	if start < 0 || stop < 0 {
		return src, false
	}
	var b bytes.Buffer
	for i, l := range lines {
		if i >= start && i <= stop {
			continue
		}
		b.WriteString(l)
	}
	out := b.Bytes()
	// Trim one extra blank line left by a removed trailing block.
	out = bytes.TrimRight(out, "\n")
	if len(out) > 0 {
		out = append(out, '\n')
	}
	return out, true
}

func tomlHasMCPServer(src []byte, name string) bool {
	root, err := parseTOMLMap(src)
	if err != nil {
		return false
	}
	m, ok := root["mcp_servers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = m[name]
	return ok
}
