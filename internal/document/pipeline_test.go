package document_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"paper-claw/internal/document"
	"paper-claw/internal/storage/fake"
)

// validMeta returns a Metadata whose classifier-supplied fields will all pass
// schema validation. ID, ContentHash, ProcessedAt, SourceFilename are set by
// the pipeline / stubClassifier.
func validMeta() document.Metadata {
	return document.Metadata{
		Type:            "invoice",
		DocumentDate:    "2026-05-01",
		Vendor:          "Acme",
		Summary:         "A test invoice.",
		FileDescription: "test-invoice",
	}
}

func fakeExtract(_ context.Context, _ string) (string, error) {
	return "fake transcript content", nil
}

func hashOf(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestProcessOne_HappyPath(t *testing.T) {
	pdfBytes := []byte("%PDF fake")
	store := fake.New()
	store.AddPDF("invoice.pdf", pdfBytes)

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"invoice.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusProcessed {
		t.Errorf("status = %q; want %q", result.Status, document.StatusProcessed)
	}
	if result.Hash != hashOf(pdfBytes) {
		t.Errorf("hash mismatch")
	}
	if result.EntryName == "" {
		t.Error("expected non-empty EntryName")
	}
	if len(store.Library) != 1 {
		t.Errorf("library entries = %d; want 1", len(store.Library))
	}
	if len(store.Processed) != 1 || store.Processed[0] != "invoice.pdf" {
		t.Errorf("processed = %v; want [invoice.pdf]", store.Processed)
	}
	if len(store.Quarantined) != 0 {
		t.Errorf("unexpected quarantine records: %v", store.Quarantined)
	}
}

func TestProcessOne_Duplicate(t *testing.T) {
	pdfBytes := []byte("%PDF dup")
	store := fake.New()
	store.AddPDF("dup.pdf", pdfBytes)

	// Pre-populate library with the same hash.
	store.Library["existing"] = document.Entry{
		Metadata: document.Metadata{ContentHash: hashOf(pdfBytes)},
	}

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"dup.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusSkipped {
		t.Errorf("status = %q; want %q", result.Status, document.StatusSkipped)
	}
	if len(store.Processed) != 0 {
		t.Error("duplicate should not be moved to processed")
	}
	if len(store.Quarantined) != 0 {
		t.Error("duplicate should not be quarantined")
	}
}

func TestProcessOne_OCRError(t *testing.T) {
	store := fake.New()
	store.AddPDF("bad.pdf", []byte("%PDF bad"))

	failExtract := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("pdftotext: empty transcript")
	}

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"bad.pdf",
		document.WithExtractor(failExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusQuarantined {
		t.Errorf("status = %q; want %q", result.Status, document.StatusQuarantined)
	}
	if result.Stage != "ocr" {
		t.Errorf("stage = %q; want %q", result.Stage, "ocr")
	}
	if len(store.Quarantined) == 0 || store.Quarantined[0].PE.Stage != "ocr" {
		t.Errorf("quarantine record missing or wrong stage: %v", store.Quarantined)
	}
}

func TestProcessOne_ClassifyError(t *testing.T) {
	store := fake.New()
	store.AddPDF("test.pdf", []byte("%PDF test"))

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{err: errors.New("claude API timeout")},
		"test.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusQuarantined {
		t.Errorf("status = %q; want %q", result.Status, document.StatusQuarantined)
	}
	if result.Stage != "classify" {
		t.Errorf("stage = %q; want %q", result.Stage, "classify")
	}
	if len(store.Quarantined) == 0 || store.Quarantined[0].PE.Stage != "classify" {
		t.Errorf("quarantine record missing or wrong stage: %v", store.Quarantined)
	}
}

func TestProcessOne_SchemaValidateError(t *testing.T) {
	store := fake.New()
	store.AddPDF("test.pdf", []byte("%PDF test"))

	// An invalid document type will fail schema validation.
	bad := document.Metadata{
		Type:         "not-a-valid-type",
		DocumentDate: "2026-05-01",
		Vendor:       "Acme",
		Summary:      "summary",
	}

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: bad},
		"test.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusQuarantined {
		t.Errorf("status = %q; want %q", result.Status, document.StatusQuarantined)
	}
	if result.Stage != "schema_validate" {
		t.Errorf("stage = %q; want %q", result.Stage, "schema_validate")
	}
}

func TestProcessOne_WriteEntryError(t *testing.T) {
	store := fake.New()
	store.AddPDF("test.pdf", []byte("%PDF test"))
	store.WriteEntryErr = errors.New("disk full")

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"test.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusQuarantined {
		t.Errorf("status = %q; want %q", result.Status, document.StatusQuarantined)
	}
	if result.Stage != "library_write" {
		t.Errorf("stage = %q; want %q", result.Stage, "library_write")
	}
}

func TestProcessOne_IsDuplicateError(t *testing.T) {
	store := fake.New()
	store.AddPDF("test.pdf", []byte("%PDF test"))
	store.IsDuplicateErr = errors.New("db unavailable")

	_, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"test.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err == nil {
		t.Fatal("expected non-nil error from IsDuplicate infra failure; got nil")
	}
}

func TestProcessOne_ReadPDFError(t *testing.T) {
	store := fake.New()
	// No PDF added — ReadPDF returns an error.

	result, err := document.ProcessOne(
		context.Background(),
		store,
		&stubClassifier{meta: validMeta()},
		"missing.pdf",
		document.WithExtractor(fakeExtract),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != document.StatusQuarantined {
		t.Errorf("status = %q; want %q", result.Status, document.StatusQuarantined)
	}
	if result.Stage != "library_write" {
		t.Errorf("stage = %q; want %q", result.Stage, "library_write")
	}
}
