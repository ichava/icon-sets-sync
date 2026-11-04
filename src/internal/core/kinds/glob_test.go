package kinds

import "testing"

// globVectors is contract §1.2's vector table, verbatim. core's
// Support\KindGlob carries the same table as a test; the two implementations
// must agree on every row.
var globVectors = []struct {
	pattern string
	path    string
	match   bool
	invalid bool
}{
	{pattern: "scenes/**", path: "scenes/office", match: true},
	{pattern: "scenes/**", path: "scenes/a/b", match: true},
	{pattern: "scenes/*", path: "scenes/a/b", match: false},
	{pattern: "**/hero-*", path: "hero-x", match: true},
	{pattern: "**/hero-*", path: "a/b/hero-x", match: true},
	{pattern: "scenes/**/x", path: "scenes/x", match: true},
	{pattern: "scenes/offic?", path: "scenes/office", match: true},
	{pattern: "Scenes/**", path: "scenes/a", match: false},
	{pattern: "spot/tiny-*", path: "spot/tiny-cat", match: true},
	{pattern: "a**", invalid: true},
	{pattern: "[ab]/x", invalid: true},
}

func TestGlobVectorTable(t *testing.T) {
	if len(globVectors) != 11 {
		t.Fatalf("the contract's vector table has 11 rows; this has %d", len(globVectors))
	}
	for _, v := range globVectors {
		t.Run(v.pattern+" vs "+v.path, func(t *testing.T) {
			g, err := ParseGlob(v.pattern)
			if v.invalid {
				if err == nil {
					t.Fatalf("ParseGlob(%q) accepted an invalid pattern", v.pattern)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseGlob(%q): %v", v.pattern, err)
			}
			if got := g.Match(v.path); got != v.match {
				t.Errorf("%q matches %q = %v, want %v", v.pattern, v.path, got, v.match)
			}
		})
	}
}

func TestGlobInvalidPatterns(t *testing.T) {
	for _, p := range []string{
		"", "/a", "a/../b", "..", "a//b", "a/", "a**", "**a", "a/**b", "a/b**/c", "***",
		"[ab]", "a]", "{a,b}", "a{", "a}", `a\b`, `\`,
	} {
		t.Run(p, func(t *testing.T) {
			if _, err := ParseGlob(p); err == nil {
				t.Errorf("ParseGlob(%q) accepted an invalid pattern", p)
			}
		})
	}
}

func TestGlobMatching(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// star and question mark never cross a slash
		{"a/*", "a/b", true},
		{"a/*", "a/", false},
		{"a/*", "a", false},
		{"a/*/c", "a/b/c", true},
		{"a/*/c", "a/b/x/c", false},
		{"a?c", "a/c", false},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"?", "é", true}, // one character, not one byte
		{"*", "a", true},
		{"*", "", false}, // not an item path
		{"*.x", "a.x", true},
		{"*a*b*", "xxaxxbxx", true},
		{"*a*b*", "xxbxxaxx", false},
		// anchored: the whole path, not a substring
		{"office", "scenes/office", false},
		{"scenes", "scenes/office", false},
		{"scenes/office", "scenes/office", true},
		{"scenes/office", "scenes/office2", false},
		// double star is whole segments
		{"**", "a", true},
		{"**", "a/b/c", true},
		{"a/**", "a", true}, // zero segments
		{"a/**", "a/b/c", true},
		{"a/**", "b/a/c", false},
		{"**/x", "x", true},
		{"**/x", "a/b/x", true},
		{"**/x", "a/b/xy", false},
		{"a/**/b/**/c", "a/b/c", true},
		{"a/**/b/**/c", "a/1/2/b/3/4/c", true},
		{"a/**/b/**/c", "a/c", false},
		{"**/**", "a/b", true},
		// case-sensitive
		{"Scenes/*", "scenes/a", false},
		{"scenes/*", "scenes/A", true},
		// a dot is a dot
		{"a.b", "a.b", true},
		{"a.b", "axb", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+" vs "+tc.path, func(t *testing.T) {
			g, err := ParseGlob(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			if got := g.Match(tc.path); got != tc.want {
				t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

// A pattern built to blow up a naive backtracking matcher must still finish.
func TestGlobIsNotExponential(t *testing.T) {
	g, err := ParseGlob("**/**/**/**/**/**/**/**/**/**/x")
	if err != nil {
		t.Fatal(err)
	}
	if g.Match("a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/a/y") {
		t.Fatal("matched a path that does not end in x")
	}
	g2, _ := ParseGlob("*a*a*a*a*a*a*a*a*a*b")
	if g2.Match("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
		t.Fatal("matched a path that does not end in b")
	}
}
