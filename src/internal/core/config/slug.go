package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// slugRE is the one shape a pack name may take: lowercase letters and digits
// in words joined by single hyphens. A pack name is a file name, a URL path
// segment, a branch-name part and a key in run history, so it carries no dot,
// underscore, capital or space ("icon-sets-extras-ant-design", never
// "icon-sets-extras.ant-design").
var slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// SlugRule is the rule in words, for messages.
const SlugRule = "a pack name is a lowercase slug: letters a-z and digits, in words joined by single hyphens"

// IsSlug reports whether s is a pack slug.
func IsSlug(s string) bool { return len(s) <= 100 && slugRE.MatchString(s) }

// CanonicalSlug returns the slug a name stands for. deprecated is true when
// name is an old spelling of a slug -- capitals, or words joined by dots,
// underscores or spaces, as the icon-sets-extras.<set> configs were named
// until they became icon-sets-extras-<set> -- and false when name is already
// a slug or has no slug spelling at all (then name is returned unchanged).
//
// Deprecated spellings: accepted, with a warning, until 1.0.0.
func CanonicalSlug(name string) (slug string, deprecated bool) {
	if IsSlug(name) {
		return name, false
	}
	s := strings.ToLower(name)
	s = strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(s)
	if IsSlug(s) {
		return s, true
	}
	return name, false
}

// Deprecated reports a deprecated spelling. A seam for tests; the CLI and the
// server print it on stderr.
var Deprecated = func(msg string) { fmt.Fprintln(os.Stderr, "warning: "+msg) }

// canonicalName is CanonicalSlug for a name read from where: the slug, with a
// deprecation warning for an old spelling, or an error naming the rule.
func canonicalName(where, name string) (string, error) {
	slug, deprecated := CanonicalSlug(name)
	switch {
	case deprecated:
		Deprecated(fmt.Sprintf("%s: pack name %q is deprecated; use %q (%s; the old spelling stops working at 1.0.0)",
			where, name, slug, SlugRule))
		return slug, nil
	case !IsSlug(slug):
		return "", fmt.Errorf("%s: pack name %q is not allowed: %s", where, name, SlugRule)
	}
	return slug, nil
}

// findOldSpelling looks in the search directories for a config file whose
// name is a deprecated spelling of slug (icon-sets-extras.feather.json for
// icon-sets-extras-feather), so a user's own config keeps loading after the
// rename.
func findOldSpelling(configDir, slug string) string {
	for _, dir := range searchDirs(configDir) {
		var hits []string
		for _, ext := range []string{".json", ".yaml", ".yml"} {
			m, _ := filepath.Glob(filepath.Join(dir, "*"+ext))
			hits = append(hits, m...)
		}
		sort.Strings(hits)
		for _, f := range hits {
			base := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
			if got, deprecated := CanonicalSlug(base); deprecated && got == slug {
				Deprecated(fmt.Sprintf("%s: the file name is deprecated; rename it %s%s",
					f, slug, filepath.Ext(f)))
				return f
			}
		}
	}
	return ""
}
