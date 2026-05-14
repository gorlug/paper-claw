package serverhttp

import (
	"log/slog"
	"net/http"

	"paper-claw/internal/store"
	"paper-claw/internal/telemetry"
)

// WebhookHandler handles POST /webhook/drive notifications from the Google
// Drive Changes API.
//
// Security: the handler verifies both X-Goog-Channel-ID and
// X-Goog-Channel-Token against the values stored in the SQLite state. A
// mismatched token returns 403 — this is the primary auth mechanism since the
// endpoint is publicly reachable (Drive pushes to it directly).
type WebhookHandler struct {
	store   *store.DB
	enqueue func()
	metrics *telemetry.Metrics // may be nil when telemetry is disabled
}

// NewWebhookHandler creates a WebhookHandler.
// enqueue is called non-blocking when a real change notification arrives.
func NewWebhookHandler(st *store.DB, enqueue func(), m *telemetry.Metrics) *WebhookHandler {
	return &WebhookHandler{store: st, enqueue: enqueue, metrics: m}
}

// ServeHTTP implements http.Handler.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelID := r.Header.Get("X-Goog-Channel-ID")
	channelToken := r.Header.Get("X-Goog-Channel-Token")
	resourceState := r.Header.Get("X-Goog-Resource-State")

	ss, err := h.store.GetSyncState(r.Context())
	if err != nil {
		slog.Error("webhook: reading sync state", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Reject requests that don't match the active channel credentials.
	if ss.ChannelID == "" || channelID != ss.ChannelID || channelToken != ss.ChannelToken {
		slog.Warn("webhook: rejected — invalid channel credentials",
			"recv_channel", channelID, "active_channel", ss.ChannelID)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if resourceState == "sync" {
		// Drive handshake sent when a channel is first registered — acknowledge
		// and do nothing.
		w.WriteHeader(http.StatusOK)
		return
	}

	// Real change notification: enqueue a scan and return immediately.
	// Never process inline — the worker handles dedup and pipeline execution.
	h.enqueue()
	if h.metrics != nil {
		h.metrics.WebhookCount.Add(r.Context(), 1)
	}
	slog.Debug("webhook: change notification enqueued", "channel_id", channelID, "state", resourceState)
	w.WriteHeader(http.StatusOK)
}
