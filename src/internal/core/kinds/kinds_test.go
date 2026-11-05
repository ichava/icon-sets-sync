package kinds

import (
	"strings"
	"testing"
)

// mixedPack is contract section 1.4's example, verbatim.
const mixedPack = `{
  "package": { "name": "acme/scenes", "kind": "icon" },
  "metadata": { "data": {
    "categories": {
      "illustrations": { "name": "Illustrations", "kind": "illustration", "theme_variants": { "dark": "-dark" } }
    },
    "kinds": { "rules": [
      { "match": "spot/tiny-*", "kind": "icon", "note": "spot glyphs are 24px" },
      { "match": "spot/**",     "kind": "illustration" }
    ] },
    "items": {
      "glyphs/hero-banner": { "kind": "illustration" }
    }
  } }
}`

func mustParse(t *testing.T, doc string) *Config {
	t.Helper()
	cfg, err := ParseConfig([]byte(doc))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

// TestMixedPackTable is contract section 1.4's result table.
func TestMixedPackTable(t *testing.T) {
	cfg := mustParse(t, mixedPack)
	if len(cfg.Issues) != 0 {
		t.Fatalf("unexpected issues: %+v", cfg.Issues)
	}
	for _, tc := range []struct {
		path string
		want Resolution
	}{
		{"glyphs/hero-banner", Resolution{Illustration, SourceItem, ""}},
		{"glyphs/home", Resolution{Icon, SourcePack, ""}},
		{"spot/tiny-cat", Resolution{Icon, SourceRule, "spot/tiny-*"}},
		{"spot/big/park", Resolution{Illustration, SourceRule, "spot/**"}},
		{"illustrations/office", Resolution{Illustration, SourceCategory, "illustrations"}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := cfg.Resolve(tc.path); got != tc.want {
				t.Errorf("Resolve(%q) = %+v, want %+v", tc.path, got, tc.want)
			}
		})
	}
}

// One test per D6 row a pack can set (rows 2 to 6; row 1 is the Laravel host's
// config and does not exist in a sync run).
func TestEachRowOnItsOwn(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		path string
		want Resolution
	}{
		{"row 2: item", `{"metadata":{"data":{"items":{"a/b":{"kind":"illustration"}}}}}`, "a/b", Resolution{Illustration, SourceItem, ""}},
		{"row 3: rule", `{"metadata":{"data":{"kinds":{"rules":[{"match":"a/**","kind":"illustration"}]}}}}`, "a/b", Resolution{Illustration, SourceRule, "a/**"}},
		{"row 4: category", `{"metadata":{"data":{"categories":{"a":{"kind":"illustration"}}}}}`, "a/b", Resolution{Illustration, SourceCategory, "a"}},
		{"row 5: pack", `{"package":{"kind":"illustration"}}`, "a/b", Resolution{Illustration, SourcePack, ""}},
		{"row 6: default", `{"package":{"name":"x"}}`, "a/b", Resolution{Icon, SourceDefault, ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mustParse(t, tc.doc).Resolve(tc.path); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Precedence on a single item: 2 beats 3 beats 4 beats 5 beats 6. Each step
// removes the winner and checks the next row takes over.
func TestPrecedenceChain(t *testing.T) {
	// Every row names a different kind from the one below it, so a wrong winner
	// cannot hide behind agreeing values.
	full := map[string]string{
		"item":     `"items":{"a/x":{"kind":"illustration"}}`,
		"rule":     `"kinds":{"rules":[{"match":"a/**","kind":"icon"}]}`,
		"category": `"categories":{"a":{"kind":"illustration"}}`,
	}
	build := func(item, rule, category bool, pack string) string {
		var data []string
		if item {
			data = append(data, full["item"])
		}
		if rule {
			data = append(data, full["rule"])
		}
		if category {
			data = append(data, full["category"])
		}
		pk := ""
		if pack != "" {
			pk = `"package":{"kind":"` + pack + `"},`
		}
		return `{` + pk + `"metadata":{"data":{` + strings.Join(data, ",") + `}}}`
	}

	steps := []struct {
		name string
		doc  string
		want Resolution
	}{
		{"2 beats 3, 4, 5", build(true, true, true, "icon"), Resolution{Illustration, SourceItem, ""}},
		{"3 beats 4, 5", build(false, true, true, "illustration"), Resolution{Icon, SourceRule, "a/**"}},
		{"4 beats 5", build(false, false, true, "icon"), Resolution{Illustration, SourceCategory, "a"}},
		{"5 beats 6", build(false, false, false, "illustration"), Resolution{Illustration, SourcePack, ""}},
		{"6 is the default", build(false, false, false, ""), Resolution{Icon, SourceDefault, ""}},
	}
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if got := mustParse(t, s.doc).Resolve("a/x"); got != s.want {
				t.Errorf("got %+v, want %+v", got, s.want)
			}
		})
	}
}

func TestFirstMatchingRuleWins(t *testing.T) {
	doc := func(first, second string) string {
		return `{"metadata":{"data":{"kinds":{"rules":[
			{"match":"s/**","kind":"` + first + `"},
			{"match":"s/tiny-*","kind":"` + second + `"}]}}}}`
	}
	// Broad rule first: it wins, even though the specific one would disagree.
	got := mustParse(t, doc("illustration", "icon")).Resolve("s/tiny-cat")
	if got.Kind != Illustration || got.Rule != "s/**" {
		t.Errorf("first rule did not win: %+v", got)
	}
	// Swapping the order swaps the winner, which is the only thing that
	// distinguishes "first wins" from "last wins" or "most specific wins".
	swapped := `{"metadata":{"data":{"kinds":{"rules":[
		{"match":"s/tiny-*","kind":"icon"},{"match":"s/**","kind":"illustration"}]}}}}`
	got = mustParse(t, swapped).Resolve("s/tiny-cat")
	if got.Kind != Icon || got.Rule != "s/tiny-*" {
		t.Errorf("reordered rules: %+v", got)
	}
}

// A v0.1 manifest names no kind key anywhere. Every item must read icon/default.
func TestV01ManifestWithNoKindKeys(t *testing.T) {
	cfg := mustParse(t, `{
	  "schema_version": "1.0",
	  "package": { "name": "ichava/icon-sets-example", "version": "0.1.0", "type": "single" },
	  "metadata": { "data": { "variants": { "solid": { "name": "Solid" } },
	    "categories": { "arrows": { "name": "Arrows" } } } }
	}`)
	if len(cfg.Issues) != 0 {
		t.Fatalf("issues: %+v", cfg.Issues)
	}
	for _, p := range []string{"arrows/up", "other/x", "root", "a/b/c/d"} {
		if got := cfg.Resolve(p); got != (Resolution{Icon, SourceDefault, ""}) {
			t.Errorf("Resolve(%q) = %+v, want icon/default", p, got)
		}
	}
}

// Top-level items, kinds and categories are not read: PackConfigData::section()
// in ichava/core reads metadata.data.<key> only since core#169, and this
// reader must resolve a pack exactly as core does.
func TestTopLevelKeysAreIgnored(t *testing.T) {
	cfg := mustParse(t, `{
	  "items": {"a/b": {"kind": "illustration"}},
	  "kinds": {"rules": [{"match": "r/**", "kind": "illustration"}]},
	  "categories": {"c": {"kind": "illustration"}}
	}`)
	for _, path := range []string{"a/b", "r/x", "c/x"} {
		if got := cfg.Resolve(path); got.Source != SourceDefault {
			t.Errorf("Resolve(%q) = %+v, want the default: top-level keys are not read", path, got)
		}
	}

	// An empty documented section does not fall through to the top level.
	cfg = mustParse(t, `{"items": {"a/b": {"kind": "illustration"}}, "metadata": {"data": {"items": {}}}}`)
	if got := cfg.Resolve("a/b"); got.Source != SourceDefault {
		t.Errorf("an empty documented section fell back to the top level: %+v", got)
	}
}

func TestDocumentedShapeIsRead(t *testing.T) {
	cfg := mustParse(t, `{"metadata": {"data": {
	  "items": {"c/d": {"kind": "illustration"}},
	  "kinds": {"rules": [{"match": "r/**", "kind": "illustration"}]},
	  "categories": {"k": {"kind": "illustration"}}
	}}}`)
	for path, want := range map[string]Source{"c/d": SourceItem, "r/x": SourceRule, "k/x": SourceCategory} {
		if got := cfg.Resolve(path); got.Kind != Illustration || got.Source != want {
			t.Errorf("Resolve(%q) = %+v, want illustration from %s", path, got, want)
		}
	}
}

// Invalid entries are dropped, resolution falls through to the next row, and
// the problem is reported with its location.
func TestInvalidEntriesFallThroughAndAreReported(t *testing.T) {
	cfg := mustParse(t, `{
	  "package": {"kind": "illustration"},
	  "metadata": {"data": {
	    "items": {"a/x": {"kind": "banana"}, "a/y": "illustration", "a/z": {"kind": "ILLUSTRATION "}},
	    "kinds": {"rules": [
	      {"match": "[ab]/x", "kind": "icon"},
	      {"match": "a/*", "kind": "mystery"},
	      {"kind": "icon"},
	      {"match": "a/*"},
	      "not-an-object"
	    ]},
	    "categories": {"a": {"kind": 7}}
	  }}
	}`)

	// a/x: bad item kind, no valid rule, bad category kind -> the pack's kind.
	if got := cfg.Resolve("a/x"); got != (Resolution{Illustration, SourcePack, ""}) {
		t.Errorf("Resolve(a/x) = %+v", got)
	}
	// Case and whitespace are forgiven, as IconKind::fromConfig does.
	if got := cfg.Resolve("a/z"); got != (Resolution{Illustration, SourceItem, ""}) {
		t.Errorf("Resolve(a/z) = %+v", got)
	}

	codes := map[string]int{}
	for _, is := range cfg.Issues {
		codes[is.Code]++
		if is.Where == "" || is.Detail == "" {
			t.Errorf("issue without a location or detail: %+v", is)
		}
	}
	if codes["kind.invalid_glob"] != 1 {
		t.Errorf("invalid_glob = %d, want 1 (%+v)", codes["kind.invalid_glob"], cfg.Issues)
	}
	// items a/x (banana), items a/y (not an object), rule 1 (mystery), rule 2
	// (no match), rule 3 (no kind), rule 4 (not an object), category a (7).
	if codes["kind.invalid_value"] != 7 {
		t.Errorf("invalid_value = %d, want 7 (%+v)", codes["kind.invalid_value"], cfg.Issues)
	}
}

func TestParseKind(t *testing.T) {
	for in, want := range map[string]Kind{"icon": Icon, "Illustration": Illustration, "  ICON\n": Icon} {
		if got, ok := ParseKind(in); !ok || got != want {
			t.Errorf("ParseKind(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []any{"", "mixed", "icons", nil, 3, true, []any{"icon"}} {
		if got, ok := ParseKind(in); ok {
			t.Errorf("ParseKind(%v) = %q, want none", in, got)
		}
	}
}

func TestCategoryOf(t *testing.T) {
	for in, want := range map[string]string{
		"illustrations/office":   "illustrations",
		"Test Icons/x":           "test-icons",
		"fontawesome/solid/icon": "fontawesome",
		"root":                   "",
		"My_Category--2/x":       "my-category-2",
	} {
		if got := CategoryOf(in); got != want {
			t.Errorf("CategoryOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestThemeSuffixes(t *testing.T) {
	cfg := mustParse(t, mixedPack)
	if got := cfg.ThemeSuffixes("illustrations/office"); len(got) != 1 || got[0] != "-dark" {
		t.Errorf("ThemeSuffixes = %v", got)
	}
	if got := cfg.ThemeSuffixes("glyphs/home"); len(got) != 0 {
		t.Errorf("ThemeSuffixes for a category with none = %v", got)
	}
}

func TestParseConfigRejectsNonJSON(t *testing.T) {
	if _, err := ParseConfig([]byte(`{not json`)); err == nil {
		t.Fatal("garbage parsed")
	}
	if _, err := ParseConfig([]byte(`[1,2]`)); err == nil {
		t.Fatal("a non-object document parsed")
	}
}
