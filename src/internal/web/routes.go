package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/recipes"
)

// routes registers every page and action. Under ReadOnly the routes that run
// a sync or write a file are not registered at all, rather than registered
// and refused: a route that does not exist cannot be reached by a bug in a
// check.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /assets/{hash}/{path...}", s.serveAsset)
	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("POST /check", s.handleRefresh)
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("GET /about/licenses", s.handleLicenses)
	mux.HandleFunc("GET /schema/pack.schema.json", s.handleSchema)
	mux.HandleFunc("GET /packs/{slug}", s.handlePack)
	mux.HandleFunc("GET /jobs", s.handleJobs)
	mux.HandleFunc("GET /jobs/{id}", s.handleJob)
	mux.HandleFunc("GET /jobs/{id}/log.jsonl", s.handleJobLog)
	mux.HandleFunc("GET /events/jobs/{id}", s.handleEvents)
	mux.HandleFunc("GET "+auditPath, s.handleAudit)
	mux.HandleFunc("GET "+auditPath+"/{id}/manifest.json", s.handleAuditManifest)
	mux.HandleFunc("GET "+legacyAuditPath, deprecatedRoute(legacyAuditPath, auditPath, s.handleAudit))
	mux.HandleFunc("GET "+legacyAuditPath+"/{id}/manifest.json",
		deprecatedRoute(legacyAuditPath+"/{id}/manifest.json", auditPath+"/{id}/manifest.json", s.handleAuditManifest))
	mux.HandleFunc("GET /config", s.handleConfigList)
	mux.HandleFunc("GET /config/{name}", s.handleConfigEdit)
	// Validation writes nothing, so a read-only server keeps it.
	mux.HandleFunc("POST /config/{name}/validate", s.handleConfigValidate)

	if !s.opts.ReadOnly {
		s.writeRoutes(mux)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if s.opts.ReadOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			s.renderError(w, r, http.StatusMethodNotAllowed, "Read-only server",
				"This server was started with --read-only, so it cannot run a sync or change a file.")
			return
		}
		s.renderError(w, r, http.StatusNotFound, "Not found", "There is no page at this address.")
	})
	return mux
}

// writeRoutes registers the routes that run a sync or change a file.
func (s *Server) writeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /packs/{slug}/sync", s.handleSync)
	mux.HandleFunc("POST /packs/{slug}/recipe", s.handleRecipe)
	mux.HandleFunc("POST /sync-all", s.handleSyncAll)
	mux.HandleFunc("POST /jobs/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /config/{name}", s.handleConfigSave)
	mux.HandleFunc("POST "+auditPath, s.handleAuditStart)
	mux.HandleFunc("POST "+legacyAuditPath, deprecatedRoute(legacyAuditPath, auditPath, s.handleAuditStart))
}

// auditPath is the manifest-pack audit page and its actions.
const auditPath = "/manifest-pack/audit"

// legacyAuditPath is auditPath's former address. It serves the same handlers
// so a bookmark or a script keeps working, and every request logs a notice.
//
// Deprecated: use auditPath. Earliest removal: 0.2.0.
const legacyAuditPath = "/bundled/audit"

// deprecatedRoute serves h unchanged, logs that old is deprecated in favour
// of replacement, and says so in the response headers (RFC 9745's
// Deprecation, plus a Link to the successor).
func deprecatedRoute(old, replacement string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slog.Warn("deprecated route; it will be removed in "+recipes.RemovalRelease,
			"route", old, "use", replacement, "method", r.Method)
		w.Header().Set("Deprecation", "true")
		w.Header().Set("Link", "<"+strings.ReplaceAll(replacement, "{id}", r.PathValue("id"))+`>; rel="successor-version"`)
		h(w, r)
	}
}
