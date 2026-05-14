package local_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paper-claw/internal/document"
	"paper-claw/internal/storage/local"
)

func makeStorage(t *testing.T) (*local.Storage, string, string, string) {
	t.Helper()
	inbox := t.TempDir()
	library := t.TempDir()
	processed := t.TempDir()
	return local.New(inbox, library, processed), inbox, library, processed
}

func writePDF(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("%PDF-1.4 fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListInbox(t *testing.T) {
	s, inbox, _, _ := makeStorage(t)
	writePDF(t, inbox, "a.pdf")
	writePDF(t, inbox, "B.PDF")
	if err := os.WriteFile(filepath.Join(inbox, "note.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}

	names, err := s.ListInbox(context.Background())
	if err != nil {
		t.Fatalf("ListInbox: %v", err)
	}
	if len(names) != 2 {
		t.Errorf("got %d names, want 2: %v", len(names), names)
	}
}

func TestReadPDF_HashAndNoopCleanup(t *testing.T) {
	s, inbox, _, _ := makeStorage(t)
	writePDF(t, inbox, "doc.pdf")

	path, hash, cleanup, err := s.ReadPDF(context.Background(), "doc.pdf")
	if err != nil {
		t.Fatalf("ReadPDF: %v", err)
	}
	defer cleanup()

	if path != filepath.Join(inbox, "doc.pdf") {
		t.Errorf("path = %q; want inbox path", path)
	}
	if len(hash) != 64 {
		t.Errorf("hash len = %d; want 64", len(hash))
	}

	// cleanup should be a no-op — file should still exist.
	cleanup()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file gone after cleanup: %v", err)
	}
}

func TestReadPDF_Missing(t *testing.T) {
	s, _, _, _ := makeStorage(t)
	_, _, _, err := s.ReadPDF(context.Background(), "ghost.pdf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestIsDuplicate(t *testing.T) {
	s, inbox, library, _ := makeStorage(t)
	writePDF(t, inbox, "doc.pdf")

	_, hash, cleanup, _ := s.ReadPDF(context.Background(), "doc.pdf")
	cleanup()

	// No library entries → not a duplicate.
	dup, err := s.IsDuplicate(context.Background(), hash)
	if err != nil {
		t.Fatalf("IsDuplicate: %v", err)
	}
	if dup {
		t.Fatal("expected not duplicate")
	}

	// Write a metadata.json with the matching hash.
	entryDir := filepath.Join(library, "2026-05-01_acme_doc")
	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		t.Fatal(err)
	}
	m := document.Metadata{ContentHash: hash}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(entryDir, "metadata.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	dup, err = s.IsDuplicate(context.Background(), hash)
	if err != nil {
		t.Fatalf("IsDuplicate (after insert): %v", err)
	}
	if !dup {
		t.Fatal("expected duplicate after inserting matching metadata")
	}
}

func TestIsDuplicate_MissingLibrary(t *testing.T) {
	s := local.New("/nonexistent/inbox", "/nonexistent/library", "/nonexistent/proc")
	dup, err := s.IsDuplicate(context.Background(), "abc")
	if err != nil {
		t.Fatalf("unexpected error for missing library: %v", err)
	}
	if dup {
		t.Fatal("missing library should not be a duplicate")
	}
}

func TestWriteEntry_SidecarLayout(t *testing.T) {
	s, inbox, library, _ := makeStorage(t)
	pdfPath := writePDF(t, inbox, "doc.pdf")

	e := document.Entry{
		PDFPath:    pdfPath,
		Transcript: "Hello world",
		Metadata: document.Metadata{
			ID:              strings.Repeat("a", 64),
			Type:            "invoice",
			DocumentDate:    "2026-05-01",
			Vendor:          "Acme",
			Summary:         "test",
			FileDescription: "acme-invoice",
			SourceFilename:  "doc.pdf",
			ProcessedAt:     time.Now().UTC().Format(time.RFC3339),
			ContentHash:     strings.Repeat("b", 64),
		},
	}

	entryName, err := s.WriteEntry(context.Background(), e)
	if err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if entryName == "" {
		t.Fatal("empty entryName")
	}

	entryDir := filepath.Join(library, entryName)
	for _, want := range []string{"document.pdf", "transcript.md", "metadata.json"} {
		if _, err := os.Stat(filepath.Join(entryDir, want)); err != nil {
			t.Errorf("missing %s in entry dir", want)
		}
	}

	transcript, _ := os.ReadFile(filepath.Join(entryDir, "transcript.md")) //nolint:gosec
	if string(transcript) != "Hello world" {
		t.Errorf("transcript = %q; want %q", transcript, "Hello world")
	}
}

func TestWriteEntry_CollisionSuffix(t *testing.T) {
	s, inbox, library, _ := makeStorage(t)
	pdfPath := writePDF(t, inbox, "doc.pdf")

	base := document.Entry{
		PDFPath:    pdfPath,
		Transcript: "t",
		Metadata: document.Metadata{
			ID:              strings.Repeat("a", 64),
			Type:            "invoice",
			DocumentDate:    "2026-05-01",
			Vendor:          "Acme",
			Summary:         "s",
			FileDescription: "invoice",
			SourceFilename:  "doc.pdf",
			ProcessedAt:     time.Now().UTC().Format(time.RFC3339),
			ContentHash:     strings.Repeat("b", 64),
		},
	}

	name1, err := s.WriteEntry(context.Background(), base)
	if err != nil {
		t.Fatalf("first WriteEntry: %v", err)
	}

	// Second entry with same dir base should get a -2 suffix.
	name2, err := s.WriteEntry(context.Background(), base)
	if err != nil {
		t.Fatalf("second WriteEntry: %v", err)
	}

	if name1 == name2 {
		t.Errorf("expected different entry names; both = %q", name1)
	}
	if _, err := os.Stat(filepath.Join(library, name2)); err != nil {
		t.Errorf("second entry dir missing: %v", err)
	}
}

func TestMoveToProcessed_Success(t *testing.T) {
	s, inbox, _, processed := makeStorage(t)
	writePDF(t, inbox, "doc.pdf")

	if err := s.MoveToProcessed(context.Background(), "doc.pdf", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("MoveToProcessed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(inbox, "doc.pdf")); !errors.Is(err, os.ErrNotExist) {
		t.Error("original should be gone from inbox after move")
	}
	if _, err := os.Stat(filepath.Join(processed, "doc.pdf")); err != nil {
		t.Errorf("file not in processed dir: %v", err)
	}
}

func TestMoveToProcessed_CollisionHash(t *testing.T) {
	s, inbox, _, processed := makeStorage(t)
	writePDF(t, inbox, "doc.pdf")

	// Pre-existing file in processed dir.
	if err := os.WriteFile(filepath.Join(processed, "doc.pdf"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}

	hash := strings.Repeat("f", 64)
	if err := s.MoveToProcessed(context.Background(), "doc.pdf", hash); err != nil {
		t.Fatalf("MoveToProcessed: %v", err)
	}

	// Expect doc-ffffffff.pdf (first 8 chars of hash).
	want := "doc-ffffffff.pdf"
	if _, err := os.Stat(filepath.Join(processed, want)); err != nil {
		t.Errorf("expected %s in processed dir: %v", want, err)
	}
}

func TestQuarantine_WritesFiles(t *testing.T) {
	s, inbox, library, _ := makeStorage(t)
	writePDF(t, inbox, "bad.pdf")

	pe := document.ProcessingError{
		Stage:      "ocr",
		Err:        errors.New("pdftotext failed"),
		OccurredAt: time.Now().UTC(),
	}

	if err := s.Quarantine(context.Background(), "bad.pdf", pe); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}

	qDir := filepath.Join(library, "_quarantine", "bad.pdf")
	for _, f := range []string{"document.pdf", "processing_error.json"} {
		if _, err := os.Stat(filepath.Join(qDir, f)); err != nil {
			t.Errorf("missing %s in quarantine dir", f)
		}
	}

	data, _ := os.ReadFile(filepath.Join(qDir, "processing_error.json")) //nolint:gosec
	var rec struct {
		Stage string `json:"stage"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("unmarshal processing_error.json: %v", err)
	}
	if rec.Stage != "ocr" {
		t.Errorf("stage = %q; want %q", rec.Stage, "ocr")
	}
	if rec.Error == "" {
		t.Error("error field should not be empty")
	}
}
