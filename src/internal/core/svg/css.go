package svg

import (
	"strings"
	"unicode/utf8"
)

// CSS value guard.
//
// A CSS value can fetch a resource through far more than url(): CSS escapes
// spell any function name, comments split it, and image-set(), src(), image(),
// cross-fade() and element() take a string with no url( anywhere. A deny-list
// of those spellings is a list of what someone already thought of, so this is
// an allow-list over a normalised value instead:
//
//  1. normalise: strip comments, decode escapes (CSS Syntax 3, 4.3.7),
//     lowercase;
//  2. url( is allowed only with a same-document #fragment, quotes matched;
//  3. every other function must be on cssFunctionAllowList;
//  4. any at-rule drops the value.
//
// Whatever the scanner cannot parse with confidence is refused. Dropping a
// declaration costs one paint; guessing costs a request to a stranger's server.
//
// The PHP and TypeScript sanitisers carry the same algorithm and the same probe
// list (css_test.go). Keep the three identical.

// cssFunctionAllowList is every CSS function a value may use besides url().
var cssFunctionAllowList = toSet([]string{
	// colour
	"rgb", "rgba", "hsl", "hsla", "hwb", "lab", "lch", "oklab", "oklch",
	"color", "color-mix",
	// substitution and math
	"var", "calc", "min", "max", "clamp",
	// transforms
	"translate", "translatex", "translatey", "translatez", "translate3d",
	"scale", "scalex", "scaley", "scalez", "scale3d",
	"rotate", "rotatex", "rotatey", "rotatez", "rotate3d",
	"skew", "skewx", "skewy", "matrix", "matrix3d", "perspective",
	// easing
	"cubic-bezier", "steps",
}, false)

// cssLegacySinks are constructs that execute or bind rather than fetch. Most
// are caught by the function and at-rule rules; these are the property names.
var cssLegacySinks = []string{"behavior:", "-moz-binding", "expression("}

// CSSValueIsSafe reports whether a CSS value (a style attribute, the text of a
// style element, or a presentation attribute that takes a paint, url or filter)
// can be kept without allowing a fetch or script.
func CSSValueIsSafe(value string) bool {
	norm := normaliseCSS(value)

	for _, sink := range cssLegacySinks {
		if strings.Contains(norm, sink) {
			return false
		}
	}
	return scanCSS(norm)
}

// normaliseCSS strips comments, decodes escapes and lowercases.
//
// Comments are removed without leaving a space, so u/**/rl( reads url(. A
// browser would not join them, but refusing too much is the safe direction.
func normaliseCSS(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return b.String() // unterminated: the rest is comment
			}
			i += 2 + end + 2

		case s[i] == '\\':
			r, n := decodeCSSEscape(s[i:])
			if r >= 0 {
				b.WriteRune(r)
			}
			i += n

		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			b.WriteRune(r)
			i += n
		}
	}
	return strings.ToLower(b.String())
}

// decodeCSSEscape decodes the escape at the start of s, which begins with a
// backslash. It returns the rune (-1 for none) and the bytes consumed.
func decodeCSSEscape(s string) (rune, int) {
	if len(s) == 1 {
		return 0xFFFD, 1 // backslash at end of input
	}

	c := s[1]
	if c == '\n' || c == '\r' || c == '\f' {
		// Line continuation: nothing is produced. CRLF is one newline.
		if c == '\r' && len(s) > 2 && s[2] == '\n' {
			return -1, 3
		}
		return -1, 2
	}

	if !isHex(c) {
		r, n := utf8.DecodeRuneInString(s[1:])
		return r, 1 + n
	}

	end := 1
	for end < len(s) && end < 7 && isHex(s[end]) {
		end++
	}
	var v rune
	for _, h := range []byte(s[1:end]) {
		v = v<<4 | rune(hexVal(h))
	}

	// One optional whitespace after a hex escape belongs to the escape.
	if end < len(s) {
		switch s[end] {
		case ' ', '\t', '\n', '\f':
			end++
		case '\r':
			end++
			if end < len(s) && s[end] == '\n' {
				end++
			}
		}
	}

	if v == 0 || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
		v = 0xFFFD
	}
	return v, end
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	}
	return int(c-'A') + 10
}

// scanCSS walks a normalised value. Strings are skipped (their contents are
// inert), and every function and at-rule outside them is checked.
func scanCSS(s string) bool {
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '"', '\'':
			end := cssStringEnd(s, i)
			if end < 0 {
				return false
			}
			i = end

		case '@':
			return false

		case '(':
			name := cssIdentBefore(s, i)
			i++
			if name == "" {
				continue // a bare group, as in calc((1px))
			}
			if name == "url" {
				next, ok := scanCSSURL(s, i)
				if !ok {
					return false
				}
				i = next
				continue
			}
			if !cssFunctionAllowList[name] {
				return false
			}

		default:
			i++
		}
	}
	return true
}

// cssStringEnd returns the index just past the string opening at s[start], or
// -1 when it never closes. Only the matching quote closes it, so "a'b" is one
// string. Escapes are already decoded, so none is honoured here: an escaped
// quote has become a plain quote, which can only make this stricter.
func cssStringEnd(s string, start int) int {
	end := strings.IndexByte(s[start+1:], s[start])
	if end < 0 {
		return -1
	}
	return start + 1 + end + 1
}

// cssIdentBefore is the identifier ending at s[i], which holds the open paren.
func cssIdentBefore(s string, i int) string {
	j := i
	for j > 0 && isCSSIdentByte(s[j-1]) {
		j--
	}
	return s[j:i]
}

func isCSSIdentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c >= 0x80
}

// scanCSSURL checks the argument of a url( whose open paren was just consumed
// (i is the index after it) and returns the index after the closing paren.
// The only argument allowed is a same-document fragment.
func scanCSSURL(s string, i int) (int, bool) {
	i = skipCSSSpace(s, i)
	if i >= len(s) {
		return 0, false
	}

	var arg string
	if s[i] == '"' || s[i] == '\'' {
		end := cssStringEnd(s, i)
		if end < 0 {
			return 0, false
		}
		arg = s[i+1 : end-1]
		i = end
	} else {
		start := i
		for i < len(s) && s[i] != ')' && !isCSSSpace(s[i]) {
			if s[i] == '"' || s[i] == '\'' || s[i] == '(' {
				return 0, false
			}
			i++
		}
		arg = s[start:i]
	}

	i = skipCSSSpace(s, i)
	if i >= len(s) || s[i] != ')' {
		return 0, false
	}

	if len(arg) < 2 || arg[0] != '#' || strings.ContainsAny(arg, " \t\r\n\f\"'()\\") {
		return 0, false
	}
	return i + 1, true
}

func isCSSSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func skipCSSSpace(s string, i int) int {
	for i < len(s) && isCSSSpace(s[i]) {
		i++
	}
	return i
}
