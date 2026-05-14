// Package serverhttp provides the HTTP server for the paperclaw daemon,
// including /healthz (liveness), /readyz (readiness), and the OAuth endpoints.
package serverhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"paper-claw/internal/oauth"
	"paper-claw/internal/store"
)

// ReadyzProvider abstracts the readiness checks needed by /readyz.
type ReadyzProvider interface {
	// DriveAuthenticated reports whether a Drive OAuth token is stored.
	DriveAuthenticated(ctx context.Context) bool
	// AnthropicHealthy reports whether the most recent Anthropic probe succeeded.
	AnthropicHealthy() bool
}

// Server is the HTTP server for the daemon.
type Server struct {
	mux    *http.ServeMux
	server *http.Server
	store  *store.DB
	rp     ReadyzProvider
}

// New creates and configures the HTTP server but does not start listening.
func New(bindAddr string, oauthCfg *oauth.Config, st *store.DB, rp ReadyzProvider) *Server {
	mux := http.NewServeMux()
	s := &Server{
		mux:   mux,
		store: st,
		rp:    rp,
		server: &http.Server{
			Addr:              bindAddr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /oauth/start", oauthCfg.HandleStart)
	mux.HandleFunc("GET /oauth/callback", oauthCfg.HandleCallback)

	return s
}

// ListenAndServe starts the HTTP server. It returns when the server is shut
// down. Call Shutdown to stop it gracefully.
func (s *Server) ListenAndServe() error {
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the server within the given context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// Handler returns the underlying http.Handler, useful for testing.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// RegisterWebhook adds the POST /webhook/drive route to the server. Call this
// before ListenAndServe. The handler should return immediately and only enqueue
// a scan — never process inline.
func (s *Server) RegisterWebhook(handler http.Handler) {
	s.mux.Handle("POST /webhook/drive", handler)
}

// handleHealthz is the liveness probe — always returns 200 OK.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// readyzBody is the JSON body written by /readyz.
type readyzBody struct {
	Drive      string `json:"drive"`
	Anthropic  string `json:"anthropic"`
	LastPollAt string `json:"last_poll_at,omitempty"`
	Processed  int64  `json:"processed"`
	Skipped    int64  `json:"skipped"`
	Quarantine int64  `json:"quarantine"`
	LastError  string `json:"last_error,omitempty"`
}

// handleReadyz is the readiness probe.
// Returns 200 if Drive is authenticated AND the Anthropic probe is healthy,
// 503 otherwise. Always writes a JSON status body.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	driveOK := s.rp.DriveAuthenticated(r.Context())
	anthropicOK := s.rp.AnthropicHealthy()

	status, err := s.store.GetStatus(r.Context())
	if err != nil {
		http.Error(w, "reading status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	body := readyzBody{
		Drive:      boolStatus(driveOK),
		Anthropic:  boolStatus(anthropicOK),
		Processed:  status.Processed,
		Skipped:    status.Skipped,
		Quarantine: status.Quarantine,
		LastError:  status.LastError,
	}
	if !status.LastPollAt.IsZero() {
		body.LastPollAt = status.LastPollAt.UTC().Format(time.RFC3339)
	}

	code := http.StatusOK
	if !driveOK || !anthropicOK {
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unavailable"
}
