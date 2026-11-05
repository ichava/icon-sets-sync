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
