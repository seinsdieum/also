package also

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
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
	// Windows editors commonly add a UTF-8 BOM; the harnesses accept it, so
	// also must parse the same bytes it is auditing.
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF})
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
	var found *jMember
	for _, m := range n.members {
		if m.key == key {
			// JSON parsers use the last duplicate key; match that behavior so
			// audit cannot see an empty first value while the harness sees the
			// executable last value.
			found = m
		}
	}
	return found
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

// childIndent returns the indentation of an object's closing brace and the
// indent unit its members use. An empty object has no member to copy, so the
// unit falls back to two spaces.
func childIndent(src []byte, n *jNode) (string, string) {
	closeIndent := indentAt(src, n.closeStart)
	for _, m := range n.members {
		ls := lineStart(src, m.keyStart)
		if !onlySpace(src[ls:m.keyStart]) {
			continue
		}
		keyIndent := string(src[ls:m.keyStart])
		if !strings.HasPrefix(keyIndent, closeIndent) || len(keyIndent) <= len(closeIndent) {
			continue
		}
		unit := keyIndent[len(closeIndent):]
		if onlySpace([]byte(unit)) {
			return closeIndent, unit
		}
	}
	return closeIndent, "  "
}

// memberIndent returns the indentation that a member's value should use for
// continuation lines. It is the key's line indentation, or the parent's child
// indentation when the key shares a line with other content.
func memberIndent(src []byte, n *jNode, m *jMember) string {
	ls := lineStart(src, m.keyStart)
	if onlySpace(src[ls:m.keyStart]) {
		return string(src[ls:m.keyStart])
	}
	closeIndent, unit := childIndent(src, n)
	return closeIndent + unit
}

// arrayIndent returns the indentation of the array's closing bracket and the
// unit its element lines use. It prefers the existing elements, then the
// parent object's members when the array is empty, and only falls back to two
// spaces when neither exists.
func (d *JSONC) arrayIndent(path []string) (string, string) {
	n := d.nodeAt(path)
	closeIndent := indentAt(d.src, n.closeStart)
	for _, el := range n.elems {
		ls := lineStart(d.src, el.start)
		if !onlySpace(d.src[ls:el.start]) {
			continue
		}
		elemIndent := string(d.src[ls:el.start])
		if !strings.HasPrefix(elemIndent, closeIndent) || len(elemIndent) <= len(closeIndent) {
			continue
		}
		unit := elemIndent[len(closeIndent):]
		if onlySpace([]byte(unit)) {
			return closeIndent, unit
		}
	}
	if len(path) > 0 {
		parent := d.nodeAt(path[:len(path)-1])
		if parent != nil && parent.kind == jObject {
			_, unit := childIndent(d.src, parent)
			if m := d.member(parent, path[len(path)-1]); m != nil && closeIndent == "" {
				// The array shares its member's line; expand it at the key's
				// indentation instead of at column zero.
				ls := lineStart(d.src, m.keyStart)
				if onlySpace(d.src[ls:m.keyStart]) {
					closeIndent = string(d.src[ls:m.keyStart])
				}
			}
			return closeIndent, unit
		}
	}
	return closeIndent, "  "
}

func reindent(raw []byte, indent, unit string) []byte {
	lines := bytes.Split(raw, []byte("\n"))
	if len(lines) == 1 {
		return raw
	}
	var out bytes.Buffer
	out.Write(lines[0])
	for _, l := range lines[1:] {
		out.WriteByte('\n')
		out.WriteString(indent)
		depth := 0
		for depth < len(l) && l[depth] == ' ' {
			depth++
		}
		for i := 0; i < depth/2; i++ {
			out.WriteString(unit)
		}
		out.Write(l[depth:])
	}
	return out.Bytes()
}

func encodeValue(v any, indent, unit string) ([]byte, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return reindent(raw, indent, unit), nil
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
		raw, err := encodeValue(value, "", "  ")
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
			want, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			if jsonEqual(d.src[m.val.start:m.val.end], want) {
				// Already the requested value. Leave the bytes untouched so a
				// repeated install cannot reformat the user's file.
				return d.src, nil
			}
			indent := memberIndent(d.src, n, m)
			_, unit := childIndent(d.src, n)
			raw, err := encodeValue(value, indent, unit)
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
	closeIndent, unit := childIndent(d.src, n)
	if len(n.members) == 0 {
		keyIndent := closeIndent + unit
		raw, err := encodeValue(value, keyIndent, unit)
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
	keyIndent := closeIndent + unit
	raw, err := encodeValue(value, keyIndent, unit)
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
	// Removing the only member empties the object. Collapse its interior so a
	// file that was exactly `{}` is restored byte-for-byte.
	if len(n.members) == 1 &&
		len(bytes.TrimSpace(d.src[n.openEnd:start])) == 0 &&
		len(bytes.TrimSpace(d.src[end:n.closeStart])) == 0 {
		return splice(d.src, n.openEnd, n.closeStart, nil), nil
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
	closeIndent, unit := d.arrayIndent(path)
	elemIndent := closeIndent + unit
	raw, err := encodeValue(value, elemIndent, unit)
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
	if _, err := decodeJSONValue(raw); err != nil {
		return nil, false, err
	}
	for idx, el := range n.elems {
		if !jsonEqual(raw, d.src[el.start:el.end]) {
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

// jsonEqual compares two raw JSON values. Numbers compare by value, so 1 and
// 1.0 stay equal while large integers stay distinct.
func jsonEqual(a, b json.RawMessage) bool {
	av, aerr := decodeJSONValue(a)
	bv, berr := decodeJSONValue(b)
	if aerr != nil || berr != nil {
		return false
	}
	return jsonValueEqual(av, bv)
}

// decodeJSONValue decodes raw JSON while keeping numbers as json.Number, so a
// value that is decoded and marshaled again cannot round large integers
// through float64.
func decodeJSONValue(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw[dec.InputOffset():])) != 0 {
		return nil, fmt.Errorf("unexpected trailing content after JSON value")
	}
	return v, nil
}

// jsonValueEqual compares decoded JSON values. Numbers compare by value, so
// 1 and 1.0 stay equal without losing large-integer precision.
func jsonValueEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, ok := new(big.Rat).SetString(av.String())
		if !ok {
			return av == bv
		}
		br, ok := new(big.Rat).SetString(bv.String())
		if !ok {
			return av == bv
		}
		return ar.Cmp(br) == 0
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, x := range av {
			y, ok := bv[k]
			if !ok || !jsonValueEqual(x, y) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonValueEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}
