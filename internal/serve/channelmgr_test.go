package serve_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"paper-claw/internal/serve"
)

// fakeChangeDriver implements serve.ChangeDriver for tests.
type fakeChangeDriver struct {
	mu          sync.Mutex
	startToken  string
	getTokenErr error
	watchErr    error
	stopErr     error
	watchCalls  int
	stopCalls   int
}

func (f *fakeChangeDriver) GetStartPageToken(_ context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startToken, f.getTokenErr
}

func (f *fakeChangeDriver) WatchChanges(
	_ context.Context,
	_, channelID, _, _ string,
	ttl time.Duration,
) (string, string, time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.watchErr != nil {
		return "", "", time.Time{}, f.watchErr
	}
	f.watchCalls++
	return channelID, "resource-" + channelID, time.Now().Add(ttl), nil
}

func (f *fakeChangeDriver) StopChannel(_ context.Context, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	return f.stopErr
}

func (f *fakeChangeDriver) watchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchCalls
}

func (f *fakeChangeDriver) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopCalls
}

// TestChannelManager_FetchesStartPageToken verifies that the manager fetches
// and persists the start page token from Drive when none is stored.
func TestChannelManager_FetchesStartPageToken(t *testing.T) {
	st := openStore(t)
	driver := &fakeChangeDriver{startToken: "fetched-token"}

	mgr := serve.NewChannelManager(driver, st, "https://example.com/webhook/drive",
		time.Hour, 30*time.Minute, func() {})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	mgr.Run(ctx)

	ss, err := st.GetSyncState(context.Background())
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if ss.StartPageToken != "fetched-token" {
		t.Errorf("start page token = %q; want %q", ss.StartPageToken, "fetched-token")
	}
}

// TestChannelManager_SkipsSetupWhenChannelActive verifies that WatchChanges is
// NOT called when the stored channel is still active and not near expiry.
func TestChannelManager_SkipsSetupWhenChannelActive(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()

	_ = st.PutStartPageToken(ctx, "start-token")
	_ = st.PutChannel(ctx, "active-channel", "tok1", "res1", time.Now().Add(24*time.Hour))

	driver := &fakeChangeDriver{startToken: "start-token"}

	mgr := serve.NewChannelManager(driver, st, "https://example.com/webhook/drive",
		time.Hour, 30*time.Minute, func() {})

	runCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	mgr.Run(runCtx)

	if driver.watchCount() != 0 {
		t.Errorf("WatchChanges called %d times; want 0 (channel was active)", driver.watchCount())
	}
}

// TestChannelManager_ReplacesNearExpiryChannel verifies that a channel within
// the renewal window is replaced on startup.
func TestChannelManager_ReplacesNearExpiryChannel(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()

	// Channel expires in 10 minutes but renewal window is 30 minutes — already
	// within the window.
	_ = st.PutStartPageToken(ctx, "start-token")
	_ = st.PutChannel(ctx, "old-channel", "old-tok", "old-res", time.Now().Add(10*time.Minute))

	driver := &fakeChangeDriver{startToken: "start-token"}

	mgr := serve.NewChannelManager(driver, st, "https://example.com/webhook/drive",
		time.Hour, 30*time.Minute, func() {})

	runCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	mgr.Run(runCtx)

	if driver.watchCount() < 1 {
		t.Errorf("WatchChanges called %d times; want >= 1 (near-expiry channel should be replaced)", driver.watchCount())
	}
}

// TestChannelManager_RenewsChannelBeforeExpiry verifies that the manager
// renews the channel before expiry using a short TTL.
func TestChannelManager_RenewsChannelBeforeExpiry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timer-based renewal test in short mode")
	}

	st := openStore(t)
	_ = st.PutStartPageToken(context.Background(), "start-token")

	const (
		channelTTL    = 500 * time.Millisecond
		renewLeadTime = 100 * time.Millisecond
		testTimeout   = 900 * time.Millisecond
	)

	driver := &fakeChangeDriver{startToken: "start-token"}

	mgr := serve.NewChannelManager(driver, st, "https://example.com/webhook/drive",
		channelTTL, renewLeadTime, func() {})

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	mgr.Run(ctx)

	// Initial setup (1) + at least one renewal (1) = at least 2 calls.
	if driver.watchCount() < 2 {
		t.Errorf("WatchChanges called %d times; want >= 2 (initial + renewal)", driver.watchCount())
	}
	// Old channel stopped on renewal + current channel stopped on shutdown.
	if driver.stopCount() < 1 {
		t.Errorf("StopChannel called %d times; want >= 1", driver.stopCount())
	}
}

// TestChannelManager_DisabledWhenNoWebhookURL verifies that no API calls are
// made when webhookURL is empty.
func TestChannelManager_DisabledWhenNoWebhookURL(t *testing.T) {
	st := openStore(t)
	driver := &fakeChangeDriver{startToken: "start-token"}

	mgr := serve.NewChannelManager(driver, st, "", time.Hour, 30*time.Minute, func() {})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	mgr.Run(ctx)

	if driver.watchCount() != 0 || driver.stopCount() != 0 {
		t.Errorf("expected no API calls with empty webhookURL, got watch=%d stop=%d",
			driver.watchCount(), driver.stopCount())
	}
}
