package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"paper-claw/internal/store"
)

func openDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrations_Idempotent(t *testing.T) {
	// Opening the same file twice must not fail.
	dir := t.TempDir()
	path := filepath.Join(dir, "dup.db")
	db1, err := store.Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = db1.Close()

	db2, err := store.Open(path)
	if err != nil {
		t.Fatalf("second Open (idempotent migration): %v", err)
	}
	_ = db2.Close()
}

func TestHasHash_PutDocument(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	const hash = "aaaa" + "bbbb" + "cccc" + "dddd" + "eeee" + "ffff" + "0000" + "1111" // 64 hex chars

	got, err := db.HasHash(ctx, hash)
	if err != nil {
		t.Fatalf("HasHash: %v", err)
	}
	if got {
		t.Fatal("expected no hash before insert")
	}

	r := store.DocumentRecord{
		ContentHash:    hash,
		EntryName:      "2026-05-01_acme_invoice",
		DriveFileID:    "drive-file-123",
		SourceFilename: "invoice.pdf",
		ProcessedAt:    time.Now().UTC(),
	}
	if err := db.PutDocument(ctx, r); err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	got, err = db.HasHash(ctx, hash)
	if err != nil {
		t.Fatalf("HasHash after insert: %v", err)
	}
	if !got {
		t.Fatal("expected hash found after insert")
	}
}

func TestPutDocument_Idempotent(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	r := store.DocumentRecord{
		ContentHash: "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "deadbe00",
		EntryName:   "entry",
		ProcessedAt: time.Now().UTC(),
	}
	if err := db.PutDocument(ctx, r); err != nil {
		t.Fatalf("first PutDocument: %v", err)
	}
	// Second insert with same hash must not fail.
	if err := db.PutDocument(ctx, r); err != nil {
		t.Fatalf("second PutDocument (idempotent): %v", err)
	}
}

func TestGetToken_EmptyBeforeStore(t *testing.T) {
	db := openDB(t)
	tok, err := db.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if tok != nil {
		t.Fatalf("expected nil token before PutToken; got %q", tok)
	}
}

func TestPutToken_GetToken(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	want := []byte(`{"access_token":"tok","token_type":"Bearer"}`)

	if err := db.PutToken(ctx, want); err != nil {
		t.Fatalf("PutToken: %v", err)
	}
	got, err := db.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("GetToken = %q; want %q", got, want)
	}

	// Overwrite and read again.
	want2 := []byte(`{"access_token":"tok2"}`)
	if err := db.PutToken(ctx, want2); err != nil {
		t.Fatalf("PutToken (overwrite): %v", err)
	}
	got2, err := db.GetToken(ctx)
	if err != nil {
		t.Fatalf("GetToken (after overwrite): %v", err)
	}
	if string(got2) != string(want2) {
		t.Errorf("GetToken after overwrite = %q; want %q", got2, want2)
	}
}

func TestSyncState_Empty(t *testing.T) {
	db := openDB(t)
	ss, err := db.GetSyncState(context.Background())
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if ss.StartPageToken != "" || ss.ChannelID != "" {
		t.Errorf("expected empty SyncState; got %+v", ss)
	}
}

func TestPutStartPageToken(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if err := db.PutStartPageToken(ctx, "page-1"); err != nil {
		t.Fatalf("PutStartPageToken: %v", err)
	}
	ss, err := db.GetSyncState(ctx)
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if ss.StartPageToken != "page-1" {
		t.Errorf("StartPageToken = %q; want page-1", ss.StartPageToken)
	}

	// Overwrite.
	if err := db.PutStartPageToken(ctx, "page-2"); err != nil {
		t.Fatalf("PutStartPageToken overwrite: %v", err)
	}
	ss2, _ := db.GetSyncState(ctx)
	if ss2.StartPageToken != "page-2" {
		t.Errorf("StartPageToken after overwrite = %q; want page-2", ss2.StartPageToken)
	}
}

func TestPutChannel(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	exp := time.Now().UTC().Add(7 * 24 * time.Hour).Truncate(time.Second)

	if err := db.PutChannel(ctx, "ch-id", "ch-tok", "res-id", exp); err != nil {
		t.Fatalf("PutChannel: %v", err)
	}
	ss, err := db.GetSyncState(ctx)
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if ss.ChannelID != "ch-id" {
		t.Errorf("ChannelID = %q; want ch-id", ss.ChannelID)
	}
	if ss.ChannelToken != "ch-tok" {
		t.Errorf("ChannelToken = %q; want ch-tok", ss.ChannelToken)
	}
	if !ss.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt = %v; want %v", ss.ExpiresAt, exp)
	}
}

func TestGetStatus_Default(t *testing.T) {
	db := openDB(t)
	st, err := db.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Processed != 0 || st.Skipped != 0 || st.Quarantine != 0 {
		t.Errorf("expected zero counters; got %+v", st)
	}
}

func TestRecordPoll_RecordError(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	if err := db.RecordPoll(ctx); err != nil {
		t.Fatalf("RecordPoll: %v", err)
	}
	st, _ := db.GetStatus(ctx)
	if st.LastPollAt.IsZero() {
		t.Error("LastPollAt should be set after RecordPoll")
	}
	if st.LastError != "" {
		t.Errorf("LastError should be empty after RecordPoll; got %q", st.LastError)
	}

	if err := db.RecordError(ctx, "something went wrong"); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	st2, _ := db.GetStatus(ctx)
	if st2.LastError != "something went wrong" {
		t.Errorf("LastError = %q; want %q", st2.LastError, "something went wrong")
	}
}

func TestBumpCounter(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	for range 3 {
		if err := db.BumpCounter(ctx, "processed"); err != nil {
			t.Fatalf("BumpCounter processed: %v", err)
		}
	}
	for range 2 {
		if err := db.BumpCounter(ctx, "skipped"); err != nil {
			t.Fatalf("BumpCounter skipped: %v", err)
		}
	}
	if err := db.BumpCounter(ctx, "quarantine"); err != nil {
		t.Fatalf("BumpCounter quarantine: %v", err)
	}

	st, err := db.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Processed != 3 {
		t.Errorf("Processed = %d; want 3", st.Processed)
	}
	if st.Skipped != 2 {
		t.Errorf("Skipped = %d; want 2", st.Skipped)
	}
	if st.Quarantine != 1 {
		t.Errorf("Quarantine = %d; want 1", st.Quarantine)
	}
}

func TestBumpCounter_InvalidName(t *testing.T) {
	db := openDB(t)
	if err := db.BumpCounter(context.Background(), "not_a_counter"); err == nil {
		t.Error("expected error for unknown counter name")
	}
}
