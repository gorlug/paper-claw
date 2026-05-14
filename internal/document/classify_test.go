package document_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"papwer-claw/internal/document"
)

// stubClassifier is an in-process Classifier used in tests.
type stubClassifier struct {
	meta document.Metadata
	err  error
}

func (s *stubClassifier) Classify(_ context.Context, _, _, hash string, at time.Time) (document.Metadata, error) {
	if s.err != nil {
		return document.Metadata{}, s.err
	}
	m := s.meta
	m.ID = hash
	m.ContentHash = hash
	m.ProcessedAt = at.UTC().Format(time.RFC3339)
	return m, nil
}

// Verify stubClassifier satisfies the Classifier interface at compile time.
var _ document.Classifier = (*stubClassifier)(nil)

func TestStubClassifier_ReturnsConfiguredMetadata(t *testing.T) {
	stub := &stubClassifier{meta: document.Metadata{
		Type:            "utility_bill",
		DocumentDate:    "2026-04-01",
		Vendor:          "Stadtwerke",
		Summary:         "Electricity bill.",
		FileDescription: "strom-rechnung",
		SourceFilename:  "stadtwerke.pdf",
	}}
	hash := "a3b4c5d6e7f8a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a1b2c3d4e5f6a7b8"
	at := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)

	meta, err := stub.Classify(context.Background(), "transcript", "stadtwerke.pdf", hash, at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.ID != hash {
		t.Errorf("ID = %q, want %q", meta.ID, hash)
	}
	if meta.ContentHash != hash {
		t.Errorf("ContentHash = %q, want %q", meta.ContentHash, hash)
	}
	if meta.ProcessedAt != "2026-05-14T10:00:00Z" {
		t.Errorf("ProcessedAt = %q, want 2026-05-14T10:00:00Z", meta.ProcessedAt)
	}
	if meta.FileDescription != "strom-rechnung" {
		t.Errorf("FileDescription = %q, want strom-rechnung", meta.FileDescription)
	}
}

func TestStubClassifier_PropagatesError(t *testing.T) {
	sentinel := errors.New("api failure")
	stub := &stubClassifier{err: sentinel}
	_, err := stub.Classify(context.Background(), "", "", "hash", time.Now())
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}
