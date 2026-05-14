// Package store manages the SQLite database that holds all paperclaw daemon state:
// the content-hash dedup index, OAuth token, Drive Changes sync state, and run counters.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // register the sqlite3 driver
)

// DB wraps a single SQLite connection pool with paperclaw-specific accessors.
type DB struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at the given path, applies WAL
// mode and a busy timeout, runs the embedded schema migrations idempotently,
// and returns a ready-to-use DB.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	// One writer at a time; WAL allows concurrent reads alongside it.
	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrating store: %w", err)
	}
	return &DB{db: db}, nil
}

// Close releases the underlying database connection.
func (s *DB) Close() error {
	return s.db.Close()
}

// --- Documents / dedup index -------------------------------------------------

// HasHash reports whether a document with the given SHA-256 content hash has
// already been processed successfully.
func (s *DB) HasHash(ctx context.Context, contentHash string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE content_hash = ?`, contentHash,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DocumentRecord is written once a PDF has been processed successfully.
type DocumentRecord struct {
	ContentHash    string
	EntryName      string
	DriveFileID    string
	SourceFilename string
	ProcessedAt    time.Time
}

// PutDocument inserts a processed-document record. Duplicate content_hash is
// silently ignored (INSERT OR IGNORE) so the operation is idempotent.
func (s *DB) PutDocument(ctx context.Context, r DocumentRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO documents
			(content_hash, entry_name, drive_file_id, source_filename, processed_at)
		VALUES (?, ?, ?, ?, ?)`,
		r.ContentHash, r.EntryName, r.DriveFileID, r.SourceFilename,
		r.ProcessedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// --- OAuth token -------------------------------------------------------------

// GetToken returns the raw JSON token blob, or (nil, nil) if none is stored.
func (s *DB) GetToken(ctx context.Context) ([]byte, error) {
	var tok []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT token_json FROM oauth_token WHERE id = 1`,
	).Scan(&tok)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return tok, err
}

// PutToken replaces the stored OAuth token blob.
func (s *DB) PutToken(ctx context.Context, tokenJSON []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oauth_token (id, token_json) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET token_json = excluded.token_json`,
		tokenJSON,
	)
	return err
}

// --- Drive Changes sync state ------------------------------------------------

// SyncState holds the Drive Changes API page token and active push channel.
type SyncState struct {
	StartPageToken string
	ChannelID      string
	ChannelToken   string
	ResourceID     string
	ExpiresAt      time.Time
}

// GetSyncState returns the current sync state, or a zero SyncState if not set.
func (s *DB) GetSyncState(ctx context.Context) (SyncState, error) {
	var ss SyncState
	var expiresAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT start_page_token, channel_id, channel_token, resource_id, expires_at
		FROM sync_state WHERE id = 1`,
	).Scan(&ss.StartPageToken, &ss.ChannelID, &ss.ChannelToken, &ss.ResourceID, &expiresAt)
	if err == sql.ErrNoRows {
		return SyncState{}, nil
	}
	if err != nil {
		return SyncState{}, err
	}
	if expiresAt != "" {
		ss.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
	}
	return ss, nil
}

// PutStartPageToken persists the Drive Changes start page token, leaving
// channel fields unchanged.
func (s *DB) PutStartPageToken(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_state (id, start_page_token) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET start_page_token = excluded.start_page_token`,
		token,
	)
	return err
}

// PutChannel persists the Drive push-notification channel metadata.
func (s *DB) PutChannel(ctx context.Context, channelID, channelToken, resourceID string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_state (id, channel_id, channel_token, resource_id, expires_at)
		VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			channel_id    = excluded.channel_id,
			channel_token = excluded.channel_token,
			resource_id   = excluded.resource_id,
			expires_at    = excluded.expires_at`,
		channelID, channelToken, resourceID, expiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

// --- Daemon status / counters ------------------------------------------------

// Status is the daemon's aggregate run status written after every poll.
type Status struct {
	LastPollAt time.Time
	LastError  string
	Processed  int64
	Skipped    int64
	Quarantine int64
}

// GetStatus returns the current daemon status, or a zero Status if never set.
func (s *DB) GetStatus(ctx context.Context) (Status, error) {
	var st Status
	var lastPollAt, lastError string
	err := s.db.QueryRowContext(ctx, `
		SELECT last_poll_at, last_error, processed, skipped, quarantine
		FROM daemon_status WHERE id = 1`,
	).Scan(&lastPollAt, &lastError, &st.Processed, &st.Skipped, &st.Quarantine)
	if err == sql.ErrNoRows {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	if lastPollAt != "" {
		st.LastPollAt, _ = time.Parse(time.RFC3339, lastPollAt)
	}
	st.LastError = lastError
	return st, nil
}

// RecordPoll marks a successful poll completion.
func (s *DB) RecordPoll(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daemon_status (id, last_poll_at, last_error) VALUES (1, ?, '')
		ON CONFLICT(id) DO UPDATE SET
			last_poll_at = excluded.last_poll_at,
			last_error   = ''`,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

// RecordError stores a poll error message.
func (s *DB) RecordError(ctx context.Context, msg string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daemon_status (id, last_poll_at, last_error) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_poll_at = excluded.last_poll_at,
			last_error   = excluded.last_error`,
		time.Now().UTC().Format(time.RFC3339), msg,
	)
	return err
}

// BumpCounter increments one of the named counters: "processed", "skipped", or "quarantine".
func (s *DB) BumpCounter(ctx context.Context, counter string) error {
	// counter is never user-supplied; it comes from the fixed strings above.
	validCounters := map[string]bool{"processed": true, "skipped": true, "quarantine": true}
	if !validCounters[counter] {
		return fmt.Errorf("store: unknown counter %q", counter)
	}
	// The column name is safe: validated against the allow-list above.
	//nolint:gosec
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`UPDATE daemon_status SET %s = %s + 1 WHERE id = 1`, counter, counter),
	)
	return err
}
