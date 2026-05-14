// Package local provides a local-filesystem implementation of document.Storage.
// It is used by the CLI process command; the serve daemon uses the Drive impl.
package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"paper-claw/internal/document"
)

// Storage implements document.Storage using the local filesystem.
type Storage struct {
	InboxDir     string
	LibraryDir   string
	ProcessedDir string
}

// New constructs a local Storage.
func New(inboxDir, libraryDir, processedDir string) *Storage {
	return &Storage{
		InboxDir:     inboxDir,
		LibraryDir:   libraryDir,
		ProcessedDir: processedDir,
	}
}

// ListInbox returns the names of PDF files in the inbox directory.
func (s *Storage) ListInbox(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(s.InboxDir) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading inbox: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && isPDF(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// ReadPDF returns the inbox file path and its SHA-256 hash.
// The cleanup is a no-op because no temporary file is created.
func (s *Storage) ReadPDF(_ context.Context, name string) (string, string, func(), error) {
	path := filepath.Join(s.InboxDir, name)
	hash, err := hashFile(path)
	if err != nil {
		return "", "", func() {}, err
	}
	return path, hash, func() {}, nil
}

// IsDuplicate walks the library metadata files looking for a matching ContentHash.
func (s *Storage) IsDuplicate(_ context.Context, contentHash string) (bool, error) {
	entries, err := os.ReadDir(s.LibraryDir) //nolint:gosec
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_quarantine" {
			continue
		}
		m, err := loadMetadata(filepath.Join(s.LibraryDir, e.Name(), "metadata.json"))
		if err != nil {
			continue
		}
		if m.ContentHash == contentHash {
			return true, nil
		}
	}
	return false, nil
}

// WriteEntry creates the library entry directory and writes the three sidecar files.
func (s *Storage) WriteEntry(_ context.Context, e document.Entry) (string, error) {
	meta := e.Metadata
	desc := meta.FileDescription
	if desc == "" {
		desc = strings.TrimSuffix(meta.SourceFilename, filepath.Ext(meta.SourceFilename))
	}
	libraryDir := s.LibraryDir
	dirBase := document.FormatDirName(meta.DocumentDate, meta.Vendor, desc)
	dirName := document.UniqueDirName(dirBase, func(candidate string) bool {
		_, statErr := os.Stat(filepath.Join(libraryDir, candidate))
		return statErr == nil
	})
	entryDir := filepath.Join(s.LibraryDir, dirName)

	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		return "", err
	}
	if err := copyFile(e.PDFPath, filepath.Join(entryDir, "document.pdf")); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(entryDir, "transcript.md"), []byte(e.Transcript), 0o600); err != nil {
		return "", err
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(entryDir, "metadata.json"), append(metaJSON, '\n'), 0o600); err != nil {
		return "", err
	}
	return dirName, nil
}

// MoveToProcessed moves the inbox file to the processed directory.
// Failures are non-fatal: they are logged to stderr and nil is returned.
func (s *Storage) MoveToProcessed(_ context.Context, name, contentHash string) error {
	if err := os.MkdirAll(s.ProcessedDir, 0o750); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create processed dir: %v\n", err)
		return nil
	}
	src := filepath.Join(s.InboxDir, name)
	dst := filepath.Join(s.ProcessedDir, name)
	if _, err := os.Stat(dst); err == nil {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		dst = filepath.Join(s.ProcessedDir, fmt.Sprintf("%s-%s%s", stem, contentHash[:8], ext))
	}
	if err := os.Rename(src, dst); err != nil {
		if err2 := copyFile(src, dst); err2 == nil {
			_ = os.Remove(src)
		} else {
			fmt.Fprintf(os.Stderr, "warning: could not move %s to processed: %v\n", name, err)
		}
	}
	return nil
}

// Quarantine copies the inbox file and a processing_error.json to the quarantine area.
func (s *Storage) Quarantine(_ context.Context, name string, pe document.ProcessingError) error {
	qDir := filepath.Join(s.LibraryDir, "_quarantine", name)
	if err := os.MkdirAll(qDir, 0o750); err != nil {
		return err
	}
	_ = copyFile(filepath.Join(s.InboxDir, name), filepath.Join(qDir, "document.pdf"))

	errRec := struct {
		Stage      string `json:"stage"`
		Error      string `json:"error"`
		RetryHint  string `json:"retry_hint"`
		OccurredAt string `json:"occurred_at"`
	}{
		Stage:      pe.Stage,
		Error:      pe.Err.Error(),
		RetryHint:  retryHint(pe.Stage),
		OccurredAt: pe.OccurredAt.UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(errRec, "", "  ")
	if err != nil {
		return nil
	}
	_ = os.WriteFile(filepath.Join(qDir, "processing_error.json"), append(data, '\n'), 0o600)
	return nil
}

// --- helpers -----------------------------------------------------------------

func isPDF(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".pdf")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func loadMetadata(path string) (document.Metadata, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return document.Metadata{}, err
	}
	var m document.Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return document.Metadata{}, err
	}
	return m, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func retryHint(stage string) string {
	switch stage {
	case "ocr":
		return "Check that the PDF contains extractable text or try re-scanning."
	case "classify":
		return "Check ANTHROPIC_API_KEY and network connectivity; re-run process."
	case "schema_validate":
		return "LLM returned unexpected output; re-run process or file a bug."
	default:
		return "Check disk space and permissions; re-run process."
	}
}
