package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/docfile"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/webvendor"
)

type settingsData struct {
	SettingsView
	BindAddr string
	ReadOnly bool
	Version  string
	// Saved names what a save just changed (Reset, what a reset removed);
	// Problem is why a save was refused.
	Saved, Reset, Problem string
	// Field is the setting a refused save was for, and Submitted what was
	// typed, kept so the person does not lose it.
	Field, Submitted string
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	d := settingsData{
		SettingsView: s.opts.Engine.Settings(),
		BindAddr:     s.opts.BindAddr,
		ReadOnly:     s.opts.ReadOnly,
		Version:      s.opts.Version,
		Saved:        r.URL.Query().Get("saved"),
		Reset:        r.URL.Query().Get("reset"),
	}
	s.render(w, http.StatusOK, "settings", s.newView(r, "settings", "Settings", d))
}

// handlePackagesDirsSave writes the packages_dirs list editor into the global
// config, keeping every other key in it. It goes through WriteConfig, so the
// save is validated, atomic, refused on a concurrent change and kept in the
// history like an editor save.
func (s *Server) handlePackagesDirsSave(w http.ResponseWriter, r *http.Request) {
	var dirs []string
	for _, d := range r.PostForm["dir"] {
		if d = strings.TrimSpace(d); d != "" {
			dirs = append(dirs, d)
		}
	}
	err := s.savePackagesDirs(dirs)
	if err == nil {
		http.Redirect(w, r, "/settings?saved=packages_dirs#packages-dirs", http.StatusSeeOther)
		return
	}
	d := settingsData{SettingsView: s.opts.Engine.Settings(), BindAddr: s.opts.BindAddr, ReadOnly: s.opts.ReadOnly,
		Version: s.opts.Version, Problem: err.Error()}
	s.render(w, http.StatusUnprocessableEntity, "settings", s.newView(r, "settings", "Settings", d))
}

func (s *Server) savePackagesDirs(dirs []string) error {
	if len(dirs) == 0 {
		return s.saveGlobalKey("packages_dirs", nil)
	}
	return s.saveGlobalKey("packages_dirs", dirs)
}

// saveGlobalKey sets one top-level key of the global config (nil removes it),
// keeping every other key, through WriteConfig, and publishes config.changed.
func (s *Server) saveGlobalKey(key string, value any) error {
	if err := SaveGlobalSetting(s.opts.Engine, key, value); err != nil {
		return err
	}
	action := "saved"
	if value == nil {
		action = "reset"
	}
	s.publishConfigChanged(GlobalName, key, action)
	return nil
}

// ConfigStore reads and writes the config documents; an Engine is one.
type ConfigStore interface {
	ReadConfig(name string) (ConfigDoc, error)
	WriteConfig(name string, data []byte, base string) error
}

// SaveGlobalSetting sets one key of the global config (nil removes it),
// keeping every other key, through the engine's WriteConfig: validated,
// atomic, refused on a concurrent change and kept in the history. It drops
// the key's aliases. The web UI, the API, the MCP server and the webhooks
// command all save a setting through it.
func SaveGlobalSetting(eng ConfigStore, key string, value any) error {
	doc, err := eng.ReadConfig(GlobalName)
	if err != nil {
		return err
	}
	tree := map[string]any{}
	if doc.Exists && len(strings.TrimSpace(string(doc.Data))) > 0 {
		if docfile.IsYAML(doc.Path) {
			return fmt.Errorf("%s is YAML; edit %s in the config editor so its comments are kept", doc.Path, key)
		}
		if err := json.Unmarshal(doc.Data, &tree); err != nil {
			return fmt.Errorf("%s is not valid JSON (%v); fix it in the config editor first", doc.Path, err)
		}
	}
	if k, ok := settings.Lookup(key); ok {
		for _, a := range k.Aliases {
			delete(tree, a)
		}
	}
	setTreeKey(tree, key, value)
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return err
	}
	return eng.WriteConfig(GlobalName, append(data, '\n'), doc.Rev)
}

type licenseEntry struct {
	Package, Version, What, Text string
}

// handleLicenses shows the notices of the embedded third-party assets, so the
// licence travels with a bare binary as well as with the archives.
func (s *Server) handleLicenses(w http.ResponseWriter, r *http.Request) {
	ls, err := s.embeddedLicences()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Licences unavailable", err.Error())
		return
	}
	var out []licenseEntry
	for _, l := range ls {
		out = append(out, licenseEntry{l.Package, l.Version, l.What, strings.TrimRight(string(l.Text), "\n")})
	}
	s.render(w, http.StatusOK, "licenses", s.newView(r, "about", "Licences", out))
}

func (s *Server) embeddedLicences() ([]webvendor.Licence, error) {
	vers, err := webvendor.ParseVersions(s.assets.files["vendor/"+webvendor.VersionFile])
	if err != nil {
		return nil, err
	}
	texts := map[string][]byte{}
	for name, body := range s.assets.files {
		if rest, ok := strings.CutPrefix(name, "vendor/LICENSES/"); ok {
			texts[rest] = body
		}
	}
	return webvendor.Licences(vers, texts), nil
}

// handleSchema serves the pack schema the loader validates against.
func (s *Server) handleSchema(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(config.PackSchema())
}

// handleGlobalSchema serves the global config's schema, generated from the
// settings registry.
func (s *Server) handleGlobalSchema(w http.ResponseWriter, _ *http.Request) {
	data, err := settings.GlobalSchema()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
