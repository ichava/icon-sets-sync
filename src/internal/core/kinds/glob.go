// Package kinds resolves what kind an icon-pack item is (icon or illustration)
// from the pack's config.json, and suggests kinds for items a human has not
// classified. It is the Go side of the "kinds and flavours" contract; core's
// Support\KindResolver and Support\KindGlob are the PHP side, and the glob
// vector table and heuristic constants are identical in both.
package kinds

import (
	"fmt"
	"strings"
)

// Glob is a compiled item-path pattern (contract section 1.2). In a pattern,
// "*" is zero or more characters other than "/", "?" is exactly one character
// other than "/", and "**" is zero or more whole segments (it must be a whole
// segment).
//
// The match is anchored (the whole path) and case-sensitive. Everything else is
// literal. It is matched segment by segment rather than compiled to a regexp,
// which keeps "never /" true by construction and makes the cost bounded.
type Glob struct {
	pattern string
	segs    []string
}

// reservedGlobChars are rejected rather than guessed at, so a pack author who
// writes a character class or an alternation hears about it instead of getting
// a rule that silently matches nothing.
const reservedGlobChars = `[]{}\`

// ParseGlob validates and compiles a pattern. The error says why it is invalid.
func ParseGlob(pattern string) (*Glob, error) {
	switch {
	case pattern == "":
		return nil, fmt.Errorf("empty pattern")
	case strings.HasPrefix(pattern, "/"):
		return nil, fmt.Errorf("pattern %q starts with a slash", pattern)
	}
	if i := strings.IndexAny(pattern, reservedGlobChars); i >= 0 {
		return nil, fmt.Errorf("pattern %q uses the reserved character %q", pattern, pattern[i])
	}

	segs := strings.Split(pattern, "/")
	for _, s := range segs {
		switch {
		case s == "":
			return nil, fmt.Errorf("pattern %q has an empty segment", pattern)
		case s == "..":
			return nil, fmt.Errorf("pattern %q has a .. segment", pattern)
		case strings.Contains(s, "**") && s != "**":
			return nil, fmt.Errorf("pattern %q: ** must be a whole segment", pattern)
		}
	}
	return &Glob{pattern: pattern, segs: segs}, nil
}

// String is the pattern as written.
func (g *Glob) String() string { return g.pattern }

// Match reports whether an item path matches the whole pattern.
func (g *Glob) Match(path string) bool {
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "" {
			return false // not an item path: empty, leading, trailing or doubled slash
		}
	}

	// memo[i][j] is 0 unknown, 1 matches, 2 does not: pattern segment i against
	// path segment j onward. It keeps a run of ** from going exponential.
	memo := make([][]uint8, len(g.segs)+1)
	for i := range memo {
		memo[i] = make([]uint8, len(parts)+1)
	}

	var walk func(i, j int) bool
	walk = func(i, j int) bool {
		if m := memo[i][j]; m != 0 {
			return m == 1
		}
		var ok bool
		switch {
		case i == len(g.segs):
			ok = j == len(parts)
		case g.segs[i] == "**":
			// Zero segments, or swallow one and stay on the **.
			ok = walk(i+1, j) || (j < len(parts) && walk(i, j+1))
		default:
			ok = j < len(parts) && matchSegment(g.segs[i], parts[j]) && walk(i+1, j+1)
		}
		if ok {
			memo[i][j] = 1
		} else {
			memo[i][j] = 2
		}
		return ok
	}
	return walk(0, 0)
}

// matchSegment matches one path segment against a pattern segment holding only
// literals, * and ?. Iterative with a single backtrack point, so it is linear
// in the pattern for each position of the last star.
func matchSegment(pattern, s string) bool {
	p, t := []rune(pattern), []rune(s)
	pi, ti := 0, 0
	star, mark := -1, 0

	for ti < len(t) {
		switch {
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ti
			pi++
		case pi < len(p) && (p[pi] == '?' || p[pi] == t[ti]):
			pi++
			ti++
		case star >= 0:
			pi = star + 1
			mark++
			ti = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
