package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"paper-claw/internal/document"
	"paper-claw/internal/store"
)

// ErrUnauthenticated is returned by TokenProvider.Get when no OAuth token is
// available yet (daemon booted before the /oauth flow was completed).
var ErrUnauthenticated = errors.New("drive: unauthenticated — complete the OAuth flow at /oauth/start")

// TokenProvider is the source of the *http.Client used to call the Drive API.
// It is rebuilt by Storage.SetHTTPClient after the OAuth callback stores a token.
type TokenProvider interface {
	// Get returns the authenticated HTTP client, or ErrUnauthenticated if not
	// yet authorised.
	Get() (*http.Client, error)
}

// Storage implements document.Storage using Google Drive.
// The inbox, library, and processed folders are identified by Drive folder IDs.
type Storage struct {
	inboxFolderID     string
	libraryFolderID   string
	processedFolderID string
	stableThreshold   time.Duration

	tp    TokenProvider
	store *store.DB

	mu     sync.Mutex
	client *client // rebuilt whenever a new token arrives
	// scanFiles caches the inbox listing for the current scan; reset on each ListInbox call.
	scanFiles map[string]FileInfo
}

// New creates a Drive Storage. stableThreshold is the minimum age a file must
// have before it is considered fully uploaded and safe to process.
func New(
	inboxFolderID, libraryFolderID, processedFolderID string,
	stableThreshold time.Duration,
	tp TokenProvider,
	st *store.DB,
) *Storage {
	return &Storage{
		inboxFolderID:     inboxFolderID,
		libraryFolderID:   libraryFolderID,
		processedFolderID: processedFolderID,
		stableThreshold:   stableThreshold,
		tp:                tp,
		store:             st,
		scanFiles:         make(map[string]FileInfo),
	}
}

// SetHTTPClient updates the Drive API client. Called by the OAuth callback
// after a new token is stored.
func (s *Storage) SetHTTPClient(ctx context.Context, httpClient *http.Client) error {
	c, err := newClient(ctx, httpClient)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	return nil
}

func (s *Storage) driveClient(ctx context.Context) (*client, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		return c, nil
	}
	// No client yet — try to build one from the token provider.
	hc, err := s.tp.Get()
	if err != nil {
		return nil, err
	}
	c, err = newClient(ctx, hc)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	return c, nil
}

// ListInbox returns the names of stable PDF files in the Drive inbox folder.
// "Stable" means the file's modifiedTime is older than stableThreshold and has
// a non-empty md5Checksum (files still uploading often lack a checksum).
func (s *Storage) ListInbox(ctx context.Context) ([]string, error) {
	c, err := s.driveClient(ctx)
	if err != nil {
		return nil, err
	}
	files, err := c.listFolder(ctx, s.inboxFolderID, "application/pdf")
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-s.stableThreshold)
	s.mu.Lock()
	s.scanFiles = make(map[string]FileInfo, len(files))
	s.mu.Unlock()

	var names []string
	for _, f := range files {
		if f.MD5Checksum == "" {
			continue // still uploading
		}
		if f.ModifiedTime.After(cutoff) {
			continue // too recent, may still be uploading
		}
		s.mu.Lock()
		s.scanFiles[f.Name] = f
		s.mu.Unlock()
		names = append(names, f.Name)
	}
	return names, nil
}

// ReadPDF downloads the PDF from Drive to a temp file and returns the local
// path, SHA-256 hash, and a cleanup function. The Drive file metadata is
// re-verified before download to guard against partial-upload races:
// if the size or modifiedTime changed since ListInbox, the file is skipped.
func (s *Storage) ReadPDF(ctx context.Context, name string) (string, string, func(), error) {
	c, err := s.driveClient(ctx)
	if err != nil {
		return "", "", func() {}, err
	}

	s.mu.Lock()
	cached, ok := s.scanFiles[name]
	s.mu.Unlock()
	if !ok {
		return "", "", func() {}, fmt.Errorf("drive: %q not found in scan cache", name)
	}

	// Re-verify: re-fetch metadata to catch files that changed between list and download.
	fresh, err := c.svc.Files.Get(cached.ID).
		Context(ctx).
		Fields("id,name,size,modifiedTime,md5Checksum").
		Do()
	if err != nil {
		return "", "", func() {}, fmt.Errorf("drive: re-fetching %q: %w", name, err)
	}
	freshMT, _ := time.Parse(time.RFC3339, fresh.ModifiedTime)
	if fresh.Size != cached.Size || !freshMT.Equal(cached.ModifiedTime) || fresh.Md5Checksum == "" {
		return "", "", func() {}, fmt.Errorf("drive: %q changed between list and download — skipping", name)
	}

	path, hash, cleanup, err := c.download(ctx, cached.ID)
	if err != nil {
		return "", "", func() {}, err
	}
	return path, hash, cleanup, nil
}

// IsDuplicate checks the SQLite dedup index.
func (s *Storage) IsDuplicate(ctx context.Context, contentHash string) (bool, error) {
	return s.store.HasHash(ctx, contentHash)
}

// WriteEntry creates the library folder hierarchy in Drive and uploads the three
// sidecar files (document.pdf, transcript.md, metadata.json).
func (s *Storage) WriteEntry(ctx context.Context, e document.Entry) (string, error) {
	c, err := s.driveClient(ctx)
	if err != nil {
		return "", err
	}

	meta := e.Metadata
	dirBase := document.FormatDirName(meta.DocumentDate, meta.Vendor, meta.FileDescription)

	// Resolve collision: check existing Drive folder children.
	children, err := c.folderChildNames(ctx, s.libraryFolderID)
	if err != nil {
		return "", err
	}
	dirName := document.UniqueDirName(dirBase, func(candidate string) bool {
		return children[candidate]
	})

	// Create the entry folder.
	folderID, err := c.createFolder(ctx, s.libraryFolderID, dirName)
	if err != nil {
		return "", err
	}

	// Upload document.pdf.
	if err := c.uploadFile(ctx, folderID, "document.pdf", e.PDFPath); err != nil {
		return "", err
	}

	// Upload transcript.md.
	if err := c.uploadBytes(ctx, folderID, "transcript.md", []byte(e.Transcript)); err != nil {
		return "", err
	}

	// Upload metadata.json.
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := c.uploadBytes(ctx, folderID, "metadata.json", append(metaJSON, '\n')); err != nil {
		return "", err
	}

	// Record in the SQLite dedup index.
	s.mu.Lock()
	fi := s.scanFiles[meta.SourceFilename]
	s.mu.Unlock()
	if err := s.store.PutDocument(ctx, store.DocumentRecord{
		ContentHash:    meta.ContentHash,
		EntryName:      dirName,
		DriveFileID:    fi.ID,
		SourceFilename: meta.SourceFilename,
		ProcessedAt:    time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("drive: recording document in store: %w", err)
	}

	return dirName, nil
}

// MoveToProcessed moves the inbox PDF to the processed folder.
func (s *Storage) MoveToProcessed(ctx context.Context, name, _ string) error {
	c, err := s.driveClient(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	fi, ok := s.scanFiles[name]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("drive: %q not in scan cache for MoveToProcessed", name)
	}

	return c.moveFile(ctx, fi.ID, s.inboxFolderID, s.processedFolderID)
}

// Quarantine uploads the inbox PDF and a processing_error.json to the Drive
// _quarantine/<name>/ folder under the library folder.
func (s *Storage) Quarantine(ctx context.Context, name string, pe document.ProcessingError) error {
	c, err := s.driveClient(ctx)
	if err != nil {
		return err
	}

	// Ensure the top-level _quarantine folder exists.
	qFolderID, err := s.ensureQuarantineFolder(ctx, c)
	if err != nil {
		return err
	}

	// Create a subfolder named after the inbox file.
	subID, err := c.createFolder(ctx, qFolderID, name)
	if err != nil {
		return err
	}

	// Upload the PDF if we have it in the scan cache.
	s.mu.Lock()
	fi, hasFI := s.scanFiles[name]
	s.mu.Unlock()
	if hasFI {
		// Download locally first, then re-upload into quarantine.
		tmpPath, _, cleanup, dlErr := c.download(ctx, fi.ID)
		if dlErr == nil {
			defer cleanup()
			_ = c.uploadFile(ctx, subID, "document.pdf", tmpPath)
		}
	}

	// Upload processing_error.json.
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
		return nil //nolint:nilerr // JSON marshal failure is non-fatal
	}
	_ = c.uploadBytes(ctx, subID, "processing_error.json", append(data, '\n'))
	return nil
}

// ensureQuarantineFolder finds or creates "_quarantine" under the library folder.
func (s *Storage) ensureQuarantineFolder(ctx context.Context, c *client) (string, error) {
	children, err := c.folderChildNames(ctx, s.libraryFolderID)
	if err != nil {
		return "", err
	}
	if !children["_quarantine"] {
		id, err := c.createFolder(ctx, s.libraryFolderID, "_quarantine")
		if err != nil {
			return "", err
		}
		return id, nil
	}
	// Find the existing folder ID.
	items, err := c.listFolder(ctx, s.libraryFolderID, "application/vnd.google-apps.folder")
	if err != nil {
		return "", err
	}
	for _, f := range items {
		if f.Name == "_quarantine" {
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("drive: _quarantine folder listed but not found")
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
		return "Check Drive permissions and network connectivity; re-run process."
	}
}
