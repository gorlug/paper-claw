// Package fake provides an in-memory document.Storage implementation for tests.
package fake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"paper-claw/internal/document"
)

// Storage is an in-memory document.Storage. All fields are exported so test
// code can read them directly after a ProcessOne call.
type Storage struct {
	// Inbox holds the raw PDF bytes keyed by filename.
	Inbox map[string][]byte
	// Library accumulates entries written by WriteEntry.
	Library map[string]document.Entry
	// Processed accumulates names moved by MoveToProcessed.
	Processed []string
	// Quarantined accumulates quarantine records from Quarantine calls.
	Quarantined []QuarantineRecord

	// IsDuplicateErr, if non-nil, is returned by IsDuplicate.
	IsDuplicateErr error
	// WriteEntryErr, if non-nil, is returned by WriteEntry.
	WriteEntryErr error
}

// QuarantineRecord captures what was quarantined and why.
type QuarantineRecord struct {
	Name string
	PE   document.ProcessingError
}

// New returns an initialised fake Storage with an empty inbox.
func New() *Storage {
	return &Storage{
		Inbox:   make(map[string][]byte),
		Library: make(map[string]document.Entry),
	}
}

// AddPDF adds a PDF (real or stub bytes) to the inbox.
func (f *Storage) AddPDF(name string, data []byte) {
	f.Inbox[name] = data
}

// ListInbox returns the names of all files currently in the fake inbox.
func (f *Storage) ListInbox(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(f.Inbox))
	for n := range f.Inbox {
		names = append(names, n)
	}
	return names, nil
}

// ReadPDF writes the inbox bytes to a temp file and returns its path.
// The cleanup func removes the temp file.
func (f *Storage) ReadPDF(_ context.Context, name string) (string, string, func(), error) {
	data, ok := f.Inbox[name]
	if !ok {
		return "", "", func() {}, fmt.Errorf("fake: %q not in inbox", name)
	}

	tmp, err := os.CreateTemp("", "fake-pdf-*.pdf")
	if err != nil {
		return "", "", func() {}, err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) //nolint:gosec
		return "", "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:gosec
		return "", "", func() {}, err
	}

	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	cleanup := func() { _ = os.Remove(tmp.Name()) } //nolint:gosec
	return tmp.Name(), hash, cleanup, nil
}

// IsDuplicate returns IsDuplicateErr if set, otherwise checks the Library.
func (f *Storage) IsDuplicate(_ context.Context, contentHash string) (bool, error) {
	if f.IsDuplicateErr != nil {
		return false, f.IsDuplicateErr
	}
	for _, e := range f.Library {
		if e.Metadata.ContentHash == contentHash {
			return true, nil
		}
	}
	return false, nil
}

// WriteEntry returns WriteEntryErr if set, otherwise records the entry and
// returns a synthetic name.
func (f *Storage) WriteEntry(_ context.Context, e document.Entry) (string, error) {
	if f.WriteEntryErr != nil {
		return "", f.WriteEntryErr
	}
	name := e.Metadata.DocumentDate + "_" + e.Metadata.Vendor
	f.Library[name] = e
	return name, nil
}

// MoveToProcessed records that the file was moved.
func (f *Storage) MoveToProcessed(_ context.Context, name, _ string) error {
	f.Processed = append(f.Processed, name)
	return nil
}

// Quarantine records the quarantine event.
func (f *Storage) Quarantine(_ context.Context, name string, pe document.ProcessingError) error {
	f.Quarantined = append(f.Quarantined, QuarantineRecord{Name: name, PE: pe})
	return nil
}
