package web

import (
	"net/http"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
)

// apiRoutes registers the REST API, one route per line, each with the scope
// it needs. As in routes, under ReadOnly a route that runs a sync or writes
// a file is not registered at all. openapi.json describes exactly these;
// TestOpenAPIMatchesTheRoutes fails when the two disagree.
func (s *Server) apiRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1", s.scoped(apitoken.ScopeRead, s.apiIndex))
	mux.HandleFunc("GET /api/v1/openapi.json", s.scoped(apitoken.ScopeRead, s.apiOpenAPI))
	mux.HandleFunc("GET /api/v1/schemas/{type}", s.scoped(apitoken.ScopeRead, s.apiSchema))
	mux.HandleFunc("GET /api/v1/packs", s.scoped(apitoken.ScopeRead, s.apiPacks))
	mux.HandleFunc("GET /api/v1/packs/{slug}", s.scoped(apitoken.ScopeRead, s.apiPack))
	mux.HandleFunc("GET /api/v1/jobs", s.scoped(apitoken.ScopeRead, s.apiJobs))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.scoped(apitoken.ScopeRead, s.apiJob))
	mux.HandleFunc("GET /api/v1/jobs/{id}/log", s.scoped(apitoken.ScopeRead, s.handleJobLog))
	mux.HandleFunc("GET /api/v1/settings", s.scoped(apitoken.ScopeRead, s.apiSettings))
	mux.HandleFunc("GET /api/v1/validate", s.scoped(apitoken.ScopeRead, s.apiValidate))
	mux.HandleFunc("GET /api/v1/audit/{id}", s.scoped(apitoken.ScopeRead, s.apiAudit))
	// An upstream check writes nothing, so a read-only server keeps it.
	mux.HandleFunc("POST /api/v1/check", s.scoped(apitoken.ScopeSync, s.apiCheck))
	mux.HandleFunc("GET /api/v1/webhooks/deliveries", s.scoped(apitoken.ScopeAdmin, s.apiWebhookDeliveries))
	mux.HandleFunc("GET /api/v1/tokens", s.scoped(apitoken.ScopeAdmin, s.apiTokens))
	// Stopping the server changes no file, so a read-only server keeps it.
	mux.HandleFunc("POST /api/v1/server/stop", s.scoped(apitoken.ScopeAdmin, s.apiStop))
	if s.opts.API.EventsStream {
		mux.HandleFunc("GET /api/v1/events", s.scoped(apitoken.ScopeRead, s.apiEvents))
	}
	if !s.opts.ReadOnly {
		mux.HandleFunc("POST /api/v1/sync", s.scoped(apitoken.ScopeSync, s.apiSyncAll))
		mux.HandleFunc("POST /api/v1/packs/{slug}/sync", s.scoped(apitoken.ScopeSync, s.apiSyncPack))
		mux.HandleFunc("POST /api/v1/jobs/{id}/cancel", s.scoped(apitoken.ScopeSync, s.apiCancel))
		mux.HandleFunc("PUT /api/v1/settings/{name}", s.scoped(apitoken.ScopeConfigWrite, s.apiSettingPut))
		mux.HandleFunc("DELETE /api/v1/settings/{name}", s.scoped(apitoken.ScopeConfigWrite, s.apiSettingDelete))
		mux.HandleFunc("POST /api/v1/webhooks/{name}/test", s.scoped(apitoken.ScopeAdmin, s.apiWebhookTest))
	}
	mux.HandleFunc(apiPrefix, s.apiNoRoute)
	return mux
}
