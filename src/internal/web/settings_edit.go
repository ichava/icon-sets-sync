package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

// Editing a setting in place on the settings page. Every edit is written to
// the global config, the one layer this server can change: the environment
// and flags belong to the process that started it, and a pack's own settings
// live in its pack config. The page says so beside each value, and warns
// when a layer above the global config sets the key, because the saved value
// would then not take effect.

// settingInputs are the keys whose control differs from what their kind
// implies: directories and an executable, which are checked on disk, and the
// one multi-line text.
var settingInputs = map[string]string{
	"config_dir":         "dir",
	"private_config_dir": "dir",
	"workspace":          "dir",
	"npm_path":           "file",
	"git.commit_body":    "textarea",
}

// settingInput names the control the page offers for a key.
func settingInput(k settings.Key) string {
	if in, ok := settingInputs[k.Name]; ok {
		return in
	}
	switch {
	case k.Kind == settings.Bool:
		return "bool"
	case k.Kind == settings.Int:
		return "int"
	case k.Kind == settings.List:
		return "list"
	case len(k.Enum) > 0:
		return "enum"
	}
	return "text"
}

func defaultText(k settings.Key) string {
	switch v := k.Default.(type) {
	case []string:
		return strings.Join(v, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// Form checks stricter than the loader's, for values typed into the page.
// The loader accepts these keys as any string; a typo in a host name or a
// branch name otherwise surfaces only when a run fails on it.
var (
	hostLabel    = `[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?`
	hostRE       = regexp.MustCompile(`^` + hostLabel + `(\.` + hostLabel + `)+$`)
	unicodeVerRE = regexp.MustCompile(`^[0-9]{1,2}\.[0-9]$`)
	// A git ref name, after git check-ref-format, with {version} allowed.
	badRefRE = regexp.MustCompile(`[\s~^:?*\[\\]|\.\.|@\{|//|^[-/.]|[/.]$|\.lock$|^@$`)
)

// maxSettingLen bounds any one submitted value: a setting is a path, a name
// or a short template, never a document.
const maxSettingLen = 4096

// parseSetting turns the submitted form values for one key into the value
// the global config stores, refusing what the loader would refuse and what
// the form checks above catch. note is a warning that does not refuse.
func parseSetting(k settings.Key, raw []string) (value any, note string, err error) {
	for _, v := range raw {
		if len(v) > maxSettingLen {
			return nil, "", fmt.Errorf("is longer than %d characters", maxSettingLen)
		}
		if strings.ContainsRune(v, 0) {
			return nil, "", errors.New("contains a NUL character")
		}
	}
	last := ""
	if len(raw) > 0 {
		last = raw[len(raw)-1]
	}
	switch k.Kind {
	case settings.Bool:
		// A switch posts a hidden "false" and, when on, a "true" after it.
		v, err := k.ParseString(last)
		return v, "", err
	case settings.Int:
		if strings.TrimSpace(last) == "" {
			return nil, "", errors.New("needs a number")
		}
		n, err := strconv.Atoi(strings.TrimSpace(last))
		if err != nil {
			return nil, "", fmt.Errorf("%q is not a whole number", strings.TrimSpace(last))
		}
		v, err := k.Check(n)
		return v, "", err
	case settings.List:
		var items []string
		seen := map[string]bool{}
		for _, v := range raw {
			for _, line := range strings.Split(v, "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if seen[line] {
					return nil, "", fmt.Errorf("lists %s twice", line)
				}
				seen[line] = true
				items = append(items, line)
			}
		}
		if len(items) == 0 {
			return nil, "", errors.New("is empty; use Reset to remove it from the global config")
		}
		if k.Paths {
			var missing []string
			for _, it := range items {
				if !isDir(expandHome(it)) {
					missing = append(missing, it)
				}
			}
			if len(missing) > 0 {
				note = "not found on this machine: " + strings.Join(missing, ", ")
			}
		}
		return items, note, nil
	}

	s := last
	if settingInputs[k.Name] != "textarea" {
		s = strings.TrimSpace(s)
		if strings.ContainsAny(s, "\r\n") {
			return nil, "", errors.New("must be one line")
		}
	}
	if s == "" {
		return nil, "", errors.New("is empty; use Reset to go back to the default")
	}
	if k.Name == "allowed_hosts" && strings.Contains(s, "://") {
		// Before the loader's character check, which would only quote its
		// regular expression back.
		return nil, "", fmt.Errorf("%q is not a host name (no scheme, port or path: example.org)", s)
	}
	v, err := k.Check(s)
	if err != nil {
		return nil, "", err
	}
	s, _ = v.(string)
	switch settingInputs[k.Name] {
	case "dir":
		p := expandHome(s)
		if !filepath.IsAbs(p) {
			return nil, "", fmt.Errorf("%q is not an absolute path (start with / or ~)", s)
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, "", fmt.Errorf("%s does not exist", s)
		}
		if !info.IsDir() {
			return nil, "", fmt.Errorf("%s is not a directory", s)
		}
	case "file":
		p := expandHome(s)
		if !filepath.IsAbs(p) {
			return nil, "", fmt.Errorf("%q is not an absolute path (start with / or ~)", s)
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, "", fmt.Errorf("%s does not exist", s)
		}
		if info.IsDir() {
			return nil, "", fmt.Errorf("%s is a directory, not the npm executable", s)
		}
		if info.Mode().Perm()&0o111 == 0 {
			return nil, "", fmt.Errorf("%s is not executable", s)
		}
	}
	switch k.Name {
	case "allowed_hosts":
		var hosts []string
		for _, h := range strings.Split(s, ",") {
			h = strings.ToLower(strings.TrimSpace(h))
			if h == "" {
				continue
			}
			if !hostRE.MatchString(h) {
				return nil, "", fmt.Errorf("%q is not a host name (no scheme, port or path: example.org)", h)
			}
			hosts = append(hosts, h)
		}
		if len(hosts) == 0 {
			return nil, "", errors.New("names no host")
		}
		s = strings.Join(hosts, ",")
	case "git.base", "git.branch":
		if badRefRE.MatchString(strings.ReplaceAll(s, "{version}", "v")) {
			return nil, "", fmt.Errorf("%q is not a valid git branch name", s)
		}
	case "emoji.unicode_version":
		if !unicodeVerRE.MatchString(s) {
			return nil, "", fmt.Errorf("%q is not a Unicode emoji release such as 16.0", s)
		}
	case "git.commit_subject":
		if len(s) > 200 {
			return nil, "", errors.New("is longer than 200 characters; a pull request title that long is cut off")
		}
	}
	return s, note, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// settingFormKey reads {name} and finds the key, or answers 404.
func (s *Server) settingFormKey(w http.ResponseWriter, r *http.Request) (settings.Key, bool) {
	k, ok := settings.Lookup(r.PathValue("name"))
	if !ok || k.Name != r.PathValue("name") {
		s.renderError(w, r, http.StatusNotFound, "No such setting", "There is no setting by that name.")
		return settings.Key{}, false
	}
	return k, true
}

// handleSettingCheck validates a value without writing it: what the page's
// inputs call as you type. It answers JSON.
func (s *Server) handleSettingCheck(w http.ResponseWriter, r *http.Request) {
	k, ok := s.settingFormKey(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := map[string]any{"ok": true}
	value, note, err := parseSetting(k, r.PostForm["value"])
	if err != nil {
		out["ok"], out["error"] = false, k.Name+" "+err.Error()
	} else {
		out["value"] = displayValue(value)
		if note != "" {
			out["note"] = note
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

func displayValue(v any) string {
	switch x := v.(type) {
	case []string:
		return strings.Join(x, "\n")
	default:
		return fmt.Sprint(x)
	}
}

// handleSettingSave writes one setting into the global config (action=save)
// or removes it from there (action=reset), through WriteConfig: validated,
// atomic, refused on a concurrent change and kept in the history.
func (s *Server) handleSettingSave(w http.ResponseWriter, r *http.Request) {
	k, ok := s.settingFormKey(w, r)
	if !ok {
		return
	}
	var err error
	saved := "saved"
	switch r.PostFormValue("action") {
	case "reset":
		err, saved = s.saveGlobalKey(k.Name, nil), "reset"
	case "save", "":
		var value any
		if value, _, err = parseSetting(k, r.PostForm["value"]); err == nil {
			err = s.saveGlobalKey(k.Name, value)
		} else {
			err = fmt.Errorf("%s %w", k.Name, err)
		}
	default:
		err = errors.New("unknown action")
	}
	anchor := SettingRow{Name: k.Name}.Anchor()
	if err == nil {
		http.Redirect(w, r, "/settings?"+saved+"="+k.Name+"#"+anchor, http.StatusSeeOther)
		return
	}
	d := settingsData{SettingsView: s.opts.Engine.Settings(), BindAddr: s.opts.BindAddr, ReadOnly: s.opts.ReadOnly,
		Version: s.opts.Version, Problem: err.Error(), Field: k.Name, Submitted: strings.Join(r.PostForm["value"], "\n")}
	s.render(w, http.StatusUnprocessableEntity, "settings", s.newView(r, "settings", "Settings", d))
}

// setTreeKey sets a setting in a decoded global config, nested by its dots
// ("git.base" under "git") the way the shipped configs write it, and drops a
// flat "git.base" spelling so the two cannot disagree. A nil value removes
// it, and any table it leaves empty.
func setTreeKey(tree map[string]any, name string, value any) {
	delete(tree, name)
	parts := strings.Split(name, ".")
	var walk func(node map[string]any, parts []string) bool
	walk = func(node map[string]any, parts []string) bool {
		if len(parts) == 1 {
			if value == nil {
				delete(node, parts[0])
			} else {
				node[parts[0]] = value
			}
			return len(node) == 0
		}
		child, ok := node[parts[0]].(map[string]any)
		if !ok {
			if value == nil {
				return len(node) == 0
			}
			child = map[string]any{}
			node[parts[0]] = child
		}
		if walk(child, parts[1:]) {
			delete(node, parts[0])
		}
		return len(node) == 0
	}
	walk(tree, parts)
}
