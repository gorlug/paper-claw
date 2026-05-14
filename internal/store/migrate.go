package store

import (
	"database/sql"
	"fmt"
)

// migrate applies the embedded schema to the database idempotently. Each
// statement is safe to run on an already-migrated database (CREATE TABLE IF
// NOT EXISTS, INSERT OR IGNORE).
func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_version (
			version  INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS documents (
			content_hash    TEXT PRIMARY KEY,
			entry_name      TEXT NOT NULL,
			drive_file_id   TEXT NOT NULL DEFAULT '',
			source_filename TEXT NOT NULL DEFAULT '',
			processed_at    TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS sync_state (
			id              INTEGER PRIMARY KEY CHECK (id = 1),
			start_page_token TEXT NOT NULL DEFAULT '',
			channel_id       TEXT NOT NULL DEFAULT '',
			channel_token    TEXT NOT NULL DEFAULT '',
			resource_id      TEXT NOT NULL DEFAULT '',
			expires_at       TEXT NOT NULL DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS oauth_token (
			id         INTEGER PRIMARY KEY CHECK (id = 1),
			token_json BLOB NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS daemon_status (
			id          INTEGER PRIMARY KEY CHECK (id = 1),
			last_poll_at TEXT NOT NULL DEFAULT '',
			last_error   TEXT NOT NULL DEFAULT '',
			processed    INTEGER NOT NULL DEFAULT 0,
			skipped      INTEGER NOT NULL DEFAULT 0,
			quarantine   INTEGER NOT NULL DEFAULT 0
		)`,

		`INSERT OR IGNORE INTO daemon_status (id) VALUES (1)`,

		`INSERT OR IGNORE INTO schema_version (version, applied_at)
			VALUES (1, datetime('now'))`,
	}

	for i, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration statement %d: %w", i, err)
		}
	}
	return nil
}
