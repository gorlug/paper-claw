package document

import (
	"context"
	"time"
)

// Storage is the backend-agnostic interface for document storage operations.
// The local CLI uses an os.* implementation; the serve daemon uses Google Drive.
type Storage interface {
	// ListInbox returns the names of PDF files currently in the inbox.
	ListInbox(ctx context.Context) ([]string, error)

	// ReadPDF provides a real on-disk path to the named PDF (pdftotext requires
	// a local file). It also returns the file's SHA-256 content hash. The caller
	// must always call cleanup when done, which may remove a temporary file.
	ReadPDF(ctx context.Context, name string) (localPath, contentHash string, cleanup func(), err error)

	// IsDuplicate reports whether a document with the given content hash already
	// exists in the library.
	IsDuplicate(ctx context.Context, contentHash string) (bool, error)

	// WriteEntry writes the PDF, transcript, and metadata sidecar into the
	// library and returns the entry directory name.
	WriteEntry(ctx context.Context, e Entry) (entryName string, err error)

	// MoveToProcessed moves the named inbox file to the processed area.
	MoveToProcessed(ctx context.Context, name, contentHash string) error

	// Quarantine copies the named inbox file and a processing error record to
	// the quarantine area.
	Quarantine(ctx context.Context, name string, pe ProcessingError) error
}

// Entry holds the data needed to write a library entry.
type Entry struct {
	PDFPath    string // local on-disk path of the PDF to copy/upload
	Transcript string
	Metadata   Metadata // includes SourceFilename, FileDescription, DocumentDate, Vendor, ContentHash
}

// ProcessingError describes a failure at a specific pipeline stage.
type ProcessingError struct {
	Stage      string
	Err        error
	OccurredAt time.Time
}

// Result is returned by ProcessOne for each file.
type Result struct {
	Status    string // StatusProcessed | StatusSkipped | StatusQuarantined
	Hash      string // always set after ReadPDF succeeds
	EntryName string // non-empty when Status == StatusProcessed
	Stage     string // non-empty when Status == StatusQuarantined
	Err       error  // non-nil when Status == StatusQuarantined
	MoveErr   error  // non-nil if MoveToProcessed failed (non-fatal)
}

// Result status values returned by ProcessOne.
const (
	StatusProcessed   = "processed"
	StatusSkipped     = "skipped"
	StatusQuarantined = "quarantined"
)
