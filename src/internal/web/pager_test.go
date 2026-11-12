package web

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestPagerClampsWhatTheURLAsksFor(t *testing.T) {
	cases := []struct {
		query                       string
		total, def                  int
		page, pages, size, from, to int
	}{
		{"", 133, 25, 1, 6, 25, 1, 25},
		{"page=3", 133, 25, 3, 6, 25, 51, 75},
		{"page=6", 133, 25, 6, 6, 25, 126, 133},
		{"page=99", 133, 25, 6, 6, 25, 126, 133}, // past the end: the last page
		{"page=-2", 133, 25, 1, 6, 25, 1, 25},    // below one: the first
		{"page=abc", 133, 25, 1, 6, 25, 1, 25},   // not a number: the first
		{"per_page=100", 133, 25, 1, 2, 100, 1, 100},
		{"per_page=7", 133, 25, 1, 6, 25, 1, 25},      // not in the menu: the setting
		{"per_page=100000", 133, 25, 1, 6, 25, 1, 25}, // nor is a huge one
		{"", 0, 25, 1, 1, 25, 0, 0},
		{"", 10, 40, 1, 1, 40, 1, 10}, // a configured size outside the menu joins it
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/jobs?"+c.query, nil)
		p := newPager(r, "Runs", c.total, c.def)
		got := []int{p.Page, p.Pages, p.Size, p.From, p.To}
		want := []int{c.page, c.pages, c.size, c.from, c.to}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q of %d: page,pages,size,from,to = %v, want %v", c.query, c.total, got, want)
		}
	}
}

func TestPaginateSlicesThePage(t *testing.T) {
	rows := make([]int, 53)
	for i := range rows {
		rows[i] = i + 1
	}
	r := httptest.NewRequest("GET", "/?page=3&per_page=25", nil)
	got := paginate(rows, newPager(r, "Packs", len(rows), 25))
	if len(got) != 3 || got[0] != 51 || got[2] != 53 {
		t.Errorf("page 3 = %v", got)
	}
	if got := paginate([]int{}, newPager(r, "Packs", 0, 25)); len(got) != 0 {
		t.Errorf("empty list paginated to %v", got)
	}
}

func TestPagerLinksKeepTheFilter(t *testing.T) {
	r := httptest.NewRequest("GET", "/jobs?pack=icon-sets-flag&outcome=failed&per_page=10&page=2&token=x", nil)
	p := newPager(r, "Runs", 45, 25)
	if u := p.URL(3); !strings.Contains(u, "pack=icon-sets-flag") || !strings.Contains(u, "per_page=10") || !strings.Contains(u, "page=3") {
		t.Errorf("URL(3) = %s", u)
	}
	if u := p.URL(1); strings.Contains(u, "page=1") && !strings.Contains(u, "per_page") {
		t.Errorf("URL(1) = %s keeps a page=1", u)
	}
	var keys []string
	for _, h := range p.Hidden() {
		keys = append(keys, h.Key)
	}
	if !reflect.DeepEqual(keys, []string{"outcome", "pack"}) {
		t.Errorf("size form carries %v, want the filter only (no page, size or token)", keys)
	}
}

func TestPagerWindow(t *testing.T) {
	r := httptest.NewRequest("GET", "/?page=10&per_page=10", nil)
	got := newPager(r, "Runs", 200, 25).Window()
	want := []int{1, 0, 8, 9, 10, 11, 12, 0, 20}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Window = %v, want %v", got, want)
	}
}

// The packs list shows one page of packs, the pager, and keeps the totals
// counting every pack.
func TestPacksListIsPaged(t *testing.T) {
	s, eng := newTestServer(t)
	base := eng.packs[0]
	eng.packs = nil
	for i := 1; i <= 23; i++ {
		p := *base
		p.Name = fmt.Sprintf("icon-sets-p%02d", i)
		eng.packs = append(eng.packs, &p)
	}
	body := signIn(t, s).get("/?per_page=10&page=2").Body.String()
	for _, want := range []string{`data-role="pager"`, "11–20 of 23", `aria-current="page">2<`, `<div class="text-nowrap"><a href="/packs/icon-sets-p11">`, `<div class="text-nowrap"><a href="/packs/icon-sets-p20">`, `name="per_page"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page 2 lacks %q", want)
		}
	}
	// The command palette lists every pack; the table's row markup is the
	// table's own.
	for _, not := range []string{`<div class="text-nowrap"><a href="/packs/icon-sets-p10">`, `<div class="text-nowrap"><a href="/packs/icon-sets-p21">`} {
		if strings.Contains(body, not) {
			t.Errorf("page 2 shows %q from another page", not)
		}
	}
	if !strings.Contains(body, `<div class="h2 mb-0">23</div>`) {
		t.Error("the Packs total no longer counts every pack")
	}
}
