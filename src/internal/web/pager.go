package web

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// The page sizes a list's own menu offers, besides the page_size setting.
var pageSizes = []int{10, 25, 50, 100, 200}

// Query parameters a pager reads. A page holding two lists would give the
// second its own pair; every list today is alone on its page.
const (
	pageParam = "page"
	sizeParam = "per_page"
)

// pager is one page of a list: which rows to show, and the links to the
// others. It is built from the request, so a page and a size survive a
// reload and can be linked to.
type pager struct {
	Page, Pages, Size, Total int
	// From and To are the 1-based rows shown, for "21–40 of 133".
	From, To int
	// Sizes is the page-size menu: pageSizes plus the configured size.
	Sizes []int
	// Default is the configured page_size.
	Default int
	// Label names the list, for the navigation's accessible name ("Runs").
	Label string
	// Param and SizeParam name the query parameters, for the size form.
	Param, SizeParam string
	path             string
	query            url.Values
}

// newPager reads ?page= and ?per_page= and clamps them to what exists. Bad
// values do not fail the page: a page that is not a number, or below 1,
// is the first; one past the end is the last; a size not in the menu is the
// configured one. The URL is someone's bookmark, not a form to refuse.
func newPager(r *http.Request, label string, total, def int) pager {
	if def < 1 {
		def = 25
	}
	p := pager{Label: label, Total: total, Default: def, Param: pageParam, SizeParam: sizeParam, path: r.URL.Path, query: r.URL.Query()}
	p.Sizes = append([]int{}, pageSizes...)
	if !slices.Contains(p.Sizes, def) {
		p.Sizes = append(p.Sizes, def)
		slices.Sort(p.Sizes)
	}
	p.Size = def
	if n, err := strconv.Atoi(strings.TrimSpace(p.query.Get(sizeParam))); err == nil && slices.Contains(p.Sizes, n) {
		p.Size = n
	}
	p.Pages = (total + p.Size - 1) / p.Size
	if p.Pages < 1 {
		p.Pages = 1
	}
	p.Page = 1
	if n, err := strconv.Atoi(strings.TrimSpace(p.query.Get(pageParam))); err == nil && n > 1 {
		p.Page = min(n, p.Pages)
	}
	if total > 0 {
		p.From = (p.Page-1)*p.Size + 1
		p.To = min(p.Page*p.Size, total)
	}
	return p
}

// bounds are the slice indexes of this page's rows.
func (p pager) bounds() (int, int) {
	if p.Total == 0 {
		return 0, 0
	}
	return p.From - 1, p.To
}

// paginate returns this page's rows of list.
func paginate[T any](list []T, p pager) []T {
	lo, hi := p.bounds()
	if hi > len(list) {
		hi = len(list)
	}
	if lo > hi {
		lo = hi
	}
	return list[lo:hi]
}

// URL links to page n, keeping every other query parameter (a filter, the
// size).
func (p pager) URL(n int) string {
	q := url.Values{}
	for k, v := range p.query {
		q[k] = v
	}
	if n <= 1 {
		q.Del(pageParam)
	} else {
		q.Set(pageParam, strconv.Itoa(n))
	}
	if s := q.Encode(); s != "" {
		return p.path + "?" + s
	}
	return p.path
}

// Hidden are the query parameters the size form carries through: every one
// but the page (a new size starts at page 1) and the size itself.
func (p pager) Hidden() []queryPair {
	var out []queryPair
	keys := make([]string, 0, len(p.query))
	for k := range p.query {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k == pageParam || k == sizeParam || k == "token" || k == "theme" {
			continue
		}
		for _, v := range p.query[k] {
			out = append(out, queryPair{k, v})
		}
	}
	return out
}

type queryPair struct{ Key, Value string }

// Many reports whether there is more than one page, or a size other than the
// configured one is in force: when the menu is worth showing.
func (p pager) Many() bool { return p.Total > p.Sizes[0] }

// Window is the page numbers to link, with 0 for a gap: the first, the last
// and two either side of the current one.
func (p pager) Window() []int {
	var out []int
	last := 0
	for n := 1; n <= p.Pages; n++ {
		if n == 1 || n == p.Pages || (n >= p.Page-2 && n <= p.Page+2) {
			if last != 0 && n != last+1 {
				out = append(out, 0)
			}
			out = append(out, n)
			last = n
		}
	}
	return out
}
