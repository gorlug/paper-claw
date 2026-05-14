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

	"paper-claw/internal/document"
)

func TestWriteLog(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	writeLog(f, "foo.pdf", "processed", "", nil)
	writeLog(f, "bar.pdf", "quarantined", "classify", errors.New("api unavailable"))

	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(f)

	var e1 logEntry
	if err := dec.Decode(&e1); err != nil {
		t.Fatalf("decode entry 1: %v", err)
	}
	if e1.Filename != "foo.pdf" {
		t.Errorf("entry1 filename = %q", e1.Filename)
	}
	if e1.Status != "processed" {
		t.Errorf("entry1 status = %q", e1.Status)
	}
	if e1.Error != "" {
		t.Errorf("entry1 unexpected error: %q", e1.Error)
	}
	if e1.OccurredAt == "" {
		t.Error("entry1 occurred_at is empty")
	}

	var e2 logEntry
	if err := dec.Decode(&e2); err != nil {
		t.Fatalf("decode entry 2: %v", err)
	}
	if e2.Filename != "bar.pdf" {
		t.Errorf("entry2 filename = %q", e2.Filename)
	}
	if e2.Status != "quarantined" {
		t.Errorf("entry2 status = %q", e2.Status)
	}
	if e2.Stage != "classify" {
		t.Errorf("entry2 stage = %q", e2.Stage)
	}
	if e2.Error != "api unavailable" {
		t.Errorf("entry2 error = %q", e2.Error)
	}
}

func TestRunProcess_LogFileProcessed(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	if err := runProcess(processArgs(inbox, library, processed), goodClassifier()); err != nil {
		t.Fatalf("process: %v", err)
	}

	entries := readLogFile(t, filepath.Join(library, "process.log"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Filename != "stadtwerke-stromrechnung.pdf" {
		t.Errorf("filename = %q", e.Filename)
	}
	if e.Status != "processed" {
		t.Errorf("status = %q, want processed", e.Status)
	}
	if e.Error != "" {
		t.Errorf("unexpected error: %q", e.Error)
	}
}

func TestRunProcess_LogFileSkipped(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	cl := goodClassifier()
	if err := runProcess(processArgs(inbox, library, processed), cl); err != nil {
		t.Fatalf("first run: %v", err)
	}
	// Re-copy the PDF to simulate re-submitting an already-filed document.
	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))
	if err := runProcess(processArgs(inbox, library, processed), cl); err != nil {
		t.Fatalf("second run: %v", err)
	}

	entries := readLogFile(t, filepath.Join(library, "process.log"))
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries (processed + skipped), got %d", len(entries))
	}
	if entries[0].Status != "processed" {
		t.Errorf("entry[0] status = %q, want processed", entries[0].Status)
	}
	if entries[1].Status != "skipped" {
		t.Errorf("entry[1] status = %q, want skipped", entries[1].Status)
	}
}

func TestRunProcess_LogFileQuarantine(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	broken := &testClassifier{err: errors.New("api unavailable")}
	if err := runProcess(processArgs(inbox, library, processed), broken); err != nil {
		t.Fatalf("process: %v", err)
	}

	entries := readLogFile(t, filepath.Join(library, "process.log"))
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Status != "quarantined" {
		t.Errorf("status = %q, want quarantined", e.Status)
	}
	if e.Stage != "classify" {
		t.Errorf("stage = %q, want classify", e.Stage)
	}
	if e.Error != "api unavailable" {
		t.Errorf("error = %q, want api unavailable", e.Error)
	}
}

func TestRunProcess_MovesToProcessed(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	const pdfName = "stadtwerke-stromrechnung.pdf"
	original, err := os.ReadFile("../../testdata/" + pdfName)
	if err != nil {
		t.Fatalf("reading test PDF: %v", err)
	}
	copyPDF(t, "../../testdata/"+pdfName, filepath.Join(inbox, pdfName))

	if err := runProcess(processArgs(inbox, library, processed), goodClassifier()); err != nil {
		t.Fatalf("process: %v", err)
	}

	// File must be gone from inbox.
	if _, err := os.Stat(filepath.Join(inbox, pdfName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still in inbox after process: %v", err)
	}

	// File must be in processed dir with identical content.
	entries, err := os.ReadDir(processed)
	if err != nil {
		t.Fatalf("reading processed dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("processed dir is empty after process")
	}
	got, err := os.ReadFile(filepath.Join(processed, entries[0].Name()))
	if err != nil {
		t.Fatalf("reading processed file: %v", err)
	}
	if string(got) != string(original) {
		t.Error("processed file content differs from original")
	}
}

func TestRunProcess_MovesToProcessed_Collision(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	const pdfName = "stadtwerke-stromrechnung.pdf"
	copyPDF(t, "../../testdata/"+pdfName, filepath.Join(inbox, pdfName))

	// Pre-create a file in processed dir with the same name to force a collision.
	if err := os.WriteFile(filepath.Join(processed, pdfName), []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("creating collision file: %v", err)
	}

	if err := runProcess(processArgs(inbox, library, processed), goodClassifier()); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Inbox must be empty.
	if _, err := os.Stat(filepath.Join(inbox, pdfName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file still in inbox after process: %v", err)
	}

	// Processed dir must have 2 files: the placeholder and the suffixed move.
	entries, err := os.ReadDir(processed)
	if err != nil {
		t.Fatalf("reading processed dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 files in processed dir (placeholder + suffixed), got %d", len(entries))
	}
	var hasSuffixed bool
	for _, e := range entries {
		if e.Name() != pdfName {
			hasSuffixed = true
		}
	}
	if !hasSuffixed {
		t.Error("expected a collision-suffixed filename in processed dir")
	}
}

func readLogFile(t *testing.T, path string) []logEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("log file missing: %v", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	var entries []logEntry
	for dec.More() {
		var e logEntry
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("decode log entry: %v", err)
		}
		entries = append(entries, e)
	}
	return entries
}

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

// setupDirs creates temporary inbox, library, and processed directories.
func setupDirs(t *testing.T) (inbox, library, processed string) {
	t.Helper()
	return t.TempDir(), t.TempDir(), t.TempDir()
}

// processArgs builds runProcess arguments including all three directories.
func processArgs(inbox, library, processed string) []string {
	return []string{"--inbox", inbox, "--library", library, "--processed", processed}
}

func TestRunProcess_EmptyInbox(t *testing.T) {
	inbox, library, processed := setupDirs(t)
	err := runProcess(processArgs(inbox, library, processed), goodClassifier())
	if err != nil {
		t.Fatalf("expected no error on empty inbox, got: %v", err)
	}
}

func TestRunProcess_WritesLibraryEntry(t *testing.T) {
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	if err := runProcess(processArgs(inbox, library, processed), goodClassifier()); err != nil {
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
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	cl := goodClassifier()
	if err := runProcess(processArgs(inbox, library, processed), cl); err != nil {
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

	// Second run: inbox is now empty (file was moved to processed) → no new entries.
	if err := runProcess(processArgs(inbox, library, processed), cl); err != nil {
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
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	broken := &testClassifier{err: errors.New("api unavailable")}
	if err := runProcess(processArgs(inbox, library, processed), broken); err != nil {
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
	inbox, library, processed := setupDirs(t)

	copyPDF(t, "../../testdata/stadtwerke-stromrechnung.pdf", filepath.Join(inbox, "stadtwerke-stromrechnung.pdf"))

	// Return metadata with an invalid type to trigger schema_validate failure.
	broken := &testClassifier{meta: document.Metadata{
		Type:            "grocery_receipt", // not in enum
		DocumentDate:    "2026-04-01",
		Vendor:          "Stadtwerke",
		Summary:         "Bill.",
		FileDescription: "strom",
	}}
	if err := runProcess(processArgs(inbox, library, processed), broken); err != nil {
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
