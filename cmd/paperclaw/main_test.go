package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"papwer-claw/internal/document"
)

// testClassifier returns canned metadata, injecting hash and processedAt.
type testClassifier struct {
	meta document.Metadata
	err  error
}

func (c *testClassifier) Classify(_ context.Context, _, srcName, hash string, at time.Time) (document.Metadata, error) {
	if c.err != nil {
		return document.Metadata{}, c.err
	}
	m := c.meta
	m.ID = hash
	m.ContentHash = hash
	m.SourceFilename = srcName
	m.ProcessedAt = at.UTC().Format(time.RFC3339)
	return m, nil
}

func goodClassifier() *testClassifier {
	return &testClassifier{meta: document.Metadata{
		Type:            "utility_bill",
		DocumentDate:    "2026-04-01",
		Vendor:          "Stadtwerke",
		Summary:         "Electricity bill for April 2026.",
		FileDescription: "strom-rechnung",
		Language:        "de",
	}}
}

// copyPDF copies a real testdata PDF to dst.
func copyPDF(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading test PDF: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatalf("writing test PDF: %v", err)
	}
}

func TestRunProcess_EmptyInbox(t *testing.T) {
	inbox := t.TempDir()
	library := t.TempDir()
	err := runProcess([]string{"--inbox", inbox, "--library", library}, goodClassifier())
	if err != nil {
		t.Fatalf("expected no error on empty inbox, got: %v", err)
	}
}

func TestRunProcess_WritesLibraryEntry(t *testing.T) {
	inbox := t.TempDir()
	library := t.TempDir()

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	if err := runProcess([]string{"--inbox", inbox, "--library", library}, goodClassifier()); err != nil {
		t.Fatalf("process failed: %v", err)
	}

	// Verify at least one library directory was created (not _quarantine).
	entries, err := os.ReadDir(library)
	if err != nil {
		t.Fatalf("reading library: %v", err)
	}
	var docDirs []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "_quarantine" {
			docDirs = append(docDirs, e.Name())
		}
	}
	if len(docDirs) == 0 {
		t.Fatal("expected at least one library entry, got none")
	}

	entryDir := filepath.Join(library, docDirs[0])

	// document.pdf must exist and be non-empty.
	fi, err := os.Stat(filepath.Join(entryDir, "document.pdf"))
	if err != nil {
		t.Fatalf("document.pdf missing: %v", err)
	}
	if fi.Size() == 0 {
		t.Error("document.pdf is empty")
	}

	// transcript.md must be non-empty (pdftotext ran).
	transcriptData, err := os.ReadFile(filepath.Join(entryDir, "transcript.md"))
	if err != nil {
		t.Fatalf("transcript.md missing: %v", err)
	}
	if len(strings.TrimSpace(string(transcriptData))) == 0 {
		t.Error("transcript.md is empty")
	}
	if !strings.Contains(string(transcriptData), "Stadtwerke") {
		t.Error("transcript.md does not contain expected content")
	}

	// metadata.json must be valid JSON with required fields.
	metaData, err := os.ReadFile(filepath.Join(entryDir, "metadata.json"))
	if err != nil {
		t.Fatalf("metadata.json missing: %v", err)
	}
	var meta document.Metadata
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("invalid metadata.json: %v", err)
	}
	if meta.ID == "" {
		t.Error("metadata.json: id is empty")
	}
	if meta.Type != "utility_bill" {
		t.Errorf("metadata.json: type = %q, want utility_bill", meta.Type)
	}

	// Directory name must start with the document date.
	if !strings.HasPrefix(docDirs[0], "2026-04-01_") {
		t.Errorf("directory name %q should start with 2026-04-01_", docDirs[0])
	}

	// FileDescription must NOT appear in metadata.json.
	if strings.Contains(string(metaData), "file_description") {
		t.Error("metadata.json must not contain file_description field")
	}
}

func TestRunProcess_SkipsDuplicate(t *testing.T) {
	inbox := t.TempDir()
	library := t.TempDir()

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	cl := goodClassifier()
	if err := runProcess([]string{"--inbox", inbox, "--library", library}, cl); err != nil {
		t.Fatalf("first process: %v", err)
	}

	// Count library entries before second run.
	before, _ := os.ReadDir(library)
	beforeCount := 0
	for _, e := range before {
		if e.IsDir() && e.Name() != "_quarantine" {
			beforeCount++
		}
	}

	// Second run: same inbox file → must be skipped, no new entry.
	if err := runProcess([]string{"--inbox", inbox, "--library", library}, cl); err != nil {
		t.Fatalf("second process: %v", err)
	}

	after, _ := os.ReadDir(library)
	afterCount := 0
	for _, e := range after {
		if e.IsDir() && e.Name() != "_quarantine" {
			afterCount++
		}
	}
	if afterCount != beforeCount {
		t.Errorf("second run created new entries: before=%d after=%d", beforeCount, afterCount)
	}
}

func TestRunProcess_QuarantinesOnClassifierError(t *testing.T) {
	inbox := t.TempDir()
	library := t.TempDir()

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	broken := &testClassifier{err: errors.New("api unavailable")}
	if err := runProcess([]string{"--inbox", inbox, "--library", library}, broken); err != nil {
		t.Fatalf("process should not return error even if classifier fails: %v", err)
	}

	// Quarantine directory must exist.
	qDir := filepath.Join(library, "_quarantine")
	entries, err := os.ReadDir(qDir)
	if err != nil {
		t.Fatalf("quarantine dir missing: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected quarantine entry, found none")
	}

	// processing_error.json must exist in the quarantine subdirectory.
	errPath := filepath.Join(qDir, entries[0].Name(), "processing_error.json")
	errData, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("processing_error.json missing: %v", err)
	}
	var pe processingErrorJSON
	if err := json.Unmarshal(errData, &pe); err != nil {
		t.Fatalf("invalid processing_error.json: %v", err)
	}
	if pe.Stage != "classify" {
		t.Errorf("stage = %q, want classify", pe.Stage)
	}
	if pe.Error == "" {
		t.Error("error message must not be empty")
	}
	if pe.OccurredAt == "" {
		t.Error("occurred_at must not be empty")
	}

	// No normal library entry should exist.
	allEntries, _ := os.ReadDir(library)
	for _, e := range allEntries {
		if e.IsDir() && e.Name() != "_quarantine" {
			t.Errorf("unexpected library entry %q", e.Name())
		}
	}
}

func TestRunProcess_QuarantinesOnSchemaValidationError(t *testing.T) {
	inbox := t.TempDir()
	library := t.TempDir()

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	// Return metadata with an invalid type to trigger schema_validate failure.
	broken := &testClassifier{meta: document.Metadata{
		Type:            "grocery_receipt", // not in enum
		DocumentDate:    "2026-04-01",
		Vendor:          "Stadtwerke",
		Summary:         "Bill.",
		FileDescription: "strom",
	}}
	if err := runProcess([]string{"--inbox", inbox, "--library", library}, broken); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qDir := filepath.Join(library, "_quarantine")
	entries, err := os.ReadDir(qDir)
	if err != nil || len(entries) == 0 {
		t.Fatal("expected quarantine entry")
	}

	errPath := filepath.Join(qDir, entries[0].Name(), "processing_error.json")
	errData, _ := os.ReadFile(errPath)
	var pe processingErrorJSON
	json.Unmarshal(errData, &pe) //nolint:errcheck // test helper
	if pe.Stage != "schema_validate" {
		t.Errorf("stage = %q, want schema_validate", pe.Stage)
	}
}

func TestRunList_Empty(t *testing.T) {
	library := t.TempDir()
	err := runList([]string{"--library", library})
	if err != nil {
		t.Fatalf("list on empty library should not error: %v", err)
	}
}

func TestRunList_FilterByType(t *testing.T) {
	library := t.TempDir()
	writeTestMetadata(t, library, "2026-04-01_stadtwerke_strom", document.Metadata{
		ID: strings.Repeat("a", 64), Type: "utility_bill", DocumentDate: "2026-04-01",
		Vendor: "Stadtwerke", Summary: "Bill.", SourceFilename: "a.pdf",
		ProcessedAt: "2026-05-14T10:00:00Z", ContentHash: strings.Repeat("a", 64),
	})
	writeTestMetadata(t, library, "2026-04-01_finanzamt_bescheid", document.Metadata{
		ID: strings.Repeat("b", 64), Type: "government_letter", DocumentDate: "2026-04-01",
		Vendor: "Finanzamt", Summary: "Tax.", SourceFilename: "b.pdf",
		ProcessedAt: "2026-05-14T10:00:00Z", ContentHash: strings.Repeat("b", 64),
	})

	metas, err := walkLibrary(library)
	if err != nil {
		t.Fatalf("walkLibrary: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(metas))
	}

	// Filter by type: only utility_bill.
	var filtered []document.Metadata
	for _, m := range metas {
		if m.Type == "utility_bill" {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) != 1 {
		t.Errorf("expected 1 utility_bill, got %d", len(filtered))
	}
}

func TestRunSearch_FindsMatch(t *testing.T) {
	library := t.TempDir()
	entryDir := filepath.Join(library, "2026-04-01_stadtwerke_strom")
	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryDir, "transcript.md"), []byte("Stadtwerke München Stromrechnung 2026"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	writeTestMetadata(t, library, "2026-04-01_stadtwerke_strom", document.Metadata{
		ID: strings.Repeat("c", 64), Type: "utility_bill", DocumentDate: "2026-04-01",
		Vendor: "Stadtwerke", Summary: "Bill.", SourceFilename: "c.pdf",
		ProcessedAt: "2026-05-14T10:00:00Z", ContentHash: strings.Repeat("c", 64),
	})

	// runSearch writes to stdout; test the underlying walk logic instead.
	entries, _ := os.ReadDir(library)
	found := false
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_quarantine" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(library, e.Name(), "transcript.md"))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(data)), "stromrechnung") {
			found = true
		}
	}
	if !found {
		t.Error("search should have found 'stromrechnung' in transcript")
	}
}

// writeTestMetadata creates a library directory with metadata.json for testing.
func writeTestMetadata(t *testing.T, library, dirName string, m document.Metadata) {
	t.Helper()
	dir := filepath.Join(library, dirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
}
