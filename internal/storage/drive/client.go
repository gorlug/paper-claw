// Package drive provides a document.Storage implementation backed by Google Drive.
package drive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	gdrive "google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// FileInfo is a cached summary of a Drive file returned by ListFolder.
type FileInfo struct {
	ID           string
	Name         string
	Size         int64
	ModifiedTime time.Time
	MD5Checksum  string
}

// client is a thin wrapper around the Drive v3 API service.
type client struct {
	svc *gdrive.Service
}

func newClient(ctx context.Context, httpClient *http.Client) (*client, error) {
	svc, err := gdrive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("drive: creating service: %w", err)
	}
	return &client{svc: svc}, nil
}

// listFolder returns all files in the given folder matching the given MIME type
// filter (pass "" to list all files). Results are paginated automatically.
func (c *client) listFolder(ctx context.Context, folderID, mimeType string) ([]FileInfo, error) {
	q := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	if mimeType != "" {
		q += fmt.Sprintf(" and mimeType = '%s'", mimeType)
	}

	var files []FileInfo
	pageToken := ""
	for {
		call := c.svc.Files.List().
			Context(ctx).
			Q(q).
			Fields("nextPageToken, files(id,name,size,modifiedTime,md5Checksum)").
			PageSize(100)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("drive: listing folder %s: %w", folderID, err)
		}
		for _, f := range resp.Files {
			mt, _ := time.Parse(time.RFC3339, f.ModifiedTime)
			files = append(files, FileInfo{
				ID:           f.Id,
				Name:         f.Name,
				Size:         f.Size,
				ModifiedTime: mt,
				MD5Checksum:  f.Md5Checksum,
			})
		}
		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}
	return files, nil
}

// folderChildNames returns the names of all direct children of a Drive folder.
// Used by UniqueDirName to check for collision.
func (c *client) folderChildNames(ctx context.Context, folderID string) (map[string]bool, error) {
	items, err := c.listFolder(ctx, folderID, "")
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(items))
	for _, f := range items {
		names[f.Name] = true
	}
	return names, nil
}

// download streams the file content to a temp file and returns the path, the
// SHA-256 hex digest, and a cleanup function. The caller must always call cleanup.
func (c *client) download(ctx context.Context, fileID string) (path, hash string, cleanup func(), err error) {
	resp, err := c.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return "", "", func() {}, fmt.Errorf("drive: downloading %s: %w", fileID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	tmp, err := os.CreateTemp("", "drive-pdf-*.pdf")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("drive: temp file: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) //nolint:gosec
		return "", "", func() {}, fmt.Errorf("drive: writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:gosec
		return "", "", func() {}, fmt.Errorf("drive: closing temp file: %w", err)
	}

	hexHash := hex.EncodeToString(h.Sum(nil))
	cleanup = func() { _ = os.Remove(tmp.Name()) } //nolint:gosec
	return tmp.Name(), hexHash, cleanup, nil
}

// createFolder creates a subfolder with the given name inside parentID.
func (c *client) createFolder(ctx context.Context, parentID, name string) (string, error) {
	f, err := c.svc.Files.Create(&gdrive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}).Context(ctx).Fields("id").Do()
	if err != nil {
		return "", fmt.Errorf("drive: creating folder %q in %s: %w", name, parentID, err)
	}
	return f.Id, nil
}

// uploadFile uploads local file at srcPath into parentFolderID with the given name.
// Drive infers the MIME type from content.
func (c *client) uploadFile(ctx context.Context, parentFolderID, name, srcPath string) error {
	f, err := os.Open(srcPath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("drive: opening %s for upload: %w", srcPath, err)
	}
	defer func() { _ = f.Close() }()

	_, err = c.svc.Files.Create(&gdrive.File{
		Name:    name,
		Parents: []string{parentFolderID},
	}).Media(f).Context(ctx).Fields("id").Do()
	if err != nil {
		return fmt.Errorf("drive: uploading %q to %s: %w", name, parentFolderID, err)
	}
	return nil
}

// uploadBytes uploads content from a byte slice.
func (c *client) uploadBytes(ctx context.Context, parentFolderID, name string, data []byte) error {
	tmp, err := os.CreateTemp("", "drive-upload-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name()) //nolint:gosec
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := c.uploadFile(ctx, parentFolderID, name, tmp.Name()); err != nil {
		return err
	}
	return nil
}

// moveFile moves fileID from its current parent to newParentID (Drive
// "rename" via addParents / removeParents).
func (c *client) moveFile(ctx context.Context, fileID, oldParentID, newParentID string) error {
	_, err := c.svc.Files.Update(fileID, &gdrive.File{}).
		Context(ctx).
		AddParents(newParentID).
		RemoveParents(oldParentID).
		Fields("id").
		Do()
	if err != nil {
		return fmt.Errorf("drive: moving file %s: %w", fileID, err)
	}
	return nil
}
