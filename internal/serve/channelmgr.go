package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"time"

	"paper-claw/internal/store"
)

// ChangeDriver is the subset of Drive API operations needed by ChannelManager.
// *drive.Storage implements this interface.
type ChangeDriver interface {
	// GetStartPageToken returns the current Drive Changes API page token.
	GetStartPageToken(ctx context.Context) (string, error)
	// WatchChanges registers a push-notification channel. Returns the
	// Drive-assigned channel ID, resource ID, and expiration time.
	WatchChanges(ctx context.Context, pageToken, channelID, webhookURL, channelToken string, ttl time.Duration) (string, string, time.Time, error)
	// StopChannel cancels a push-notification channel (best-effort).
	StopChannel(ctx context.Context, channelID, resourceID string) error
}

// ChannelManager sets up and renews a Drive push-notification channel.
// It runs a loop that sleeps until just before the channel expires, then
// stops the old channel and registers a fresh one.
type ChannelManager struct {
	driver        ChangeDriver
	store         *store.DB
	webhookURL    string
	channelTTL    time.Duration
	renewLeadTime time.Duration
	enqueue       func()
}

// NewChannelManager creates a ChannelManager.
// webhookURL is the full public HTTPS URL that Drive will POST to (e.g.
// "https://paperclaw.example.com/webhook/drive"). Pass an empty string to
// disable push notifications (useful when the daemon has no public address).
func NewChannelManager(
	driver ChangeDriver,
	st *store.DB,
	webhookURL string,
	channelTTL, renewLeadTime time.Duration,
	enqueue func(),
) *ChannelManager {
	return &ChannelManager{
		driver:        driver,
		store:         st,
		webhookURL:    webhookURL,
		channelTTL:    channelTTL,
		renewLeadTime: renewLeadTime,
		enqueue:       enqueue,
	}
}

// Run sets up the initial push-notification channel and then loops, renewing
// the channel before it expires. It returns when ctx is cancelled, stopping
// the active channel as a best-effort cleanup to avoid leaking Drive channels.
func (m *ChannelManager) Run(ctx context.Context) {
	if m.webhookURL == "" {
		slog.Info("channel: webhookURL not configured — push notifications disabled")
		<-ctx.Done()
		return
	}

	m.ensureStartPageToken(ctx)
	m.ensureChannel(ctx)

	for {
		ss, err := m.store.GetSyncState(ctx)
		if err != nil {
			slog.Warn("channel: reading sync state", "err", err)
		}

		var wait time.Duration
		if !ss.ExpiresAt.IsZero() {
			wait = time.Until(ss.ExpiresAt.Add(-m.renewLeadTime))
		} else {
			// No channel yet (setup failed); retry after a short delay.
			wait = time.Minute
		}
		if wait < 0 {
			wait = 0
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			// Best-effort: stop the current channel so Drive doesn't keep
			// sending notifications to a dead endpoint.
			if bss, err2 := m.store.GetSyncState(context.Background()); err2 == nil && bss.ChannelID != "" {
				_ = m.driver.StopChannel(context.Background(), bss.ChannelID, bss.ResourceID)
			}
			return
		case <-timer.C:
			// Renewal: stop the old channel then register a new one.
			if ss.ChannelID != "" {
				_ = m.driver.StopChannel(ctx, ss.ChannelID, ss.ResourceID)
			}
			m.ensureChannel(ctx)
		}
	}
}

// ensureStartPageToken fetches and persists the Drive Changes start page token
// if none is stored yet. This token is used as the starting position when a
// new push-notification channel is registered.
func (m *ChannelManager) ensureStartPageToken(ctx context.Context) {
	ss, err := m.store.GetSyncState(ctx)
	if err != nil {
		slog.Warn("channel: reading sync state", "err", err)
		return
	}
	if ss.StartPageToken != "" {
		return
	}
	token, err := m.driver.GetStartPageToken(ctx)
	if err != nil {
		slog.Warn("channel: getting start page token", "err", err)
		return
	}
	if err := m.store.PutStartPageToken(ctx, token); err != nil {
		slog.Warn("channel: persisting start page token", "err", err)
	}
}

// ensureChannel registers a new push-notification channel unless the stored
// channel is still active and not yet within the renewal window.
func (m *ChannelManager) ensureChannel(ctx context.Context) {
	ss, err := m.store.GetSyncState(ctx)
	if err != nil {
		slog.Warn("channel: reading sync state", "err", err)
		return
	}

	// Skip setup if a fresh channel is already registered.
	if ss.ChannelID != "" && time.Now().Before(ss.ExpiresAt.Add(-m.renewLeadTime)) {
		return
	}

	if ss.StartPageToken == "" {
		slog.Warn("channel: no start page token — deferring push channel setup until Drive is authenticated")
		return
	}

	channelID := randomHex(16)
	channelToken := randomHex(32)

	assignedID, resourceID, expiresAt, err := m.driver.WatchChanges(
		ctx, ss.StartPageToken, channelID, m.webhookURL, channelToken, m.channelTTL,
	)
	if err != nil {
		slog.Warn("channel: registering push channel", "err", err)
		return
	}

	if err := m.store.PutChannel(ctx, assignedID, channelToken, resourceID, expiresAt); err != nil {
		slog.Warn("channel: persisting channel metadata", "err", err)
	}
	slog.Info("channel: push channel registered", "id", assignedID, "expires", expiresAt)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
