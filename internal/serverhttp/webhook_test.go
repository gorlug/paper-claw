package serverhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"paper-claw/internal/serverhttp"
)

func newWebhookRequest(channelID, channelToken, resourceState string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhook/drive", nil)
	req.Header.Set("X-Goog-Channel-ID", channelID)
	req.Header.Set("X-Goog-Channel-Token", channelToken)
	req.Header.Set("X-Goog-Resource-State", resourceState)
	return req
}

// TestWebhookHandler_ValidChange_Enqueues verifies that a valid change
// notification enqueues a scan and returns 200.
func TestWebhookHandler_ValidChange_Enqueues(t *testing.T) {
	st := openStore(t)
	_ = st.PutChannel(context.Background(), "ch1", "tok1", "res1", time.Now().Add(time.Hour))

	enqueued := 0
	h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newWebhookRequest("ch1", "tok1", "change"))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rr.Code)
	}
	if enqueued != 1 {
		t.Errorf("enqueued = %d; want 1", enqueued)
	}
}

// TestWebhookHandler_SyncHandshake_NoEnqueue verifies that Drive's sync
// handshake returns 200 but does NOT enqueue a scan.
func TestWebhookHandler_SyncHandshake_NoEnqueue(t *testing.T) {
	st := openStore(t)
	_ = st.PutChannel(context.Background(), "ch1", "tok1", "res1", time.Now().Add(time.Hour))

	enqueued := 0
	h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newWebhookRequest("ch1", "tok1", "sync"))

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rr.Code)
	}
	if enqueued != 0 {
		t.Errorf("enqueued = %d; want 0 (sync handshake must not trigger scan)", enqueued)
	}
}

// TestWebhookHandler_InvalidToken_Returns403 verifies that a wrong channel
// token is rejected with 403.
func TestWebhookHandler_InvalidToken_Returns403(t *testing.T) {
	st := openStore(t)
	_ = st.PutChannel(context.Background(), "ch1", "correct-token", "res1", time.Now().Add(time.Hour))

	enqueued := 0
	h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newWebhookRequest("ch1", "wrong-token", "change"))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rr.Code)
	}
	if enqueued != 0 {
		t.Error("enqueued scan on bad token; want 0")
	}
}

// TestWebhookHandler_InvalidChannelID_Returns403 verifies that a wrong channel
// ID is rejected with 403.
func TestWebhookHandler_InvalidChannelID_Returns403(t *testing.T) {
	st := openStore(t)
	_ = st.PutChannel(context.Background(), "active-channel", "tok1", "res1", time.Now().Add(time.Hour))

	enqueued := 0
	h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newWebhookRequest("unknown-channel", "tok1", "change"))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rr.Code)
	}
	if enqueued != 0 {
		t.Error("enqueued scan on bad channel ID; want 0")
	}
}

// TestWebhookHandler_NoActiveChannel_Returns403 verifies that requests are
// rejected when no channel is registered in the store.
func TestWebhookHandler_NoActiveChannel_Returns403(t *testing.T) {
	st := openStore(t) // no PutChannel

	enqueued := 0
	h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newWebhookRequest("ch1", "tok1", "change"))

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rr.Code)
	}
	if enqueued != 0 {
		t.Error("enqueued scan with no active channel; want 0")
	}
}

// TestWebhookHandler_NonSyncStates_AllEnqueue verifies that any resource state
// other than "sync" triggers a scan.
func TestWebhookHandler_NonSyncStates_AllEnqueue(t *testing.T) {
	for _, state := range []string{"change", "update", "remove", ""} {
		t.Run("state="+state, func(t *testing.T) {
			st := openStore(t)
			_ = st.PutChannel(context.Background(), "ch1", "tok1", "res1", time.Now().Add(time.Hour))

			enqueued := 0
			h := serverhttp.NewWebhookHandler(st, func() { enqueued++ }, nil)

			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, newWebhookRequest("ch1", "tok1", state))

			if rr.Code != http.StatusOK {
				t.Errorf("status = %d; want 200", rr.Code)
			}
			if enqueued != 1 {
				t.Errorf("enqueued = %d; want 1", enqueued)
			}
		})
	}
}
