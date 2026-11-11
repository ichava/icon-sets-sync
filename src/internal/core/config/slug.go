package config

import (
	"fmt"
	"regexp"
	"strings"
)

// SlugPattern is the one shape a pack name may take: lowercase letters and
// digits in words joined by single hyphens. A pack name is a file name, a URL
// path segment, a branch-name part and a key in run history, so it carries no
// dot, underscore, capital or space ("icon-sets-extras-ant-design", never
// "icon-sets-extras.ant-design"). config/schema/pack.schema.json carries the
// same pattern for editors; TestSchemaSlugPatternMatchesIsSlug keeps them one.
const SlugPattern = `^[a-z0-9]+(-[a-z0-9]+)*$`

var slugRE = regexp.MustCompile(SlugPattern)

// SlugRule is the rule in words, for messages.
const SlugRule = "a pack name is a lowercase slug: letters a-z and digits, in words joined by single hyphens"

// IsSlug reports whether s is a pack slug.
func IsSlug(s string) bool { return len(s) <= 100 && slugRE.MatchString(s) }

// checkName refuses a name that is not a slug, suggesting the slug it was
// probably meant to be.
func checkName(where, name string) error {
	if IsSlug(name) {
		return nil
	}
	msg := fmt.Sprintf("%s: pack name %q is not allowed: %s", where, name, SlugRule)
	s := strings.NewReplacer(".", "-", "_", "-", " ", "-").Replace(strings.ToLower(name))
	if IsSlug(s) {
		msg += fmt.Sprintf(" (use %q, and name its file %s.json)", s, s)
	}
	return fmt.Errorf("%s", msg)
}
