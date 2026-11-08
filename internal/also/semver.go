package also

// Semantic versions are parsed by Masterminds/semver. The wrapper keeps the
// rest of also independent of the library's types and adds the one behavior
// this project needs: stable versions win over prereleases.

import (
	"fmt"
	"math"
	"strings"

	semverlib "github.com/Masterminds/semver/v3"
)

type Semver struct {
	Major, Minor, Patch int
	Pre                 string
	Build               string
}

// ParseSemver accepts "1.2.3", "v1.2.3" and prerelease/build suffixes.
// Partial versions such as "1.2" and leading zeroes are rejected.
func ParseSemver(s string) (Semver, bool) {
	s = strings.TrimSpace(s)
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	v, err := semverlib.StrictNewVersion(s)
	if err != nil {
		return Semver{}, false
	}
	if v.Major() > uint64(math.MaxInt) || v.Minor() > uint64(math.MaxInt) || v.Patch() > uint64(math.MaxInt) {
		return Semver{}, false
	}
	return Semver{
		Major: int(v.Major()), Minor: int(v.Minor()), Patch: int(v.Patch()),
		Pre: v.Prerelease(), Build: v.Metadata(),
	}, true
}

func (v Semver) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

func (v Semver) lib() *semverlib.Version {
	return semverlib.New(uint64(nonNegative(v.Major)), uint64(nonNegative(v.Minor)),
		uint64(nonNegative(v.Patch)), v.Pre, v.Build)
}

func nonNegative(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// CompareSemver returns -1, 0 or 1. Prereleases sort before the release.
func CompareSemver(a, b Semver) int {
	return a.lib().Compare(b.lib())
}

// SemverRange is a Masterminds constraint set, e.g. "^1.2.3",
// "~1.2", ">=1.0.0 <2.0.0", "1.2.x", "1.x" or "*".
type SemverRange struct {
	constraints *semverlib.Constraints
	raw         string
}

func ParseSemverRange(s string) (SemverRange, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "*"
	}
	// A bare v-prefixed version is common; normalize it before parsing.
	if len(s) > 1 && (s[0] == 'v' || s[0] == 'V') {
		if _, err := semverlib.StrictNewVersion(s[1:]); err == nil {
			s = s[1:]
		}
	}
	if !validRangeSyntax(s) {
		return SemverRange{}, false
	}
	constraints, err := semverlib.NewConstraint(s)
	if err != nil {
		return SemverRange{}, false
	}
	return SemverRange{constraints: constraints, raw: s}, true
}

// validRangeSyntax rejects forms Masterminds accepts but also does not
// document: OR, hyphen ranges, malformed operators, mixed wildcards and
// leading zeroes. Keeping one range syntax makes error messages predictable.
func validRangeSyntax(s string) bool {
	if strings.Contains(s, "||") || strings.Contains(s, " - ") ||
		strings.Contains(s, "=>") || strings.Contains(s, "=<") {
		return false
	}
	for _, token := range strings.Fields(s) {
		token = strings.TrimLeft(token, "^~><=vV")
		if token == "" || token == "x" || token == "X" || token == "*" {
			continue
		}
		core := token
		if i := strings.IndexAny(core, "-+"); i >= 0 {
			core = core[:i]
		}
		if core == "" {
			continue
		}
		wild := false
		for _, part := range strings.Split(core, ".") {
			if part == "x" || part == "X" || part == "*" {
				wild = true
				continue
			}
			if wild || part == "" {
				return false
			}
			for _, c := range part {
				if c < '0' || c > '9' {
					return false
				}
			}
			if len(part) > 1 && part[0] == '0' {
				return false
			}
		}
	}
	return true
}

func (r SemverRange) Match(v Semver) bool {
	if r.constraints == nil {
		return false
	}
	return r.constraints.Check(v.lib())
}

func (r SemverRange) String() string {
	if r.constraints == nil {
		return "*"
	}
	return r.constraints.String()
}

// HighestSemver prefers the highest stable version; prereleases are used only
// when no stable version exists.
func HighestSemver(versions []Semver) (Semver, bool) {
	var stable, any *Semver
	for i := range versions {
		v := versions[i]
		if v.Pre == "" {
			if stable == nil || CompareSemver(v, *stable) > 0 {
				stable = &v
			}
		}
		if any == nil || CompareSemver(v, *any) > 0 {
			any = &v
		}
	}
	if stable != nil {
		return *stable, true
	}
	if any != nil {
		return *any, true
	}
	return Semver{}, false
}
