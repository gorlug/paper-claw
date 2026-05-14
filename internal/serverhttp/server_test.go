package serverhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"paper-claw/internal/oauth"
	"paper-claw/internal/serverhttp"
	"paper-claw/internal/store"
)

// stubReadyz implements ReadyzProvider for tests.
type stubReadyz struct {
	driveOK     bool
	anthropicOK bool
}

func (s *stubReadyz) DriveAuthenticated(_ context.Context) bool { return s.driveOK }
func (s *stubReadyz) AnthropicHealthy() bool                    { return s.anthropicOK }

func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func noopOAuthCfg(t *testing.T, st *store.DB) *oauth.Config {
	t.Helper()
	return oauth.New("id", "secret", "http://localhost", "/oauth/callback", st,
		func(_ context.Context, _ *http.Client) error { return nil }, nil)
}

func newHandler(t *testing.T, rp serverhttp.ReadyzProvider) http.Handler {
	t.Helper()
	st := openStore(t)
	oauthCfg := noopOAuthCfg(t, st)
	srv := serverhttp.New(":0", oauthCfg, st, rp)
	return srv.Handler()
}

// --- /healthz ----------------------------------------------------------------

func TestHealthz_AlwaysOK(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: false, anthropicOK: false})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("/healthz status = %d; want 200", w.Code)
	}
}

// --- /readyz -----------------------------------------------------------------

func TestReadyz_BothOK(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: true, anthropicOK: true})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}
	assertReadyzJSON(t, w, "ok", "ok")
}

func TestReadyz_DriveDown_Returns503(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: false, anthropicOK: true})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", w.Code)
	}
	assertReadyzJSON(t, w, "unavailable", "ok")
}

func TestReadyz_AnthropicDown_Returns503(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: true, anthropicOK: false})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", w.Code)
	}
	assertReadyzJSON(t, w, "ok", "unavailable")
}

func TestReadyz_BothDown_Returns503(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: false, anthropicOK: false})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", w.Code)
	}
	assertReadyzJSON(t, w, "unavailable", "unavailable")
}

func TestReadyz_JSONContentType(t *testing.T) {
	h := newHandler(t, &stubReadyz{driveOK: true, anthropicOK: true})
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q; want application/json", ct)
	}
}

func assertReadyzJSON(t *testing.T, w *httptest.ResponseRecorder, wantDrive, wantAnthropic string) {
	t.Helper()
	var body struct {
		Drive     string `json:"drive"`
		Anthropic string `json:"anthropic"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decoding readyz body: %v", err)
	}
	if body.Drive != wantDrive {
		t.Errorf("drive = %q; want %q", body.Drive, wantDrive)
	}
	if body.Anthropic != wantAnthropic {
		t.Errorf("anthropic = %q; want %q", body.Anthropic, wantAnthropic)
	}
}
