package web

import (
	"net/http"
	"runtime"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/version"
)

// AppName is the product's name as the UI shows it: in the header, the page
// titles and About. The binary, the module and the repository stay
// icon-sets-sync.
const AppName = "Icon Sets Sync"

// Where the product lives, for About, the footer and the server menu.
const (
	repoURL    = "https://github.com/ichava/icon-sets-sync"
	docsURL    = repoURL + "/tree/main/docs"
	issuesURL  = repoURL + "/issues"
	changesURL = repoURL + "/blob/main/CHANGELOG.md"
)

// BuildInfo is what About reports about the running binary.
type BuildInfo struct {
	Version, Commit, Date, Go, Platform string
}

// BuildInfoOf reads the running binary's build: its version (the one the
// server was given, else the module's), commit and date, Go version and
// platform.
func BuildInfoOf(v string) BuildInfo {
	if v == "" {
		v = version.Version()
	}
	return BuildInfo{
		Version: v, Commit: version.Commit(), Date: version.Date(),
		Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
}

type aboutData struct {
	// Tab is the About tab shown: overview, shortcuts or help. Licences is
	// its own page, /about/licenses, under the same tab strip.
	Tab       string
	Build     BuildInfo
	BindAddr  string
	ConfigDir string
	ReadOnly  bool
	Repo      string
	Docs      string
	Issues    string
	Changes   string
}

// aboutTabs are About's tabs, in order. Licences links to its own page.
var aboutTabs = []struct{ Key, Label, Href, Icon string }{
	{"overview", "Overview", "/about", "info-circle"},
	{"shortcuts", "Shortcuts", "/about?tab=shortcuts", "keyboard"},
	{"licences", "Licences", "/about/licenses", "license"},
	{"help", "Help", "/about?tab=help", "book"},
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "licences" {
		http.Redirect(w, r, "/about/licenses", http.StatusSeeOther)
		return
	}
	if tab != "shortcuts" && tab != "help" {
		tab = "overview"
	}
	d := aboutData{
		Tab:   tab,
		Build: s.opts.Build, BindAddr: s.opts.BindAddr, ConfigDir: s.opts.Engine.Settings().ConfigDir,
		ReadOnly: s.opts.ReadOnly, Repo: repoURL, Docs: docsURL, Issues: issuesURL, Changes: changesURL,
	}
	s.render(w, http.StatusOK, "about", s.newView(r, "about", "About", d))
}

// versionLabel is the short form of a version for the header: "v0.1.0" for
// "0.1.0-SNAPSHOT-907a2a5" or "0.1.0", and a name like "dev" as it is.
func versionLabel(v string) string {
	if v == "" {
		return "dev"
	}
	if i := strings.Index(v, "-SNAPSHOT"); i > 0 {
		v = v[:i]
	}
	if v[0] >= '0' && v[0] <= '9' {
		return "v" + v
	}
	return v
}

// shortcut is one keyboard binding. JS is a string app.js must contain for
// the binding to be handled; a test holds the two together.
type shortcut struct {
	Keys        []string
	Description string
	JS          string
}

// shortcuts are the UI's keyboard bindings, in the order the dialog lists
// them. app.js implements them; TestShortcutsDialogListsTheBindings fails
// when one is listed here and not handled there.
var shortcuts = []shortcut{
	{[]string{"Ctrl", "K"}, "Open the command palette (⌘K on a Mac)", `ev.key === "k"`},
	{[]string{"/"}, "Open the command palette", `ev.key === "/"`},
	{[]string{"?"}, "Show these shortcuts", `ev.key === "?"`},
	{[]string{"g", "p"}, "Go to Packs", `p: "/"`},
	{[]string{"g", "r"}, "Go to Runs", `r: "/jobs"`},
	{[]string{"g", "a"}, "Go to the manifest pack audit", `a: "/manifest-pack/audit"`},
	{[]string{"g", "c"}, "Go to Config", `c: "/config"`},
	{[]string{"g", "s"}, "Go to Settings", `s: "/settings"`},
	{[]string{"g", "i"}, "Go to About", `i: "/about"`},
	{[]string{"t"}, "Switch between light and dark", `ev.key === "t"`},
	{[]string{"Esc"}, "Close the open dialog or panel", ""},
}
