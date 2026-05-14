package serve_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"paper-claw/internal/document"
	"paper-claw/internal/serve"
	"paper-claw/internal/storage/fake"
	"paper-claw/internal/store"
)

// minimalPDF is a stub that pdftotext will reject gracefully.
var minimalPDF = []byte("%PDF-1.4 stub")

// stubClassifier returns a fixed, valid Metadata.
type stubClassifier struct{}

func (s *stubClassifier) Classify(_ context.Context, _, filename, hash string, at time.Time) (document.Metadata, error) {
	return document.Metadata{
		ID:              hash,
		Type:            "invoice",
		DocumentDate:    "2026-05-01",
		Vendor:          "Acme",
		Summary:         "Test invoice",
		FileDescription: "acme-invoice",
		SourceFilename:  filename,
		ProcessedAt:     at.UTC().Format(time.RFC3339),
		ContentHash:     hash,
	}, nil
}

func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestWorker_ProcessesFile(t *testing.T) {
	st := openStore(t)
	storage := fake.New()
	storage.AddPDF("invoice.pdf", minimalPDF)

	w := serve.New(storage, &stubClassifier{}, st, nil, nil)

	// Enqueue before starting the worker so the scan executes as soon as Run
	// begins. processOne uses context.Background() with a per-file timeout, so
	// the fake PDF will be processed (OCR fails → quarantined) before Run loops
	// back to the select and sees ctx.Done().
	w.Enqueue()

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		w.Run(ctx)
	}()

	// Give the worker enough time to drain the scan and quarantine the file,
	// then cancel and wait for the goroutine to exit.
	time.Sleep(2 * time.Second)
	cancel()
	<-runDone // guaranteed: no concurrent access to storage after this point

	if len(storage.Processed)+len(storage.Quarantined) == 0 {
		t.Fatal("expected file to be processed or quarantined after worker ran")
	}
}

func TestWorker_Coalesces(t *testing.T) {
	st := openStore(t)
	storage := fake.New()

	w := serve.New(storage, &stubClassifier{}, st, nil, nil)

	// Enqueue more than the channel capacity — only one should be buffered.
	for range 5 {
		w.Enqueue()
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		w.Run(ctx)
	}()

	// Give the worker time to drain the channel and become idle.
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-runDone
	// Test passes as long as the worker didn't deadlock or panic.
}

func TestWorker_StopsOnCtxCancel(t *testing.T) {
	st := openStore(t)
	storage := fake.New()
	w := serve.New(storage, &stubClassifier{}, st, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after ctx cancel")
	}
}
