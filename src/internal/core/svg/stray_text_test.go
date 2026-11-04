package svg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenMoji 15.1.0's 1F684.svg (high-speed train) carries a stray "d" line
// directly under <g id="color">, which renders as a glyph. It was removed by
// hand in icon-sets-openmoji#4; the sanitiser now drops it, so the next sync
// does not bring it back. testdata/openmoji-1F684-stray-text.svg is the first
// lines of the upstream file, reduced to one path.

func TestStrayTextUnderAGroupIsDropped(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "openmoji-1F684-stray-text.svg"))
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := SanitiseBytes(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	// Exactly what icon-sets-openmoji#4 did by hand: the line goes, the
	// indentation before the next element stays.
	want := "<svg xmlns=\"http://www.w3.org/2000/svg\" id=\"emoji\" viewBox=\"0 0 72 72\">\n  <g id=\"color\">\n    " +
		"<path fill=\"#fff\" d=\"m68 48-61.78-0.5026-1.131-3.958 31.76-21.02 31.15-0.5784z\"/>\n  </g>\n</svg>"
	if string(out) != want {
		t.Errorf("got:\n%q\nwant:\n%q", out, want)
	}
	if StrayTextCount(removed) != 1 {
		t.Errorf("removed = %v, want one %s", removed, StrayText)
	}
}

func TestStrayTextRules(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		survive   []string
		die       []string
		stray     int
	}{
		{"text directly under svg",
			`<svg xmlns="http://www.w3.org/2000/svg">oops<path d="M0 0"/></svg>`,
			[]string{`<path d="M0 0"/>`}, []string{"oops"}, 1},
		{"CDATA directly under g",
			`<svg xmlns="http://www.w3.org/2000/svg"><g><![CDATA[junk]]><path d="M0 0"/></g></svg>`,
			[]string{`<path d="M0 0"/>`}, []string{"junk"}, 1},
		{"two stray runs in one file",
			`<svg xmlns="http://www.w3.org/2000/svg">a<g>b</g></svg>`,
			nil, []string{">a<", ">b<"}, 2},
		{"whitespace is not stray",
			"<svg xmlns=\"http://www.w3.org/2000/svg\">\n  <g>\n    <path d=\"M0 0\"/>\n  </g>\n</svg>",
			[]string{"\n  <g>\n    <path"}, nil, 0},
		{"title, desc, text and tspan keep their text",
			`<svg xmlns="http://www.w3.org/2000/svg"><title>Train</title><desc>A train</desc><g><text>Hi<tspan>there</tspan></text></g></svg>`,
			[]string{"<title>Train</title>", "<desc>A train</desc>", "Hi", "there"}, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, removed, err := SanitiseBytes([]byte(tc.doc), false)
			if err != nil {
				t.Fatal(err)
			}
			got := string(out)
			for _, s := range tc.survive {
				if !strings.Contains(got, s) {
					t.Errorf("%q should have survived:\n%s", s, got)
				}
			}
			for _, s := range tc.die {
				if strings.Contains(got, s) {
					t.Errorf("%q should have been removed:\n%s", s, got)
				}
			}
			if n := StrayTextCount(removed); n != tc.stray {
				t.Errorf("stray text count = %d, want %d (removed %v)", n, tc.stray, removed)
			}
		})
	}
}
