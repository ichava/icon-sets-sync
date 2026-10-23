package web

import (
	"net/http"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/tabler"
)

type settingsData struct {
	SettingsView
	BindAddr string
	ReadOnly bool
	Version  string
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	d := settingsData{
		SettingsView: s.opts.Engine.Settings(),
		BindAddr:     s.opts.BindAddr,
		ReadOnly:     s.opts.ReadOnly,
		Version:      s.opts.Version,
	}
	s.render(w, http.StatusOK, "settings", s.newView(r, "settings", "Settings", d))
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
	s.render(w, http.StatusOK, "licenses", s.newView(r, "licenses", "Licences", out))
}

func (s *Server) embeddedLicences() ([]tabler.Licence, error) {
	vers, err := tabler.ParseVersions(s.assets.files["vendor/"+tabler.VersionFile])
	if err != nil {
		return nil, err
	}
	texts := map[string][]byte{}
	for name, body := range s.assets.files {
		if rest, ok := strings.CutPrefix(name, "vendor/LICENSES/"); ok {
			texts[rest] = body
		}
	}
	return tabler.Licences(vers, texts), nil
}

// handleSchema serves the pack schema the loader validates against.
func (s *Server) handleSchema(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(config.PackSchema())
}
