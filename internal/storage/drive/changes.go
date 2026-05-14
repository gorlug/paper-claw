package drive

import (
	"context"
	"fmt"
	"time"

	gdrive "google.golang.org/api/drive/v3"
)

// GetStartPageToken returns the current Drive Changes API page token. This
// marks the position from which a new push-notification channel will start
// delivering change events.
func (s *Storage) GetStartPageToken(ctx context.Context) (string, error) {
	c, err := s.driveClient(ctx)
	if err != nil {
		return "", err
	}
	resp, err := c.svc.Changes.GetStartPageToken().Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("drive: getting start page token: %w", err)
	}
	return resp.StartPageToken, nil
}

// WatchChanges registers a Drive push-notification channel that POSTs to
// webhookURL when files change. channelID and channelToken are caller-supplied;
// channelToken is echoed back by Drive in X-Goog-Channel-Token headers and
// should be used to authenticate incoming webhook requests. ttl controls the
// requested channel lifetime (Drive may grant a shorter TTL).
//
// Returns the Drive-assigned channel ID, resource ID, and expiration time.
func (s *Storage) WatchChanges(
	ctx context.Context,
	pageToken, channelID, webhookURL, channelToken string,
	ttl time.Duration,
) (string, string, time.Time, error) {
	c, err := s.driveClient(ctx)
	if err != nil {
		return "", "", time.Time{}, err
	}

	resp, err := c.svc.Changes.Watch(pageToken, &gdrive.Channel{
		Id:         channelID,
		Type:       "web_hook",
		Address:    webhookURL,
		Token:      channelToken,
		Expiration: time.Now().Add(ttl).UnixMilli(),
	}).Context(ctx).Do()
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("drive: watching changes: %w", err)
	}

	return resp.Id, resp.ResourceId, time.UnixMilli(resp.Expiration).UTC(), nil
}

// StopChannel cancels an active Drive push-notification channel. It is safe to
// call on an already-expired or unknown channel — Drive returns a 404 in that
// case which callers should treat as success. Treat non-nil errors as advisory.
func (s *Storage) StopChannel(ctx context.Context, channelID, resourceID string) error {
	c, err := s.driveClient(ctx)
	if err != nil {
		return err
	}
	if err := c.svc.Channels.Stop(&gdrive.Channel{
		Id:         channelID,
		ResourceId: resourceID,
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("drive: stopping channel %s: %w", channelID, err)
	}
	return nil
}
