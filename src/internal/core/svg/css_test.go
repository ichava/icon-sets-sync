package svg

import (
	"strings"
	"testing"
)

// cssProbes is the shared probe list for the CSS value guard. The PHP and
// TypeScript sanitisers carry the same list; keep the three identical.
//
// safe=false means the whole declaration (style) or attribute must be dropped.
var cssProbes = []struct {
	name  string
	value string
	safe  bool
}{
	// Positive controls: these must keep working.
	{"literal colour", "fill:#123456", true},
	{"same-document url", "fill:url(#grad)", true},
	{"same-document url, single quotes", "fill:url('#grad')", true},
	{"same-document url, double quotes", `fill:url("#grad")`, true},
	{"same-document url, spaces", "fill:url(  #grad  ) red", true},
	{"uppercase url", "fill:URL(#grad)", true},
	{"currentColor", "fill:currentColor", true},
	{"var token", "fill:var(--token)", true},
	{"var with fallback", "fill:var(--token, #fff)", true},
	{"rgb", "fill:rgb(1,2,3)", true},
	{"rgba", "fill:rgba(1,2,3,.5)", true},
	{"hsl", "fill:hsl(10 20% 30%)", true},
	{"oklch", "fill:oklch(60% .1 200)", true},
	{"color-mix", "fill:color-mix(in srgb, red 50%, blue)", true},
	{"calc", "stroke-width:calc(1px + 2px)", true},
	{"clamp", "stroke-width:clamp(1px, 2px, 3px)", true},
	{"transform functions", "transform:translate(1px,2px) rotate(45deg) scale(2)", true},
	{"cubic-bezier", "transition-timing-function:cubic-bezier(0,0,1,1)", true},
	{"steps", "animation-timing-function:steps(4, end)", true},
	{"two same-document urls", "fill:url(#a);stroke:url(#b)", true},
	{"url text inside a string", `content:"url(https://e/x)"`, true},
	{"empty", "", true},

	// Off-document url().
	{"https url", "fill:url(https://evil.test/x)", false},
	{"uppercase https url", "FILL:URL(HTTPS://EVIL.TEST/X)", false},
	{"relative url", "fill:url(x.png)", false},
	{"data url", "fill:url(data:image/png;base64,AAAA)", false},
	{"protocol-relative url", "fill:url(//evil.test/x)", false},
	{"second url is remote", "fill:url(#a);background:url(https://e/x)", false},
	{"empty url", "fill:url()", false},
	{"unterminated url", "fill:url(#a", false},
	{"fragment then junk", "fill:url(#a b)", false},

	// CSS escapes spelling url(.
	{"escape \\75rl", `background:\75rl(https://e/x)`, false},
	{"escape \\000075rl", `background:\000075rl(https://e/x)`, false},
	{"escape with trailing space", `background:\75 rl(https://e/x)`, false},
	{"escape uppercase hex", `background:\55rl(https://e/x)`, false},
	{"escape u\\rl", `background:u\rl(https://e/x)`, false},
	{"escape \\72 in rl", `background:u\72 l(https://e/x)`, false},
	{"escape of paren", `background:url\28 https://e/x)`, false},
	{"escape newline continuation", "background:u\\\nrl(https://e/x)", false},
	{"escape of fragment is still checked", `background:\75rl(#ok)`, true},

	// Comments splitting the name.
	{"comment u/**/rl", "background:u/**/rl(https://e/x)", false},
	{"comment inside url", "background:url(/**/https://e/x)", false},
	{"comment after url(", "background:url(/* x */#grad)", true},
	{"unterminated comment hides a url", "fill:red;/* url(https://e/x)", true},

	// Image-valued functions with no url( at all.
	{"image-set", `background-image:image-set("https://e/x" 1x)`, false},
	{"webkit image-set", `background-image:-webkit-image-set("https://e/x" 1x)`, false},
	{"image()", `background-image:image("https://e/x")`, false},
	{"src()", `background-image:src("https://e/x")`, false},
	{"cross-fade()", `background-image:cross-fade(url(#a), url(#b), 50%)`, false},
	{"element()", "background:element(#thing)", false},
	{"image-set escaped", `background-image:\69mage-set("https://e/x" 1x)`, false},
	{"image-set nested in var fallback", `fill:var(--a, image-set("https://e/x" 1x))`, false},
	{"image-set nested in calc", `fill:calc(image-set("https://e/x" 1x))`, false},
	{"unknown function", "fill:frobnicate(1)", false},

	// At-rules and legacy sinks.
	{"@import", `@import "https://e/x.css"`, false},
	{"@import url", "@import url(#x)", false},
	{"@import escaped", `\40import "https://e/x.css"`, false},
	{"expression()", "x:expression(alert(1))", false},
	{"expression() uppercase", "x:EXPRESSION(alert(1))", false},
	{"expression() with comment", "x:expr/**/ession(alert(1))", false},
	{"behavior", "behavior:url(#x)", false},
	{"-moz-binding", "-moz-binding:url(#x)", false},

	// Quote and paren mismatches.
	{"quote mismatch single in double", `fill:url("https://e/x'y")`, false},
	{"quote mismatch double in single", `fill:url('https://e/x"y')`, false},
	{"paren in quoted remote", `fill:url("https://e/a)b")`, false},
	{"unterminated string", `fill:url("#a)`, false},
	{"mismatched quotes", `fill:url("#a')`, false},
	{"quoted fragment with paren", `fill:url("#a)b")`, false},
}

func TestCSSValueGuardProbes(t *testing.T) {
	if len(cssProbes) < 60 {
		t.Fatalf("probe list shrank to %d; it is shared with the PHP and TS sanitisers", len(cssProbes))
	}
	for _, tc := range cssProbes {
		t.Run(tc.name, func(t *testing.T) {
			if got := StyleValueIsSafe(tc.value); got != tc.safe {
				t.Errorf("StyleValueIsSafe(%q) = %v, want %v", tc.value, got, tc.safe)
			}
		})
	}
}

// urlPresentationAttributes are the attributes whose value is a CSS <paint>,
// <url> or <filter>, so a url() in them fetches exactly as one in style does.
var urlPresentationAttributes = []string{
	"fill", "stroke", "filter", "clip-path", "mask",
	"marker-start", "marker-mid", "marker-end",
}

func TestPresentationAttributesGetTheSameGuard(t *testing.T) {
	p := MustLoad()
	for _, name := range urlPresentationAttributes {
		for _, tc := range []struct {
			value string
			want  bool
		}{
			{"url(#grad)", true},
			{"none", true},
			{"url(https://evil.test/x)", false},
			{`\75rl(https://evil.test/x)`, false},
			{`image-set("https://evil.test/x" 1x)`, false},
			{`url("https://e/x'y")`, false},
		} {
			// A name off the allow-list cannot survive at all, so for one the
			// only question is that it never survives with a remote target.
			got := p.AttributeAllowed(name, tc.value, false)
			if tc.want && !p.allowedAttrLower[name] {
				continue
			}
			if got != tc.want {
				t.Errorf("AttributeAllowed(%q, %q) = %v, want %v", name, tc.value, got, tc.want)
			}
		}
	}
}

func TestSanitiseDropsTheBypassesEndToEnd(t *testing.T) {
	for _, doc := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg" style="background:\75rl(https://evil.test/p.png)"/>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path fill="url(https://evil.test/p.png)"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path filter="image-set(&quot;https://evil.test/p.png&quot; 1x)"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path style="background:url(&quot;https://evil.test/x'y&quot;)"/></svg>`,
	} {
		out, _, err := SanitiseBytes([]byte(doc), false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "evil.test") {
			t.Errorf("remote reference survived:\n%s", out)
		}
	}

	good := `<svg xmlns="http://www.w3.org/2000/svg"><linearGradient id="g"><stop stop-color="#fff"/></linearGradient><path fill="url(#g)" style="stroke:url(#g)"/></svg>`
	out, _, err := SanitiseBytes([]byte(good), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `fill="url(#g)"`) || !strings.Contains(string(out), `style="stroke:url(#g)"`) {
		t.Errorf("url(#g) was dropped:\n%s", out)
	}
}
