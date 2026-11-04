package svg

import (
	"strings"
	"testing"
)

// OpenMoji's Cornwall flag (1F3F4-E0063-E0061-E0071-E0063-E007F) carries
// xmlns:sodipodi on <svg> with the sodipodi:docname that used it. The policy
// strips the attribute, which left the declaration behind with nothing using
// it, and js-icon-components' generator refuses any attribute it does not
// allow, xmlns:sodipodi included. A declaration nothing uses is dropped now;
// one that something uses stays.

func TestAnUnusedNamespaceDeclarationIsDropped(t *testing.T) {
	doc := `<svg id="emoji" viewBox="0 0 72 72" xmlns="http://www.w3.org/2000/svg" xmlns:sodipodi="http://sodipodi.sourceforge.net/DTD/sodipodi-0.dtd" sodipodi:docname="x.svg"><path d="M0 0"/></svg>`
	out, removed, err := SanitiseBytes([]byte(doc), false)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Contains(got, "sodipodi") {
		t.Errorf("the unused sodipodi declaration survived:\n%s", got)
	}
	if !strings.Contains(got, `xmlns="http://www.w3.org/2000/svg"`) {
		t.Errorf("the default namespace was dropped:\n%s", got)
	}
	if s := SummariseRemoved(removed); s != "docname" {
		t.Errorf("removed = %q; a dropped declaration is tidying, not a policy violation", s)
	}
}

func TestAnUnusedDeclarationWithNoAttributeEverIsDropped(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape"><path d="M0 0"/></svg>`
	out, _, err := SanitiseBytes([]byte(doc), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "inkscape") {
		t.Errorf("the unused inkscape declaration survived:\n%s", out)
	}
}

func TestAUsedNamespaceDeclarationSurvives(t *testing.T) {
	for _, tc := range []struct{ name, doc, want string }{
		{
			"xlink on a use",
			`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><defs><path id="a" d="M0 0"/></defs><use xlink:href="#a"/></svg>`,
			`xmlns:xlink="http://www.w3.org/1999/xlink"`,
		},
		{
			"xlink declared on a child that uses it",
			`<svg xmlns="http://www.w3.org/2000/svg"><g xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="#a"/></g><path id="a" d="M0 0"/></svg>`,
			`<g xmlns:xlink="http://www.w3.org/1999/xlink">`,
		},
		{
			"svg as a prefix on an element",
			`<s:svg xmlns:s="http://www.w3.org/2000/svg"><s:path d="M0 0"/></s:svg>`,
			`xmlns:s="http://www.w3.org/2000/svg"`,
		},
	} {
		out, _, err := SanitiseBytes([]byte(tc.doc), false)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(string(out), tc.want) {
			t.Errorf("%s: %s was dropped:\n%s", tc.name, tc.want, out)
		}
	}
}

func TestXlinkIsDroppedOnceNothingUsesIt(t *testing.T) {
	doc := `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"><use xlink:href="https://e/x#a"/></svg>`
	out, _, err := SanitiseBytes([]byte(doc), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "xlink") {
		t.Errorf("xmlns:xlink survived after the only xlink:href was stripped:\n%s", out)
	}
}
