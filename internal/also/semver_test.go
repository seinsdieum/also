package also

import (
	"math"
	"strings"
	"testing"
)

func TestParseSemver(t *testing.T) {
	valid := []struct {
		in   string
		want Semver
	}{
		{"1.2.3", Semver{1, 2, 3, "", ""}},
		{"v1.2.3", Semver{1, 2, 3, "", ""}},
		{"V1.2.3", Semver{1, 2, 3, "", ""}},
		{"  v1.2.3  ", Semver{1, 2, 3, "", ""}},
		{"0.0.0", Semver{0, 0, 0, "", ""}},
		{"1.2.3-alpha.1", Semver{1, 2, 3, "alpha.1", ""}},
		{"1.2.3+build.7", Semver{1, 2, 3, "", "build.7"}},
		{"1.2.3-alpha.1+build.7", Semver{1, 2, 3, "alpha.1", "build.7"}},
		{"10.20.30-rc.1", Semver{10, 20, 30, "rc.1", ""}},
	}
	for _, tc := range valid {
		got, ok := ParseSemver(tc.in)
		if !ok {
			t.Errorf("ParseSemver(%q) = invalid, want %v", tc.in, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseSemver(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}

	invalid := []string{
		"", "v", "V", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.03",
		"1.2.3-", "1.2.3+", "1.2.3-01", "1.2.3-alpha..1", "1.2.3_1",
		"-1.2.3", "1.-2.3", "1.2.-3", "1.2.3-alpha+", "1.2.3+build+2",
		"99999999999999999999.0.0", "1.2.3-\u03b1", "\u2603",
	}
	for _, in := range invalid {
		if v, ok := ParseSemver(in); ok {
			t.Errorf("ParseSemver(%q) = %v, want invalid", in, v)
		}
	}
}

func TestSemverStringRoundTrip(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"1.2.3-alpha.1", "1.2.3-alpha.1"},
		{"1.2.3+build.7", "1.2.3+build.7"},
		{"1.2.3-alpha.1+build.7", "1.2.3-alpha.1+build.7"},
	}
	for _, tc := range cases {
		v, ok := ParseSemver(tc.in)
		if !ok {
			t.Fatalf("ParseSemver(%q) failed", tc.in)
		}
		if got := v.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
		again, ok := ParseSemver(v.String())
		if !ok || again != v {
			t.Errorf("round trip of %q: got %v, %v", v.String(), again, ok)
		}
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.2.4", "1.2.3", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.3.0", "1.2.9", 1},
		{"1.2.3-alpha", "1.2.3", -1},
		{"1.2.3", "1.2.3-alpha", 1},
		{"1.2.3-alpha", "1.2.3-beta", -1},
		{"1.2.3-alpha.1", "1.2.3-alpha.beta", -1},
		{"1.2.3-alpha.2", "1.2.3-alpha.10", -1},
		{"1.2.3-alpha", "1.2.3-alpha.1", -1},
		{"1.2.3+build.1", "1.2.3+build.2", 0},
		{"1.2.3-alpha+build.1", "1.2.3-alpha+build.2", 0},
	}
	for _, tc := range cases {
		a, ok := ParseSemver(tc.a)
		if !ok {
			t.Fatalf("ParseSemver(%q) failed", tc.a)
		}
		b, ok := ParseSemver(tc.b)
		if !ok {
			t.Fatalf("ParseSemver(%q) failed", tc.b)
		}
		if got := CompareSemver(a, b); got != tc.want {
			t.Errorf("CompareSemver(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func mustParseSemver(t *testing.T, s string) Semver {
	t.Helper()
	v, ok := ParseSemver(s)
	if !ok {
		t.Fatalf("ParseSemver(%q) failed", s)
	}
	return v
}

func TestParseSemverRange(t *testing.T) {
	cases := []struct {
		name    string
		rng     string
		matches []string
		rejects []string
	}{
		{
			name:    "caret major",
			rng:     "^1.2.3",
			matches: []string{"1.2.3", "1.2.4", "1.9.9"},
			rejects: []string{"1.2.2", "2.0.0", "0.9.9", "1.2.4-alpha"},
		},
		{
			name:    "caret zero minor",
			rng:     "^0.2.3",
			matches: []string{"0.2.3", "0.2.9"},
			rejects: []string{"0.2.2", "0.3.0", "1.0.0"},
		},
		{
			name:    "caret zero zero patch",
			rng:     "^0.0.3",
			matches: []string{"0.0.3"},
			rejects: []string{"0.0.2", "0.0.4", "0.1.0"},
		},
		{
			name:    "tilde full",
			rng:     "~1.2.3",
			matches: []string{"1.2.3", "1.2.99"},
			rejects: []string{"1.2.2", "1.3.0", "2.0.0"},
		},
		{
			name:    "tilde minor",
			rng:     "~1.2",
			matches: []string{"1.2.0", "1.2.9"},
			rejects: []string{"1.3.0", "1.1.9"},
		},
		{
			name:    "tilde major",
			rng:     "~1",
			matches: []string{"1.0.0", "1.9.9"},
			rejects: []string{"2.0.0", "0.9.9"},
		},
		{
			name:    "x patch",
			rng:     "1.2.x",
			matches: []string{"1.2.0", "1.2.99"},
			rejects: []string{"1.3.0", "1.1.9"},
		},
		{
			name:    "x minor",
			rng:     "1.x",
			matches: []string{"1.0.0", "1.9.9"},
			rejects: []string{"2.0.0", "0.9.9"},
		},
		{
			name:    "wildcard",
			rng:     "*",
			matches: []string{"0.0.0", "1.2.3"},
			rejects: []string{"1.2.3-alpha"},
		},
		{
			name:    "x wildcard",
			rng:     "x",
			matches: []string{"1.2.3"},
			rejects: []string{"1.2.3-alpha"},
		},
		{
			name:    "empty",
			rng:     "",
			matches: []string{"1.2.3"},
			rejects: []string{"1.2.3-alpha"},
		},
		{
			name:    "comparator and",
			rng:     ">=1.0.0 <2.0.0",
			matches: []string{"1.0.0", "1.9.9"},
			rejects: []string{"0.9.9", "2.0.0", "1.5.0-alpha"},
		},
		{
			name:    "greater than",
			rng:     ">1.2.3",
			matches: []string{"1.2.4", "2.0.0"},
			rejects: []string{"1.2.3", "1.2.2"},
		},
		{
			name:    "less than or equal",
			rng:     "<=1.2.3",
			matches: []string{"1.2.3", "0.1.0"},
			rejects: []string{"1.2.4"},
		},
		{
			name:    "equals prefix",
			rng:     "=1.2.3",
			matches: []string{"1.2.3"},
			rejects: []string{"1.2.4", "1.2.3-alpha"},
		},
		{
			name:    "exact stable",
			rng:     "1.2.3",
			matches: []string{"1.2.3"},
			rejects: []string{"1.2.4", "1.2.3-alpha"},
		},
		{
			name:    "exact prerelease",
			rng:     "1.2.3-alpha.1",
			matches: []string{"1.2.3-alpha.1", "1.2.3-alpha.1+build.7"},
			rejects: []string{"1.2.3-alpha.2", "1.2.3"},
		},
		{
			name:    "prerelease comparator enables prereleases",
			rng:     ">=1.2.3-alpha <2.0.0",
			matches: []string{"1.2.3-beta", "1.5.0"},
			rejects: []string{"1.2.2", "2.0.0"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := ParseSemverRange(tc.rng)
			if !ok {
				t.Fatalf("ParseSemverRange(%q) = invalid", tc.rng)
			}
			for _, s := range tc.matches {
				if !r.Match(mustParseSemver(t, s)) {
					t.Errorf("range %q should match %q", tc.rng, s)
				}
			}
			for _, s := range tc.rejects {
				if r.Match(mustParseSemver(t, s)) {
					t.Errorf("range %q should not match %q", tc.rng, s)
				}
			}
		})
	}
}

func TestSemverRangeInvalid(t *testing.T) {
	cases := []string{
		"foo", "1.2.3.4", ">=1.0.0 <", "^", "~", "=>1.2.3",
		"1.2.3 - 2.0.0", "1.2.3 || 2.0.0", "1.x.3", "01.2.3",
		">=1.2.3 2.0.0.0", "1.2.3-alpha..1", "\u2603",
	}
	for _, in := range cases {
		if r, ok := ParseSemverRange(in); ok {
			t.Errorf("ParseSemverRange(%q) = %q, want invalid", in, r.String())
		}
	}
}

func TestSemverRangeString(t *testing.T) {
	inputs := []string{
		"", "*", "x", "^1.2.3", "^0.2.3", "^0.0.3", "~1.2.3", "~1.2", "~1",
		"1.2.x", "1.x", "1.2.3", ">=1.0.0 <2.0.0",
	}
	for _, in := range inputs {
		r, ok := ParseSemverRange(in)
		if !ok {
			t.Fatalf("ParseSemverRange(%q) failed", in)
		}
		if r.String() == "" {
			t.Errorf("ParseSemverRange(%q).String() is empty", in)
		}
		again, ok := ParseSemverRange(r.String())
		if !ok {
			t.Errorf("reparse of %q failed", r.String())
			continue
		}
		for _, s := range []string{"0.0.0", "1.2.3", "1.9.9", "2.0.0", "1.2.3-alpha"} {
			v := mustParseSemver(t, s)
			if r.Match(v) != again.Match(v) {
				t.Errorf("round trip of %q differs on %q", in, s)
			}
		}
	}
}

func TestHighestSemver(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
		ok   bool
	}{
		{"empty", nil, "", false},
		{"single stable", []string{"1.2.3"}, "1.2.3", true},
		{"picks highest stable", []string{"1.2.3", "2.0.0", "1.9.9"}, "2.0.0", true},
		{"ignores higher prerelease", []string{"2.0.0-beta", "1.5.0"}, "1.5.0", true},
		{"only prereleases", []string{"0.9.0-alpha", "1.0.0-alpha", "1.0.0-beta"}, "1.0.0-beta", true},
		{"prerelease ordering", []string{"1.0.0-alpha.2", "1.0.0-alpha.10"}, "1.0.0-alpha.10", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var versions []Semver
			for _, s := range tc.in {
				versions = append(versions, mustParseSemver(t, s))
			}
			got, ok := HighestSemver(versions)
			if ok != tc.ok {
				t.Fatalf("HighestSemver() ok = %v, want %v", ok, tc.ok)
			}
			if ok && got.String() != tc.want {
				t.Errorf("HighestSemver() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSemverEdgeInputs(t *testing.T) {
	inputs := []string{
		"", " ", "v", "V", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3",
		"99999999999999999999.0.0", "1.2.3-\u03b1", "\u2603", "\x00",
		"^", "~", ">=", "*", "1.x.3", strings.Repeat("9", 100) + ".0.0",
	}
	for _, in := range inputs {
		if v, ok := ParseSemver(in); ok {
			_ = v.String()
		}
		r, ok := ParseSemverRange(in)
		if ok {
			_ = r.String()
			_ = r.Match(Semver{})
			_ = r.Match(Semver{Major: 1, Pre: "\u03b1"})
		}
	}
}

func FuzzParseSemverRange(f *testing.F) {
	for _, s := range []string{
		"", "*", "x", "1", "1.2", "1.2.3", "^1.2.3", "~1.2", "~1",
		"1.2.x", "1.x", ">=1.0.0 <2.0.0", "1.2.3-alpha.1",
		">=1.2.3-alpha <2.0.0", "foo", "1.2.3.4", "01.2.3",
		"99999999999999999999.0.0", "^9223372036854775807.0.0", "\u2603",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, ok := ParseSemverRange(s)
		if !ok {
			return
		}
		// An accepted range must never panic and its canonical form must parse.
		out := r.String()
		again, ok := ParseSemverRange(out)
		if !ok {
			t.Fatalf("accepted range %q rendered unparsable %q", s, out)
		}
		for _, v := range []Semver{
			{},
			{Major: 1, Minor: 2, Patch: 3},
			{Major: 1, Pre: "alpha"},
			{Major: 0, Minor: 0, Patch: 4},
			{Major: math.MaxInt},
		} {
			if r.Match(v) != again.Match(v) {
				t.Fatalf("range %q and canonical %q disagree on %v", s, out, v)
			}
		}
	})
}
