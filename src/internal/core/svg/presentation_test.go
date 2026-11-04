package svg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A dry run of OpenMoji 17 showed 13 subdivision flags losing display="none",
// so the hidden construction shapes in them would render, and a few files
// losing color and marker-end. Those attributes are harmless presentation and
// now survive, each behind a value guard. testdata/openmoji-subdivision-flag.svg
// is a reduced copy of the shape those flags take (England's, 1F3F4-E0067-...),
// with the Inkscape sodipodi:docname that stays stripped.

func TestOpenMojiSubdivisionFlagKeepsItsHiddenShapes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "openmoji-subdivision-flag.svg"))
	if err != nil {
		t.Fatal(err)
	}
	out, removed, err := SanitiseBytes(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{`display="none"`, `color="#000"`, `marker-end="url(#arrow)"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s was stripped:\n%s", want, got)
		}
	}
	if strings.Contains(got, "docname") {
		t.Errorf("sodipodi:docname is Inkscape metadata and must stay stripped:\n%s", got)
	}
	if s := SummariseRemoved(removed); s != "docname" {
		t.Errorf("removed = %q, want only docname", s)
	}
}

func TestPresentationAttributeValueGuards(t *testing.T) {
	p := MustLoad()
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"display", "none", true},
		{"display", "inline", true},
		{"display", "block", true},
		{"display", " None ", true},
		{"display", "inline flow-root", true},
		{"display", "url(x)", false},
		{"display", "url(#a)", false},
		{"display", `\75rl(x)`, false},
		{"display", "var(--d)", false},
		{"display", "none;fill:url(https://e)", false},
		{"display", "", false},

		{"color", "#000", true},
		{"color", "currentColor", true},
		{"color", "rgb(0 0 0)", true},
		{"color", "url(https://e/x)", false},
		{"color", `image-set("https://e/x" 1x)`, false},

		{"marker-start", "none", true},
		{"marker-mid", "url(#m)", true},
		{"marker-end", "url(#arrow)", true},
		{"marker-end", `url("#arrow")`, true},
		{"marker-end", "url(https://e)", false},
		{"marker-end", `\75rl(https://e)`, false},
		{"marker-end", "#arrow", false},
		{"marker-end", "inherit", false},
		{"marker-end", "url(#a) url(#b)", false},

		{"docname", "x.svg", false},
		{"sodipodi:docname", "x.svg", false},
	} {
		if got := p.AttributeAllowed(tc.name, tc.value, false); got != tc.want {
			t.Errorf("AttributeAllowed(%q, %q) = %v, want %v", tc.name, tc.value, got, tc.want)
		}
	}
}

func TestHostilePresentationValuesAreStrippedEndToEnd(t *testing.T) {
	for _, doc := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"><path display="url(x)" d="M0 0"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path style="display:url(x)" d="M0 0"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path marker-end="url(https://e)" d="M0 0"/></svg>`,
		`<svg xmlns="http://www.w3.org/2000/svg"><path color="url(https://e)" d="M0 0"/></svg>`,
	} {
		out, removed, err := SanitiseBytes([]byte(doc), false)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(out), "url(") || len(removed) == 0 {
			t.Errorf("hostile value survived or went unreported (%v):\n%s", removed, out)
		}
		if !strings.Contains(string(out), `d="M0 0"`) {
			t.Errorf("path data was collateral damage:\n%s", out)
		}
	}
}
