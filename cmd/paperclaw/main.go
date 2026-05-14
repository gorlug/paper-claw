package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"papwer-claw/internal/document"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: paperclaw <command> [flags]")
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "process":
		err = runProcess(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type docMetadata struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	DocumentDate   string `json:"document_date"`
	Vendor         string `json:"vendor"`
	Summary        string `json:"summary"`
	SourceFilename string `json:"source_filename"`
	ProcessedAt    string `json:"processed_at"`
	ContentHash    string `json:"content_hash"`
}

type processSummary struct {
	Processed  int `json:"processed"`
	Skipped    int `json:"skipped"`
	Quarantine int `json:"quarantine"`
}

func runProcess(args []string) error {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	inbox := fs.String("inbox", filepath.Join(mustHomeDir(), "paperclaw", "inbox"), "inbox directory")
	library := fs.String("library", filepath.Join(mustHomeDir(), "paperclaw", "library"), "library directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	inboxDir := filepath.Clean(*inbox)
	libraryDir := filepath.Clean(*library)

	entries, err := os.ReadDir(inboxDir) //nolint:gosec // path from operator-supplied CLI flag
	if err != nil {
		return fmt.Errorf("reading inbox: %w", err)
	}

	var summary processSummary
	today := time.Now().UTC().Format("2006-01-02")

	for _, e := range entries {
		if e.IsDir() || !isPDF(e.Name()) {
			continue
		}
		srcPath := filepath.Join(inboxDir, e.Name())

		hash, err := hashFile(srcPath)
		if err != nil {
			summary.Quarantine++
			continue
		}

		dup, err := isDuplicate(libraryDir, hash)
		if err != nil {
			return fmt.Errorf("checking library: %w", err)
		}
		if dup {
			summary.Skipped++
			continue
		}

		if err := writeLibraryEntry(libraryDir, srcPath, e.Name(), hash, today); err != nil {
			summary.Quarantine++
			continue
		}
		summary.Processed++
	}

	return printSummary(summary)
}

func isPDF(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".pdf")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // path is constructed from trusted inbox dir
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isDuplicate(libraryDir, hash string) (bool, error) {
	entries, err := os.ReadDir(libraryDir) //nolint:gosec // path from operator-supplied CLI flag
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
		metaPath := filepath.Join(libraryDir, e.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath) //nolint:gosec // path constructed from trusted library dir
		if err != nil {
			continue
		}
		var meta struct {
			ContentHash string `json:"content_hash"`
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		if meta.ContentHash == hash {
			return true, nil
		}
	}
	return false, nil
}

func writeLibraryEntry(libraryDir, srcPath, srcName, hash, today string) error {
	nameWithoutExt := strings.TrimSuffix(srcName, filepath.Ext(srcName))
	dirBase := document.FormatDirName(today, "unknown", nameWithoutExt)
	dirName := document.UniqueDirName(dirBase, func(s string) bool {
		_, statErr := os.Stat(filepath.Join(libraryDir, s))
		return statErr == nil
	})
	entryDir := filepath.Join(libraryDir, dirName)

	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		return err
	}
	if err := copyFile(srcPath, filepath.Join(entryDir, "document.pdf")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(entryDir, "transcript.md"), []byte(""), 0o600); err != nil {
		return err
	}

	meta := docMetadata{
		ID:             hash,
		Type:           "other",
		DocumentDate:   today,
		Vendor:         "unknown",
		Summary:        "Document pending classification.",
		SourceFilename: srcName,
		ProcessedAt:    time.Now().UTC().Format(time.RFC3339),
		ContentHash:    hash,
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(entryDir, "metadata.json"), append(metaJSON, '\n'), 0o600)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // path is from trusted inbox dir
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // fixed path under library dir
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func printSummary(s processSummary) error {
	if isTerminal() {
		fmt.Printf("%d documents processed, %d skipped (duplicate), %d quarantined\n",
			s.Processed, s.Skipped, s.Quarantine)
		return nil
	}
	return json.NewEncoder(os.Stdout).Encode(s)
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func mustHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
