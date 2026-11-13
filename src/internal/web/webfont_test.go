package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/webvendor"
)

// Icons render from the Tabler Icons webfont (the owner's choice,
// 2026-11-08): an <i class="icon ti ti-NAME"> beside its text.
func TestIconsRenderFromTheWebfont(t *testing.T) {
	if got := string(iconHTML("check")); got != `<i class="icon ti ti-check" aria-hidden="true"></i>` {
		t.Errorf("iconHTML = %s", got)
	}
	if got := string(iconClassHTML("check", "icon-lg text-danger")); got != `<i class="icon ti ti-check icon-lg text-danger" aria-hidden="true"></i>` {
		t.Errorf("iconClassHTML = %s", got)
	}
	if got := string(iconHTML(`x" onclick="`)); got != "" {
		t.Errorf("an unsafe name rendered: %s", got)
	}
}

// The page links the cut CSS, the CSS points at the fingerprinted font, and
// the font is served as font/woff2. A font URL left relative would resolve
// under the CSS's own fingerprint and 404, drawing every icon as nothing.
func TestTheWebfontIsLinkedAndServed(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	body := c.get("/").Body.String()
	m := regexp.MustCompile(`<link rel="stylesheet" href="(/assets/[0-9a-f]+/vendor/tabler-icons/tabler-icons\.css)">`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("the page does not link the webfont CSS")
	}
	css := c.get(m[1])
	if css.Code != 200 {
		t.Fatalf("GET %s = %d", m[1], css.Code)
	}
	f := regexp.MustCompile(`url\("(/assets/[0-9a-f]+/vendor/tabler-icons/tabler-icons\.woff2)"\)`).FindStringSubmatch(css.Body.String())
	if f == nil {
		t.Fatalf("the webfont CSS does not point at the fingerprinted font:\n%.300s", css.Body.String())
	}
	font := c.get(f[1])
	if font.Code != 200 || font.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("GET %s = %d %q", f[1], font.Code, font.Header().Get("Content-Type"))
	}
	if strings.Contains(body, `<symbol id="tabler-`) {
		t.Error("the page still inlines the SVG sprite")
	}
}

// Every icon a template uses has a rule in the cut webfont CSS.
func TestEveryIconUsedIsInTheWebfont(t *testing.T) {
	css := string(vendorFile(t, "tabler-icons/tabler-icons.css"))
	listed := webvendor.ReadIconList(vendorFile(t, "icons.txt"))
	for _, n := range listed {
		if !strings.Contains(css, ".ti-"+n+":before") {
			t.Errorf("icons.txt lists %s but the webfont CSS lacks it", n)
		}
	}
	if len(listed) < 20 {
		t.Fatalf("icons.txt lists %d icons, want at least 20", len(listed))
	}
}
