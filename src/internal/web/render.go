package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/checker"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// assetSet is the embedded assets with a content hash each, for
// fingerprinted, immutable URLs.
type assetSet struct {
	files  map[string][]byte // path below assets/
	hashes map[string]string // first 12 hex chars of sha256
	sprite template.HTML
}

func loadAssets() (*assetSet, error) {
	a := &assetSet{files: map[string][]byte{}, hashes: map[string]string{}}
	err := fs.WalkDir(assetFS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := assetFS.ReadFile(p)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(p, "assets/")
		sum := sha256.Sum256(body)
		a.files[name] = body
		a.hashes[name] = hex.EncodeToString(sum[:])[:12]
		return nil
	})
	if err != nil {
		return nil, err
	}
	sprite, ok := a.files["vendor/tabler-icons.svg"]
	if !ok {
		return nil, fmt.Errorf("web: the icon sprite is not embedded")
	}
	// The sprite is vendored, hash-checked by a test, and contains no script:
	// it is inlined so <use href="#..."> stays same-document.
	a.sprite = template.HTML(sprite) //nolint:gosec // trusted vendored file, see above
	return a, nil
}

// url returns the fingerprinted URL of an asset. Asking for one that does not
// exist panics at template execution, which the golden tests turn into a
// failure rather than a broken page.
func (a *assetSet) url(name string) (string, error) {
	h, ok := a.hashes[name]
	if !ok {
		return "", fmt.Errorf("no embedded asset %q", name)
	}
	return "/assets/" + h + "/" + name, nil
}

// pageSet is every page template, each parsed once over the shared layout.
type pageSet struct {
	pages map[string]*template.Template
}

// pageNames lists the page templates. A golden test asserts there is one
// golden file per name here, and that this list matches the files.
func pageNames() ([]string, error) {
	entries, err := fs.ReadDir(templateFS, "templates")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || e.Name() == "layout.html" || !strings.HasSuffix(e.Name(), ".html") {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".html"))
	}
	sort.Strings(out)
	return out, nil
}

func funcs(a *assetSet) template.FuncMap {
	return template.FuncMap{
		"asset":  a.url,
		"sprite": func() template.HTML { return a.sprite },
		"icon":   iconHTML,
		"when":   func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") },
		"iso":    func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"ago":    ago,
		"statusClass": func(s checker.Status) string {
			switch s {
			case checker.StatusOK:
				return "bg-green-lt"
			case checker.StatusUpdateAvailable:
				return "bg-yellow-lt"
			}
			return "bg-red-lt"
		},
		"statusText": func(s checker.Status) string {
			switch s {
			case checker.StatusOK:
				return "Up to date"
			case checker.StatusUpdateAvailable:
				return "Update available"
			}
			return "Check failed"
		},
		"orDash": func(s string) string {
			if s == "" {
				return "–"
			}
			return s
		},
		"thousands": thousands,
		"clock":     func(t time.Time) string { return t.UTC().Format("15:04:05") },
		"runClass": func(s any) string {
			switch fmt.Sprint(s) {
			case "succeeded", "synced", "up-to-date":
				return "bg-green-lt"
			case "running":
				return "bg-blue-lt"
			case "cancelled":
				return "bg-secondary-lt"
			}
			return "bg-red-lt"
		},
		"runAlert": func(s jobs.Status) string {
			switch s {
			case jobs.StatusSucceeded:
				return "alert-success"
			case jobs.StatusRunning:
				return "alert-info"
			case jobs.StatusCancelled:
				return "alert-secondary"
			}
			return "alert-danger"
		},
		"runText": func(s jobs.Status) string {
			switch s {
			case jobs.StatusSucceeded:
				return "Finished"
			case jobs.StatusRunning:
				return "Running"
			case jobs.StatusCancelled:
				return "Cancelled"
			}
			return "Failed"
		},
		"stageClass": func(state string) string {
			switch state {
			case jobs.StageFinished:
				return "bg-green-lt"
			case jobs.StageStarted:
				return "bg-blue-lt"
			}
			return "bg-red-lt"
		},
		"outcomes": func() []string {
			return []string{string(jobs.StatusRunning), string(jobs.StatusSucceeded), string(jobs.StatusFailed), string(jobs.StatusCancelled)}
		},
		"navItem": func(v view, key, href, icon, label string) navItem {
			return navItem{Active: v.Nav == key, Href: href, Icon: icon, Label: label}
		},
	}
}

// iconHTML renders a Tabler icon from the inline sprite. Icons are
// decorative here -- every one sits beside text that says the same thing.
func iconHTML(name string) template.HTML {
	if !validIcon(name) {
		return ""
	}
	return template.HTML(`<svg class="icon" aria-hidden="true" focusable="false"><use href="#tabler-` + name + `"></use></svg>`) //nolint:gosec // name is [a-z0-9-] only
}

func validIcon(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t).Round(time.Second)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%d h ago", int(d.Hours()))
	}
}

func thousands(n int) string {
	s := itoa(n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func parsePages(a *assetSet) (*pageSet, error) {
	base, err := template.New("layout.html").Funcs(funcs(a)).ParseFS(templateFS, "templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return nil, err
	}
	names, err := pageNames()
	if err != nil {
		return nil, err
	}
	ps := &pageSet{pages: map[string]*template.Template{}}
	for _, n := range names {
		t, err := base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := t.ParseFS(templateFS, path.Join("templates", n+".html")); err != nil {
			return nil, err
		}
		ps.pages[n] = t
	}
	return ps, nil
}

type navItem struct {
	Active            bool
	Href, Icon, Label string
}

// view is what every page template receives.
type view struct {
	Title    string
	Nav      string
	Authed   bool
	CSRF     string
	Version  string
	ReadOnly bool
	// Reload asks app.js to reload the page after a few seconds, while
	// background work the page is waiting on is running.
	Reload bool
	Data   any
}

func (s *Server) newView(r *http.Request, nav, title string, data any) view {
	v := view{Title: title, Nav: nav, Version: s.opts.Version, ReadOnly: s.opts.ReadOnly, Data: data}
	if sess := sessionFrom(r.Context()); sess != nil {
		v.Authed = true
		v.CSRF = sess.csrf
	}
	return v
}

// render executes a page into a buffer first, so a template error becomes a
// clean 500 instead of half a page.
func (s *Server) render(w http.ResponseWriter, status int, page string, v view) {
	t, ok := s.pages.pages[page]
	if !ok {
		http.Error(w, "no such page template: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

type errorData struct {
	Status         int
	Heading        string
	Message        string
	Link, LinkText string
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	s.render(w, status, "error", s.newView(r, "", heading, errorData{Status: status, Heading: heading, Message: message}))
}

// renderErrorLink is renderError with a way forward, e.g. to the job that
// holds the lock a request wanted.
func (s *Server) renderErrorLink(w http.ResponseWriter, r *http.Request, status int, heading, message, link, text string) {
	s.render(w, status, "error", s.newView(r, "", heading,
		errorData{Status: status, Heading: heading, Message: message, Link: link, LinkText: text}))
}

func (s *Server) renderUnauthorized(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusUnauthorized, "Sign-in link required",
		"Open the address icon-sets-sync serve printed when it started. It carries a one-time token that signs this browser in.")
}

// serveAsset answers /assets/{hash}/{path...}. A stale hash is a 404, not the
// current file: an immutable URL must never change content.
func (s *Server) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	body, ok := s.assets.files[name]
	if !ok || r.PathValue("hash") != s.assets.hashes[name] {
		http.NotFound(w, r)
		return
	}
	switch path.Ext(name) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(body)
}
