package also

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// A minimal JSONC parser that keeps byte spans, so also can edit a user's
// config without deleting comments or reformatting unrelated keys.

type jKind int

const (
	jObject jKind = iota
	jArray
	jString
	jNumber
	jBool
	jNull
)

type jMember struct {
	key      string
	keyStart int
	val      *jNode
}

type jNode struct {
	kind       jKind
	start      int
	end        int
	openEnd    int // object/array: index just after '{' or '['
	closeStart int // object/array: index of '}' or ']'
	members    []*jMember
	elems      []*jNode
	str        string
}

type jparser struct {
	b []byte
	i int
}

func (p *jparser) skip() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\t', '\r', '\n':
			p.i++
		case '/':
			if p.i+1 < len(p.b) && p.b[p.i+1] == '/' {
				p.i += 2
				for p.i < len(p.b) && p.b[p.i] != '\n' {
					p.i++
				}
				continue
			}
			if p.i+1 < len(p.b) && p.b[p.i+1] == '*' {
				p.i += 2
				for p.i+1 < len(p.b) && !(p.b[p.i] == '*' && p.b[p.i+1] == '/') {
					p.i++
				}
				if p.i+1 < len(p.b) {
					p.i += 2
				}
				continue
			}
			return
		default:
			return
		}
	}
}

func (p *jparser) parseString() (string, int, int, error) {
	start := p.i
	if p.i >= len(p.b) || p.b[p.i] != '"' {
		return "", start, start, fmt.Errorf("expected string at byte %d", p.i)
	}
	p.i++
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case '\\':
			p.i += 2
		case '"':
			p.i++
			var s string
			if err := json.Unmarshal(p.b[start:p.i], &s); err != nil {
				return "", start, p.i, err
			}
			return s, start, p.i, nil
		default:
			p.i++
		}
	}
	return "", start, p.i, fmt.Errorf("unterminated string at byte %d", start)
}

func (p *jparser) parseValue() (*jNode, error) {
	p.skip()
	if p.i >= len(p.b) {
		return nil, fmt.Errorf("unexpected end of input")
	}
	start := p.i
	switch p.b[p.i] {
	case '{':
		n := &jNode{kind: jObject, start: start}
		p.i++
		n.openEnd = p.i
		p.skip()
		if p.i < len(p.b) && p.b[p.i] == '}' {
			n.closeStart = p.i
			p.i++
			n.end = p.i
			return n, nil
		}
		for {
			p.skip()
			key, keyStart, _, err := p.parseString()
			if err != nil {
				return nil, err
			}
			p.skip()
			if p.i >= len(p.b) || p.b[p.i] != ':' {
				return nil, fmt.Errorf("expected ':' at byte %d", p.i)
			}
			p.i++
			val, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			n.members = append(n.members, &jMember{key: key, keyStart: keyStart, val: val})
			p.skip()
			if p.i >= len(p.b) {
				return nil, fmt.Errorf("unterminated object")
			}
			if p.b[p.i] == ',' {
				p.i++
				p.skip()
				if p.i < len(p.b) && p.b[p.i] == '}' {
					n.closeStart = p.i
					p.i++
					n.end = p.i
					return n, nil
				}
				continue
			}
			if p.b[p.i] == '}' {
				n.closeStart = p.i
				p.i++
				n.end = p.i
				return n, nil
			}
			return nil, fmt.Errorf("expected ',' or '}' at byte %d", p.i)
		}
	case '[':
		n := &jNode{kind: jArray, start: start}
		p.i++
		n.openEnd = p.i
		p.skip()
		if p.i < len(p.b) && p.b[p.i] == ']' {
			n.closeStart = p.i
			p.i++
			n.end = p.i
			return n, nil
		}
		for {
			val, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			n.elems = append(n.elems, val)
			p.skip()
			if p.i >= len(p.b) {
				return nil, fmt.Errorf("unterminated array")
			}
			if p.b[p.i] == ',' {
				p.i++
				p.skip()
				if p.i < len(p.b) && p.b[p.i] == ']' {
					n.closeStart = p.i
					p.i++
					n.end = p.i
					return n, nil
				}
				continue
			}
			if p.b[p.i] == ']' {
				n.closeStart = p.i
				p.i++
				n.end = p.i
				return n, nil
			}
			return nil, fmt.Errorf("expected ',' or ']' at byte %d", p.i)
		}
	case '"':
		s, _, end, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &jNode{kind: jString, start: start, end: end, str: s}, nil
	case 't':
		if bytes.HasPrefix(p.b[p.i:], []byte("true")) {
			p.i += 4
			return &jNode{kind: jBool, start: start, end: p.i}, nil
		}
	case 'f':
		if bytes.HasPrefix(p.b[p.i:], []byte("false")) {
			p.i += 5
			return &jNode{kind: jBool, start: start, end: p.i}, nil
		}
	case 'n':
		if bytes.HasPrefix(p.b[p.i:], []byte("null")) {
			p.i += 4
			return &jNode{kind: jNull, start: start, end: p.i}, nil
		}
	default:
		j := p.i
		for p.i < len(p.b) {
			c := p.b[p.i]
			if (c >= '0' && c <= '9') || c == '-' || c == '+' || c == '.' || c == 'e' || c == 'E' {
				p.i++
				continue
			}
			break
		}
		if p.i > j {
			return &jNode{kind: jNumber, start: start, end: p.i}, nil
		}
	}
	return nil, fmt.Errorf("unexpected character %q at byte %d", p.b[p.i], p.i)
}

type JSONC struct {
	src  []byte
	root *jNode
}

func ParseJSONC(src []byte) (*JSONC, error) {
	p := &jparser{b: src}
	p.skip()
	root, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(src) {
		return nil, fmt.Errorf("unexpected trailing content at byte %d", p.i)
	}
	return &JSONC{src: src, root: root}, nil
}

func (d *JSONC) member(n *jNode, key string) *jMember {
	if n == nil || n.kind != jObject {
		return nil
	}
	for _, m := range n.members {
		if m.key == key {
			return m
		}
	}
	return nil
}

func (d *JSONC) nodeAt(path []string) *jNode {
	n := d.root
	for _, key := range path {
		m := d.member(n, key)
		if m == nil {
			return nil
		}
		n = m.val
	}
	return n
}

func (d *JSONC) RawAt(path ...string) (json.RawMessage, bool) {
	n := d.nodeAt(path)
	if n == nil {
		return nil, false
	}
	out := append([]byte(nil), d.src[n.start:n.end]...)
	return json.RawMessage(out), true
}

func (d *JSONC) TypeAt(path ...string) (string, bool) {
	n := d.nodeAt(path)
	if n == nil {
		return "", false
	}
	switch n.kind {
	case jObject:
		return "object", true
	case jArray:
		return "array", true
	case jString:
		return "string", true
	case jNumber:
		return "number", true
	case jBool:
		return "bool", true
	case jNull:
		return "null", true
	}
	return "", false
}

func splice(src []byte, start, end int, repl []byte) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(repl))
	out = append(out, src[:start]...)
	out = append(out, repl...)
	out = append(out, src[end:]...)
	return out
}

func lineStart(src []byte, i int) int {
	for i > 0 && src[i-1] != '\n' {
		i--
	}
	return i
}

func onlySpace(src []byte) bool {
	for _, c := range src {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}
	return true
}

func indentAt(src []byte, i int) string {
	ls := lineStart(src, i)
	if !onlySpace(src[ls:i]) {
		return ""
	}
	return string(src[ls:i])
}

func reindent(raw []byte, indent string) []byte {
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) == 1 {
		return raw
	}
	var out bytes.Buffer
	out.Write(lines[0])
	for _, l := range lines[1:] {
		out.WriteByte('\n')
		out.WriteString(indent)
		out.Write(l)
	}
	return out.Bytes()
}

func encodeValue(v any, indent string) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return reindent(raw, indent), nil
}

func encodeNested(rest []string, v any) (any, error) {
	if len(rest) == 0 {
		return v, nil
	}
	return map[string]any{rest[0]: mustNested(rest[1:], v)}, nil
}

func mustNested(rest []string, v any) any {
	if len(rest) == 0 {
		return v
	}
	return map[string]any{rest[0]: mustNested(rest[1:], v)}
}

// Set inserts or replaces path. Intermediate objects are created.
func (d *JSONC) Set(value any, path ...string) ([]byte, error) {
	if len(path) == 0 {
		raw, err := encodeValue(value, "")
		if err != nil {
			return nil, err
		}
		return raw, nil
	}
	n := d.root
	for i, key := range path {
		if n.kind != jObject {
			return nil, fmt.Errorf("%s is not a JSON object", strings.Join(path[:i], "."))
		}
		m := d.member(n, key)
		if m == nil {
			nested, err := encodeNested(path[i+1:], value)
			if err != nil {
				return nil, err
			}
			out, err := d.insertMember(n, key, nested)
			if err != nil {
				return nil, err
			}
			return validateJSONC(out)
		}
		if i == len(path)-1 {
			indent := indentAt(d.src, m.val.start)
			raw, err := encodeValue(value, indent)
			if err != nil {
				return nil, err
			}
			return validateJSONC(splice(d.src, m.val.start, m.val.end, raw))
		}
		n = m.val
	}
	return nil, fmt.Errorf("internal: empty path")
}

func (d *JSONC) insertMember(n *jNode, key string, value any) ([]byte, error) {
	keyJSON, _ := json.Marshal(key)
	if len(n.members) == 0 {
		closeIndent := indentAt(d.src, n.closeStart)
		keyIndent := closeIndent + "  "
		raw, err := encodeValue(value, keyIndent)
		if err != nil {
			return nil, err
		}
		text := append([]byte("\n"+keyIndent), keyJSON...)
		text = append(text, ':', ' ')
		text = append(text, raw...)
		text = append(text, '\n')
		text = append(text, closeIndent...)
		return splice(d.src, n.openEnd, n.closeStart, text), nil
	}
	last := n.members[len(n.members)-1]
	hasTrailingComma := false
	insertAt := last.val.end
	j := last.val.end
	for j < n.closeStart {
		c := d.src[j]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			j++
			continue
		}
		if c == ',' {
			hasTrailingComma = true
			insertAt = j + 1
		}
		break
	}
	closeIndent := indentAt(d.src, n.closeStart)
	keyIndent := closeIndent + "  "
	raw, err := encodeValue(value, keyIndent)
	if err != nil {
		return nil, err
	}
	var text []byte
	if !hasTrailingComma {
		text = append(text, ',')
	}
	text = append(text, '\n')
	text = append(text, keyIndent...)
	text = append(text, keyJSON...)
	text = append(text, ':', ' ')
	text = append(text, raw...)
	return splice(d.src, insertAt, insertAt, text), nil
}

func validateJSONC(src []byte) ([]byte, error) {
	if _, err := ParseJSONC(src); err != nil {
		return nil, fmt.Errorf("internal: edit produced invalid JSONC: %w", err)
	}
	return src, nil
}

// Remove deletes path. It returns the new document and whether the path existed.
func (d *JSONC) Remove(path ...string) ([]byte, bool, error) {
	if len(path) == 0 {
		return nil, false, fmt.Errorf("cannot remove the document root")
	}
	n := d.root
	for i, key := range path {
		m := d.member(n, key)
		if m == nil {
			return d.src, false, nil
		}
		if i == len(path)-1 {
			out, err := d.removeMember(n, m)
			if err != nil {
				return nil, false, err
			}
			validated, err := validateJSONC(out)
			return validated, true, err
		}
		n = m.val
	}
	return d.src, false, nil
}

func (d *JSONC) removeMember(n *jNode, m *jMember) ([]byte, error) {
	idx := -1
	for i, x := range n.members {
		if x == m {
			idx = i
			break
		}
	}
	isLast := idx == len(n.members)-1
	start := m.keyStart
	end := m.val.end
	// Remove the whole line when the member owns its line.
	ls := lineStart(d.src, start)
	if onlySpace(d.src[ls:start]) {
		start = ls
	}
	if !isLast {
		// Remove through the comma and the following newline, preserving the
		// next member's indentation.
		j := end
		for j < n.closeStart && d.src[j] != ',' {
			if d.src[j] == '"' || d.src[j] == '{' || d.src[j] == '[' {
				break
			}
			j++
		}
		if j < n.closeStart && d.src[j] == ',' {
			j++
			for j < len(d.src) {
				if d.src[j] == ' ' || d.src[j] == '\t' || d.src[j] == '\r' {
					j++
					continue
				}
				if d.src[j] == '\n' {
					j++
				}
				break
			}
			end = j
		}
	} else {
		// Drop the comma that followed the previous member.
		j := start
		for j > 0 {
			c := d.src[j-1]
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				j--
				continue
			}
			if c == ',' {
				start = j - 1
			}
			break
		}
	}
	return splice(d.src, start, end, nil), nil
}

// Append inserts value at the end of the array at path. Missing arrays are created.
func (d *JSONC) Append(value any, path ...string) ([]byte, error) {
	n := d.nodeAt(path)
	if n == nil {
		return d.Set([]any{value}, path...)
	}
	if n.kind != jArray {
		return nil, fmt.Errorf("%s is not an array", strings.Join(path, "."))
	}
	closeIndent := indentAt(d.src, n.closeStart)
	elemIndent := closeIndent + "  "
	raw, err := encodeValue(value, elemIndent)
	if err != nil {
		return nil, err
	}
	if len(n.elems) == 0 {
		text := append([]byte("\n"+elemIndent), raw...)
		text = append(text, '\n')
		text = append(text, closeIndent...)
		return validateJSONC(splice(d.src, n.openEnd, n.closeStart, text))
	}
	last := n.elems[len(n.elems)-1]
	hasComma := false
	insertAt := last.end
	j := last.end
	for j < n.closeStart {
		c := d.src[j]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			j++
			continue
		}
		if c == ',' {
			hasComma = true
			insertAt = j + 1
		}
		break
	}
	var text []byte
	if !hasComma {
		text = append(text, ',')
	}
	text = append(text, '\n')
	text = append(text, elemIndent...)
	text = append(text, raw...)
	return validateJSONC(splice(d.src, insertAt, insertAt, text))
}

// RemoveEqual removes the first array element semantically equal to raw.
func (d *JSONC) RemoveEqual(raw json.RawMessage, path ...string) ([]byte, bool, error) {
	n := d.nodeAt(path)
	if n == nil || n.kind != jArray {
		return d.src, false, nil
	}
	var want any
	if err := json.Unmarshal(raw, &want); err != nil {
		return nil, false, err
	}
	for idx, el := range n.elems {
		var got any
		if err := json.Unmarshal(d.src[el.start:el.end], &got); err != nil {
			continue
		}
		if !reflect.DeepEqual(want, got) {
			continue
		}
		start := el.start
		end := el.end
		isLast := idx == len(n.elems)-1
		ls := lineStart(d.src, start)
		if onlySpace(d.src[ls:start]) {
			start = ls
		}
		if !isLast {
			j := end
			for j < n.closeStart && d.src[j] != ',' {
				j++
			}
			if j < n.closeStart {
				j++
				for j < len(d.src) && (d.src[j] == ' ' || d.src[j] == '\t' || d.src[j] == '\r') {
					j++
				}
				if j < len(d.src) && d.src[j] == '\n' {
					j++
				}
				end = j
			}
		} else {
			j := start
			for j > 0 {
				c := d.src[j-1]
				if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
					j--
					continue
				}
				if c == ',' {
					start = j - 1
				}
				break
			}
		}
		out := splice(d.src, start, end, nil)
		validated, err := validateJSONC(out)
		return validated, true, err
	}
	return d.src, false, nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
