package kinds

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Kind is what an item is. The values are the ones core's IconKind enum and
// js-ui-kernel's IconKind union use.
type Kind string

// The two kinds. There is no "mixed": a pack is mixed when its items resolve
// to more than one kind, and that is derived, never declared.
const (
	Icon         Kind = "icon"
	Illustration Kind = "illustration"
)

// ParseKind reads a config value as a kind, forgiving case and surrounding
// whitespace and nothing else, as IconKind::fromConfig does. A value that names
// no kind returns false so resolution falls through to the next row.
func ParseKind(v any) (Kind, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	switch k := Kind(strings.ToLower(strings.TrimSpace(s))); k {
	case Icon, Illustration:
		return k, true
	}
	return "", false
}

// Source says which D6 row decided a kind. A pack can set rows 2 to 6; row 1
// (the host's kinds.overrides) belongs to the Laravel app and never applies to
// a sync run, so it has no value here.
type Source string

const (
	SourceItem     Source = "item"
	SourceRule     Source = "rule"
	SourceCategory Source = "category"
	SourcePack     Source = "pack"
	SourceDefault  Source = "default"
)

// Resolution is the outcome for one item.
type Resolution struct {
	Kind   Kind
	Source Source
	// Rule is the matching rule's pattern (SourceRule) or the category slug
	// (SourceCategory), and empty otherwise.
	Rule string
}

// Issue is something wrong in a pack's kind keys. Codes match core's doctor
// check ids, so one vocabulary covers both tools.
type Issue struct {
	Code   string `json:"code"`   // kind.invalid_value or kind.invalid_glob
	Where  string `json:"where"`  // the key path
	Detail string `json:"detail"` // what is wrong
}

// Issue codes.
const (
	IssueInvalidValue = "kind.invalid_value"
	IssueInvalidGlob  = "kind.invalid_glob"
)

type rule struct {
	glob    *Glob
	pattern string
	kind    Kind
}

type category struct {
	kind    Kind
	hasKind bool
	// themes is theme -> filename suffix, before ".svg".
	themes map[string]string
}

// Config is the kind-relevant part of a pack's resources/assets/svg/config.json.
type Config struct {
	packKind    Kind
	hasPackKind bool
	items       map[string]Kind
	rules       []rule
	categories  map[string]category

	// Issues lists every entry that was dropped, in document order where the
	// document has one.
	Issues []Issue
}

// ParseConfig reads a pack's config.json.
//
// It mirrors core's PackConfigData: the documented shape is
// metadata.data.<section>, the top-level key is a fallback, and the documented
// section wins whenever it holds anything. An entry that names no valid kind or
// glob is dropped and recorded in Issues rather than failing the pack.
func ParseConfig(data []byte) (*Config, error) {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("pack config: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("pack config: not a JSON object")
	}

	cfg := &Config{
		items:      map[string]Kind{},
		categories: map[string]category{},
	}

	if pkg, ok := doc["package"].(map[string]any); ok {
		if raw, present := pkg["kind"]; present {
			if k, ok := ParseKind(raw); ok {
				cfg.packKind, cfg.hasPackKind = k, true
			} else {
				cfg.invalid("package.kind", raw)
			}
		}
	}

	for _, slug := range sortedKeys(section(doc, "categories")) {
		cat, ok := section(doc, "categories")[slug].(map[string]any)
		if !ok {
			continue
		}
		c := category{}
		if raw, present := cat["kind"]; present {
			if k, ok := ParseKind(raw); ok {
				c.kind, c.hasKind = k, true
			} else {
				cfg.invalid("categories."+slug+".kind", raw)
			}
		}
		if themes, ok := cat["theme_variants"].(map[string]any); ok {
			for theme, suffix := range themes {
				if s, ok := suffix.(string); ok && theme != "" && s != "" {
					if c.themes == nil {
						c.themes = map[string]string{}
					}
					c.themes[theme] = s
				}
			}
		}
		cfg.categories[slug] = c
	}

	itemsSection := section(doc, "items")
	for _, path := range sortedKeys(itemsSection) {
		entry, ok := itemsSection[path].(map[string]any)
		if !ok {
			cfg.add(IssueInvalidValue, fmt.Sprintf("items[%q]", path), "an item must be an object with a kind")
			continue
		}
		if k, ok := ParseKind(entry["kind"]); ok {
			cfg.items[path] = k
		} else {
			cfg.invalid(fmt.Sprintf("items[%q].kind", path), entry["kind"])
		}
	}

	if rules, ok := section(doc, "kinds")["rules"].([]any); ok {
		for i, raw := range rules {
			where := fmt.Sprintf("kinds.rules[%d]", i)
			entry, ok := raw.(map[string]any)
			if !ok {
				cfg.add(IssueInvalidValue, where, "a rule must be an object with match and kind")
				continue
			}
			pattern, ok := entry["match"].(string)
			if !ok {
				cfg.invalid(where+".match", entry["match"])
				continue
			}
			kind, kindOK := ParseKind(entry["kind"])
			if !kindOK {
				cfg.invalid(where+".kind", entry["kind"])
				continue
			}
			g, err := ParseGlob(pattern)
			if err != nil {
				cfg.add(IssueInvalidGlob, where+".match", err.Error())
				continue
			}
			cfg.rules = append(cfg.rules, rule{glob: g, pattern: pattern, kind: kind})
		}
	}

	return cfg, nil
}

// Resolve decides one item's kind. The first row that yields a kind wins:
// item, then the first matching rule, then category, then the pack, then icon.
// It reads nothing but the config, so it is the same answer at any time.
func (c *Config) Resolve(itemPath string) Resolution {
	if k, ok := c.items[itemPath]; ok {
		return Resolution{Kind: k, Source: SourceItem}
	}
	for _, r := range c.rules {
		if r.glob.Match(itemPath) {
			return Resolution{Kind: r.kind, Source: SourceRule, Rule: r.pattern}
		}
	}
	if slug := CategoryOf(itemPath); slug != "" {
		if cat, ok := c.categories[slug]; ok && cat.hasKind {
			return Resolution{Kind: cat.kind, Source: SourceCategory, Rule: slug}
		}
	}
	if c.hasPackKind {
		return Resolution{Kind: c.packKind, Source: SourcePack}
	}
	return Resolution{Kind: Icon, Source: SourceDefault}
}

// ThemeSuffixes returns the filename suffixes (before ".svg") of the theme
// twins the item's category declares, in theme-name order. A file whose stem
// ends in one of them, beside a light sibling, is a twin and not an item.
func (c *Config) ThemeSuffixes(itemPath string) []string {
	cat, ok := c.categories[CategoryOf(itemPath)]
	if !ok {
		return nil
	}
	themes := make([]string, 0, len(cat.themes))
	for t := range cat.themes {
		themes = append(themes, t)
	}
	sort.Strings(themes)
	out := make([]string, 0, len(themes))
	for _, t := range themes {
		out = append(out, cat.themes[t])
	}
	return out
}

// CategoryOf is the category an item files under: its first folder, slugged,
// or "" for a file at the pack's root. This is IconClassifier::categoryOf for
// an item path. The slug is Str::slug's for ASCII (lower case, runs of anything
// else become one "-"); non-ASCII letters are not transliterated.
func CategoryOf(itemPath string) string {
	folder, _, found := strings.Cut(itemPath, "/")
	if !found {
		return ""
	}
	return slug(folder)
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		} else {
			dash = true
		}
	}
	return b.String()
}

// section is PackConfigData::section(): metadata.data.<key> when it is an
// object, else nothing. A top-level <key> is not read, as in core since
// core#169.
func section(doc map[string]any, key string) map[string]any {
	if md, ok := doc["metadata"].(map[string]any); ok {
		if data, ok := md["data"].(map[string]any); ok {
			if documented, ok := data[key].(map[string]any); ok {
				return documented
			}
		}
	}
	return nil
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *Config) add(code, where, detail string) {
	c.Issues = append(c.Issues, Issue{Code: code, Where: where, Detail: detail})
}

func (c *Config) invalid(where string, v any) {
	c.add(IssueInvalidValue, where, fmt.Sprintf("%v names no kind (want icon or illustration)", v))
}
