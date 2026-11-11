package web

import (
	"regexp"
	"strings"
	"testing"
)

// The owner, 2026-11-08, for every Go web UI: tooltips were buggy, naggy and
// intrusive -- covering the menu they described, two open at once, on items
// that already had a label. A tooltip may sit only on an icon-only control,
// where it stands in for the label, and never beside a native title.
//
// tooltipViolations returns every element in body that carries a tooltip or
// a native title while also showing text, or that is a dropdown toggle with
// either; and every tooltip that carries a native title or lacks an
// aria-label or the agreed options.
func tooltipViolations(body string) (bad []string, inspected int) {
	for _, m := range startTag.FindAllStringSubmatchIndex(body, -1) {
		tag := strings.ToLower(body[m[2]:m[3]])
		if m[4] < 0 {
			continue
		}
		attrs := body[m[4]:m[5]]
		tooltip := strings.Contains(attrs, `data-bs-toggle="tooltip"`)
		title := nativeTitle.MatchString(attrs)
		dropdown := strings.Contains(attrs, `data-bs-toggle="dropdown"`)
		if !tooltip && !title {
			continue
		}
		inspected++
		head := "<" + tag + attrs + ">"
		inner := innerHTML(body, m[1], tag)
		switch {
		case dropdown:
			bad = append(bad, "dropdown toggle with a tooltip or title: "+head)
		case visibleText(inner) != "":
			bad = append(bad, "tooltip or title on visible text "+strings.TrimSpace(visibleText(inner))+": "+head)
		case tooltip && title:
			bad = append(bad, "tooltip beside a native title: "+head)
		case title:
			bad = append(bad, "native title on an icon-only control (use a tooltip): "+head)
		case tooltip && !strings.Contains(attrs, `aria-label="`):
			bad = append(bad, "tooltip without an aria-label: "+head)
		case tooltip && (!strings.Contains(attrs, `data-bs-trigger="hover focus"`) || !strings.Contains(attrs, `data-bs-container="body"`) || !strings.Contains(attrs, `data-bs-delay=`)):
			bad = append(bad, "tooltip without the agreed trigger, delay and container: "+head)
		}
	}
	return bad, inspected
}

var (
	startTag    = regexp.MustCompile(`<([a-zA-Z][a-zA-Z0-9]*)(\s[^<>]*?)?/?>`)
	nativeTitle = regexp.MustCompile(`(^|\s)title="`)
	anyTag      = regexp.MustCompile(`<[^<>]*>`)
	hiddenSpan  = regexp.MustCompile(`<span class="visually-hidden[^"]*">[^<]*</span>`)
)

var voidTags = map[string]bool{"input": true, "img": true, "br": true, "hr": true, "meta": true, "link": true}

// innerHTML returns the content of the element whose start tag ends at from,
// counting nested elements of the same name.
func innerHTML(body string, from int, tag string) string {
	if voidTags[tag] {
		return ""
	}
	depth := 1
	open, closing := "<"+tag, "</"+tag+">"
	for i := from; i < len(body); {
		o := strings.Index(body[i:], open)
		c := strings.Index(body[i:], closing)
		if c < 0 {
			return body[from:]
		}
		if o >= 0 && o < c && len(body) > i+o+len(open) && strings.ContainsRune(" >", rune(body[i+o+len(open)])) {
			depth++
			i += o + len(open)
			continue
		}
		depth--
		if depth == 0 {
			return body[from : i+c]
		}
		i += c + len(closing)
	}
	return ""
}

func visibleText(inner string) string {
	inner = hiddenSpan.ReplaceAllString(inner, "")
	return strings.Join(strings.Fields(anyTag.ReplaceAllString(inner, " ")), " ")
}

func TestNoTooltipOrTitleOnLabelledControls(t *testing.T) {
	pages := goldenPages(t)
	inspected := 0
	for name, body := range pages {
		bad, n := tooltipViolations(body)
		inspected += n
		for _, b := range bad {
			t.Errorf("%s: %s", name, b)
		}
	}
	if inspected < 2 {
		t.Fatalf("inspected %d tooltips and titles across %d pages; the theme toggles alone are two", inspected, len(pages))
	}
	if len(pages) < 12 {
		t.Fatalf("rendered %d pages, want at least 12", len(pages))
	}
}

// The guard can fail: each kind of violation it exists for is caught.
func TestTooltipGuardCatchesItsSamples(t *testing.T) {
	ok := `<a href="?theme=dark" class="nav-link" data-bs-toggle="tooltip" data-bs-trigger="hover focus" data-bs-delay='{"show":500,"hide":0}' data-bs-container="body" aria-label="Dark theme"><i class="icon ti ti-moon" aria-hidden="true"></i></a>`
	if bad, n := tooltipViolations(ok); len(bad) != 0 || n != 1 {
		t.Fatalf("the agreed icon-only tooltip was refused: %v (inspected %d)", bad, n)
	}
	for _, sample := range []string{
		`<a href="/" data-bs-toggle="tooltip" data-bs-trigger="hover focus" data-bs-delay="{}" data-bs-container="body" aria-label="Packs">Packs</a>`,
		`<a href="/" data-bs-toggle="dropdown" title="Recent runs"><i class="icon"></i></a>`,
		`<div class="text-truncate" title="unreachable: GET https://x">unreachable: GET https://x</div>`,
		`<a data-bs-toggle="tooltip" data-bs-trigger="hover focus" data-bs-delay="{}" data-bs-container="body" aria-label="Dark" title="Dark"><i class="icon"></i></a>`,
		`<a data-bs-toggle="tooltip" aria-label="Dark"><i class="icon"></i></a>`,
		`<span title="82d948812bfe3f269ef8f731dcdb07b08160edc4">82d948812bfe</span>`,
	} {
		if bad, _ := tooltipViolations(sample); len(bad) == 0 {
			t.Errorf("the guard passed %s", sample)
		}
	}
	if bad, _ := tooltipViolations(`<form data-confirm-title="Stop the server?"><button>Stop</button></form>`); len(bad) != 0 {
		t.Errorf("data-confirm-title was read as a native title: %v", bad)
	}
}
