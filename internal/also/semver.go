package also

import (
	"strconv"
	"strings"
)

// Semver is a parsed semantic version.
type Semver struct {
	Major, Minor, Patch int
	Pre                 string // without leading '-'; empty for a stable release
	Build               string // without leading '+'
}

const maxSemverInt = int(^uint(0) >> 1)

// ParseSemver accepts "1.2.3", "v1.2.3", "1.2.3-alpha.1", "1.2.3+build.7".
func ParseSemver(s string) (Semver, bool) {
	nums, wild, pre, build, n, ok := parseSemverParts(s)
	if !ok || n != 3 || wild[0] || wild[1] || wild[2] {
		return Semver{}, false
	}
	return Semver{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre, Build: build}, true
}

func (v Semver) String() string {
	s := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// CompareSemver returns -1, 0 or 1. Build metadata is ignored.
func CompareSemver(a, b Semver) int {
	if c := cmpInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := cmpInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := cmpInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	return comparePre(a.Pre, b.Pre)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func comparePre(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return 1 // stable sorts after prerelease
	}
	if b == "" {
		return -1
	}
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if c := comparePreIdent(as[i], bs[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(as), len(bs))
}

func comparePreIdent(a, b string) int {
	if a == b {
		return 0
	}
	an, bn := allDigits(a), allDigits(b)
	switch {
	case an && bn:
		// Numeric identifiers compare by value; strip leading zeros.
		a2 := strings.TrimLeft(a, "0")
		b2 := strings.TrimLeft(b, "0")
		if a2 == "" {
			a2 = "0"
		}
		if b2 == "" {
			b2 = "0"
		}
		if c := cmpInt(len(a2), len(b2)); c != 0 {
			return c
		}
		return strings.Compare(a2, b2)
	case an:
		return -1 // numeric identifiers sort before alphanumeric ones
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

type semverOp int

const (
	opEq semverOp = iota
	opGt
	opGte
	opLt
	opLte
)

func (o semverOp) String() string {
	switch o {
	case opGt:
		return ">"
	case opGte:
		return ">="
	case opLt:
		return "<"
	case opLte:
		return "<="
	}
	return ""
}

type semverComparator struct {
	op semverOp
	v  Semver
}

// SemverRange is one or more space-separated comparators, e.g. "^1.2.3",
// "~1.2", ">=1.0.0 <2.0.0", "1.2.x", "1.x", "*".
type SemverRange struct {
	comps  []semverComparator
	hasPre bool
	ok     bool
}

// ParseSemverRange parses a range. Invalid input returns false.
func ParseSemverRange(s string) (SemverRange, bool) {
	s = strings.TrimSpace(s)
	r := SemverRange{ok: true}
	if s == "" {
		return r, true
	}
	for _, field := range strings.Fields(s) {
		comps, ok := parseRangeToken(field)
		if !ok {
			return SemverRange{}, false
		}
		r.comps = append(r.comps, comps...)
	}
	for _, c := range r.comps {
		if c.v.Pre != "" {
			r.hasPre = true
			break
		}
	}
	return r, true
}

func (r SemverRange) Match(v Semver) bool {
	if !r.ok {
		return false
	}
	// Prereleases only match ranges that mention a prerelease comparator.
	if v.Pre != "" && !r.hasPre {
		return false
	}
	for _, c := range r.comps {
		cmp := CompareSemver(v, c.v)
		switch c.op {
		case opEq:
			if cmp != 0 {
				return false
			}
		case opGt:
			if cmp <= 0 {
				return false
			}
		case opGte:
			if cmp < 0 {
				return false
			}
		case opLt:
			if cmp >= 0 {
				return false
			}
		case opLte:
			if cmp > 0 {
				return false
			}
		}
	}
	return true
}

func (r SemverRange) String() string {
	if !r.ok || len(r.comps) == 0 {
		return "*"
	}
	parts := make([]string, len(r.comps))
	for i, c := range r.comps {
		parts[i] = c.op.String() + c.v.String()
	}
	return strings.Join(parts, " ")
}

func parseRangeToken(tok string) ([]semverComparator, bool) {
	op := opEq
	hasOp := false
	switch {
	case strings.HasPrefix(tok, ">="):
		op, tok, hasOp = opGte, tok[2:], true
	case strings.HasPrefix(tok, "<="):
		op, tok, hasOp = opLte, tok[2:], true
	case strings.HasPrefix(tok, "~>"):
		return parseRangeExpansion('~', tok[2:])
	case strings.HasPrefix(tok, ">"):
		op, tok, hasOp = opGt, tok[1:], true
	case strings.HasPrefix(tok, "<"):
		op, tok, hasOp = opLt, tok[1:], true
	case strings.HasPrefix(tok, "="):
		op, tok, hasOp = opEq, tok[1:], true
	case strings.HasPrefix(tok, "^"):
		return parseRangeExpansion('^', tok[1:])
	case strings.HasPrefix(tok, "~"):
		return parseRangeExpansion('~', tok[1:])
	}

	nums, wild, pre, _, n, ok := parseSemverParts(tok)
	if !ok {
		return nil, false
	}
	spec := specifiedParts(wild, n)
	if spec == 0 {
		return nil, true // *, x, X, or an operator applied to a wildcard
	}
	base := Semver{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}

	if !hasOp {
		// Bare "1.2.3" is exact; bare "1.2" / "1.2.x" are x-ranges.
		return expandExact(nums, base, spec)
	}
	return []semverComparator{{op: op, v: base}}, true
}

func parseRangeExpansion(kind byte, tok string) ([]semverComparator, bool) {
	nums, wild, pre, _, n, ok := parseSemverParts(tok)
	if !ok {
		return nil, false
	}
	spec := specifiedParts(wild, n)
	if spec == 0 {
		return nil, true
	}
	base := Semver{Major: nums[0], Minor: nums[1], Patch: nums[2], Pre: pre}

	if kind == '~' {
		upper := Semver{}
		if spec == 1 {
			inc, ok := incInt(nums[0])
			if !ok {
				return nil, false
			}
			upper = Semver{Major: inc}
		} else {
			inc, ok := incInt(nums[1])
			if !ok {
				return nil, false
			}
			upper = Semver{Major: nums[0], Minor: inc}
		}
		return []semverComparator{{op: opGte, v: base}, {op: opLt, v: upper}}, true
	}

	// Caret.
	var upper Semver
	switch {
	case nums[0] > 0:
		inc, ok := incInt(nums[0])
		if !ok {
			return nil, false
		}
		upper = Semver{Major: inc}
	case spec == 1:
		upper = Semver{Major: 1}
	case nums[1] > 0:
		inc, ok := incInt(nums[1])
		if !ok {
			return nil, false
		}
		upper = Semver{Minor: inc}
	case spec == 2:
		upper = Semver{Minor: 1}
	default:
		inc, ok := incInt(nums[2])
		if !ok {
			return nil, false
		}
		upper = Semver{Patch: inc}
	}
	return []semverComparator{{op: opGte, v: base}, {op: opLt, v: upper}}, true
}

func expandExact(nums [3]int, base Semver, spec int) ([]semverComparator, bool) {
	if spec == 3 {
		return []semverComparator{{op: opEq, v: base}}, true
	}
	lower := base
	var upper Semver
	if spec == 1 {
		inc, ok := incInt(nums[0])
		if !ok {
			return nil, false
		}
		upper = Semver{Major: inc}
	} else {
		inc, ok := incInt(nums[1])
		if !ok {
			return nil, false
		}
		upper = Semver{Major: nums[0], Minor: inc}
	}
	return []semverComparator{{op: opGte, v: lower}, {op: opLt, v: upper}}, true
}

func specifiedParts(wild [3]bool, n int) int {
	spec := 0
	for spec < n && !wild[spec] {
		spec++
	}
	return spec
}

func incInt(n int) (int, bool) {
	if n == maxSemverInt {
		return 0, false
	}
	return n + 1, true
}

// parseSemverParts parses a version or a partial/wildcard version. It returns
// the numeric components, wildcard flags, prerelease and build metadata, the
// number of components present, and whether the input was valid.
func parseSemverParts(s string) (nums [3]int, wild [3]bool, pre, build string, n int, ok bool) {
	s = strings.TrimSpace(s)
	if s != "" && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	if s == "" {
		return nums, wild, "", "", 0, false
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		build = s[i+1:]
		s = s[:i]
		if !validIdentifiers(build, false) {
			return nums, wild, "", "", 0, false
		}
	}
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
		if !validIdentifiers(pre, true) {
			return nums, wild, "", "", 0, false
		}
	}
	if s == "" {
		return nums, wild, "", "", 0, false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return nums, wild, "", "", 0, false
	}
	sawWild := false
	for i, p := range parts {
		if p == "x" || p == "X" || p == "*" {
			sawWild = true
			wild[i] = true
			continue
		}
		if sawWild || !allDigits(p) || (len(p) > 1 && p[0] == '0') {
			return nums, wild, "", "", 0, false
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			return nums, wild, "", "", 0, false
		}
		nums[i] = v
	}
	n = len(parts)
	if pre != "" && (n != 3 || wild[0] || wild[1] || wild[2]) {
		return nums, wild, "", "", 0, false
	}
	return nums, wild, pre, build, n, true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func validIdentifiers(s string, pre bool) bool {
	if s == "" {
		return false
	}
	for _, id := range strings.Split(s, ".") {
		if id == "" {
			return false
		}
		for i := 0; i < len(id); i++ {
			c := id[i]
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '-' {
				continue
			}
			return false
		}
		if pre && allDigits(id) && len(id) > 1 && id[0] == '0' {
			return false
		}
	}
	return true
}

// HighestSemver returns the highest stable version; if there are no stable
// versions, the highest prerelease.
func HighestSemver(versions []Semver) (Semver, bool) {
	best := Semver{}
	found := false
	for _, v := range versions {
		if v.Pre != "" {
			continue
		}
		if !found || CompareSemver(v, best) > 0 {
			best = v
			found = true
		}
	}
	if found {
		return best, true
	}
	for _, v := range versions {
		if !found || CompareSemver(v, best) > 0 {
			best = v
			found = true
		}
	}
	return best, found
}
