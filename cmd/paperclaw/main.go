package main

import (
	"context"
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

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: paperclaw <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  process   process PDFs from inbox into the library")
	fmt.Fprintln(os.Stderr, "  list      list documents in the library")
	fmt.Fprintln(os.Stderr, "  show      show a document by ID prefix")
	fmt.Fprintln(os.Stderr, "  search    search document transcripts for a keyword")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, `Run "paperclaw <command> -help" for command-specific flags.`)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Environment:")
	fmt.Fprintln(os.Stderr, "  ANTHROPIC_API_KEY   required for the process command (document classification)")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}
	classifier := document.NewClaudeClassifier()
	var err error
	switch os.Args[1] {
	case "process":
		err = runProcess(os.Args[2:], classifier)
	case "list":
		err = runList(os.Args[2:])
	case "show":
		err = runShow(os.Args[2:])
	case "search":
		err = runSearch(os.Args[2:])
	case "-h", "-help", "--help":
		printUsage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// --- process ----------------------------------------------------------------

type processSummary struct {
	Processed  int `json:"processed"`
	Skipped    int `json:"skipped"`
	Quarantine int `json:"quarantine"`
}

// stageError wraps a pipeline error with the stage at which it occurred.
type stageError struct {
	stage string
	cause error
}

func (e *stageError) Error() string { return e.stage + ": " + e.cause.Error() }
func (e *stageError) Unwrap() error { return e.cause }

type processingErrorJSON struct {
	Stage           string `json:"stage"`
	Error           string `json:"error"`
	LastLLMResponse string `json:"last_llm_response,omitempty"`
	RetryHint       string `json:"retry_hint"`
	OccurredAt      string `json:"occurred_at"`
}

type logEntry struct {
	OccurredAt string `json:"occurred_at"`
	Filename   string `json:"filename"`
	Status     string `json:"status"` // "processed" | "skipped" | "quarantined"
	Stage      string `json:"stage,omitempty"`
	Error      string `json:"error,omitempty"`
}

func writeLog(f *os.File, filename, status, stage string, cause error) {
	entry := logEntry{
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
		Filename:   filename,
		Status:     status,
		Stage:      stage,
	}
	if cause != nil {
		entry.Error = cause.Error()
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

func runProcess(args []string, classifier document.Classifier) error {
	fs := flag.NewFlagSet("process", flag.ContinueOnError)
	inbox := fs.String("inbox", filepath.Join(mustHomeDir(), "paperclaw", "inbox"), "inbox directory")
	library := fs.String("library", filepath.Join(mustHomeDir(), "paperclaw", "library"), "library directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	inboxDir := filepath.Clean(*inbox)
	libraryDir := filepath.Clean(*library)

	fmt.Printf("inbox:   %s\nlibrary: %s\n", inboxDir, libraryDir)

	if err := os.MkdirAll(libraryDir, 0o750); err != nil {
		return fmt.Errorf("creating library: %w", err)
	}

	logFile, err := os.OpenFile(filepath.Join(libraryDir, "process.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("opening log: %w", err)
	}
	defer logFile.Close()

	entries, err := os.ReadDir(inboxDir) //nolint:gosec // path from operator-supplied CLI flag
	if err != nil {
		return fmt.Errorf("reading inbox: %w", err)
	}

	ctx := context.Background()
	var summary processSummary

	for _, e := range entries {
		if e.IsDir() || !isPDF(e.Name()) {
			continue
		}
		srcPath := filepath.Join(inboxDir, e.Name())

		hash, err := hashFile(srcPath)
		if err != nil {
			se := &stageError{"library_write", err}
			quarantineFile(libraryDir, srcPath, e.Name(), se)
			writeLog(logFile, e.Name(), "quarantined", se.stage, se.cause)
			summary.Quarantine++
			continue
		}

		dup, err := isDuplicate(libraryDir, hash)
		if err != nil {
			return fmt.Errorf("checking library: %w", err)
		}
		if dup {
			writeLog(logFile, e.Name(), "skipped", "", nil)
			summary.Skipped++
			continue
		}

		if err := processFile(ctx, classifier, libraryDir, srcPath, e.Name(), hash); err != nil {
			var se *stageError
			if !errors.As(err, &se) {
				se = &stageError{"library_write", err}
			}
			quarantineFile(libraryDir, srcPath, e.Name(), se)
			writeLog(logFile, e.Name(), "quarantined", se.stage, se.cause)
			summary.Quarantine++
			continue
		}
		writeLog(logFile, e.Name(), "processed", "", nil)
		summary.Processed++
		if err := os.Remove(srcPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not delete %s from inbox: %v\n", e.Name(), err)
		}
	}

	return printSummary(summary)
}

func processFile(ctx context.Context, classifier document.Classifier, libraryDir, srcPath, srcName, hash string) error {
	transcript, err := document.ExtractText(ctx, srcPath)
	if err != nil {
		return &stageError{"ocr", err}
	}

	processedAt := time.Now().UTC()
	meta, err := classifier.Classify(ctx, transcript, srcName, hash, processedAt)
	if err != nil {
		return &stageError{"classify", err}
	}
	meta.SourceFilename = srcName

	if err := document.ValidateMetadata(&meta); err != nil {
		return &stageError{"schema_validate", err}
	}

	desc := meta.FileDescription
	if desc == "" {
		desc = strings.TrimSuffix(srcName, filepath.Ext(srcName))
	}
	dirBase := document.FormatDirName(meta.DocumentDate, meta.Vendor, desc)
	dirName := document.UniqueDirName(dirBase, func(s string) bool {
		_, statErr := os.Stat(filepath.Join(libraryDir, s))
		return statErr == nil
	})
	entryDir := filepath.Join(libraryDir, dirName)

	if err := os.MkdirAll(entryDir, 0o750); err != nil {
		return &stageError{"library_write", err}
	}
	if err := copyFile(srcPath, filepath.Join(entryDir, "document.pdf")); err != nil {
		return &stageError{"library_write", err}
	}
	if err := os.WriteFile(filepath.Join(entryDir, "transcript.md"), []byte(transcript), 0o600); err != nil {
		return &stageError{"library_write", err}
	}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return &stageError{"library_write", err}
	}
	if err := os.WriteFile(filepath.Join(entryDir, "metadata.json"), append(metaJSON, '\n'), 0o600); err != nil {
		return &stageError{"library_write", err}
	}
	return nil
}

func quarantineFile(libraryDir, srcPath, srcName string, cause *stageError) {
	qDir := filepath.Join(libraryDir, "_quarantine", srcName)
	if err := os.MkdirAll(qDir, 0o750); err != nil {
		return
	}
	_ = copyFile(srcPath, filepath.Join(qDir, "document.pdf"))

	pe := processingErrorJSON{
		Stage:      cause.stage,
		Error:      cause.cause.Error(),
		RetryHint:  retryHint(cause.stage),
		OccurredAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(pe, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(qDir, "processing_error.json"), append(data, '\n'), 0o600)
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

// --- list -------------------------------------------------------------------

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	library := fs.String("library", filepath.Join(mustHomeDir(), "paperclaw", "library"), "library directory")
	docType := fs.String("type", "", "filter by document type")
	since := fs.String("since", "", "filter by document_date >= DATE (YYYY-MM-DD)")
	vendor := fs.String("vendor", "", "filter by vendor (substring match)")
	overdue := fs.Bool("overdue", false, "only show documents with a past due_date")
	if err := fs.Parse(args); err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")
	metas, err := walkLibrary(*library)
	if err != nil {
		return err
	}

	var results []document.Metadata
	for _, m := range metas {
		if *docType != "" && m.Type != *docType {
			continue
		}
		if *since != "" && m.DocumentDate < *since {
			continue
		}
		if *vendor != "" && !strings.Contains(strings.ToLower(m.Vendor), strings.ToLower(*vendor)) {
			continue
		}
		if *overdue && (m.DueDate == "" || m.DueDate >= today) {
			continue
		}
		results = append(results, m)
	}

	return printMetadataList(results)
}

// --- show -------------------------------------------------------------------

func runShow(args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	library := fs.String("library", filepath.Join(mustHomeDir(), "paperclaw", "library"), "library directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("show requires an id prefix argument")
	}
	prefix := fs.Arg(0)

	entries, err := os.ReadDir(*library) //nolint:gosec
	if err != nil {
		return fmt.Errorf("reading library: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_quarantine" {
			continue
		}
		metaPath := filepath.Join(*library, e.Name(), "metadata.json")
		m, err := loadMetadata(metaPath)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(m.ID, prefix) {
			continue
		}
		transcript, _ := os.ReadFile(filepath.Join(*library, e.Name(), "transcript.md")) //nolint:gosec
		if isTerminal() {
			fmt.Printf("=== %s ===\n", e.Name())
			printMetadataText(m)
			if len(transcript) > 0 {
				fmt.Printf("\n--- transcript ---\n%s\n", transcript)
			}
		} else {
			type showResult struct {
				Metadata   document.Metadata `json:"metadata"`
				Transcript string            `json:"transcript"`
			}
			return json.NewEncoder(os.Stdout).Encode(showResult{m, string(transcript)})
		}
		return nil
	}
	return fmt.Errorf("no document found with id prefix %q", prefix)
}

// --- search -----------------------------------------------------------------

func runSearch(args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	library := fs.String("library", filepath.Join(mustHomeDir(), "paperclaw", "library"), "library directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("search requires a query argument")
	}
	query := strings.ToLower(fs.Arg(0))

	entries, err := os.ReadDir(*library) //nolint:gosec
	if err != nil {
		return fmt.Errorf("reading library: %w", err)
	}

	type hit struct {
		Entry string `json:"entry"`
		ID    string `json:"id"`
	}
	var hits []hit

	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_quarantine" {
			continue
		}
		transcriptPath := filepath.Join(*library, e.Name(), "transcript.md")
		data, err := os.ReadFile(transcriptPath) //nolint:gosec
		if err != nil {
			continue
		}
		if !strings.Contains(strings.ToLower(string(data)), query) {
			continue
		}
		metaPath := filepath.Join(*library, e.Name(), "metadata.json")
		m, err := loadMetadata(metaPath)
		if err != nil {
			hits = append(hits, hit{Entry: e.Name()})
			continue
		}
		hits = append(hits, hit{Entry: e.Name(), ID: m.ID})
	}

	if isTerminal() {
		if len(hits) == 0 {
			fmt.Printf("no results for %q\n", query)
			return nil
		}
		for _, h := range hits {
			fmt.Printf("%s  %s\n", h.ID[:12], h.Entry)
		}
		return nil
	}
	return json.NewEncoder(os.Stdout).Encode(hits)
}

// --- helpers ----------------------------------------------------------------

func isPDF(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".pdf")
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec
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
	entries, err := os.ReadDir(libraryDir) //nolint:gosec
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
		m, err := loadMetadata(filepath.Join(libraryDir, e.Name(), "metadata.json"))
		if err != nil {
			continue
		}
		if m.ContentHash == hash {
			return true, nil
		}
	}
	return false, nil
}

func walkLibrary(libraryDir string) ([]document.Metadata, error) {
	entries, err := os.ReadDir(libraryDir) //nolint:gosec
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading library: %w", err)
	}
	var metas []document.Metadata
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "_quarantine" {
			continue
		}
		m, err := loadMetadata(filepath.Join(libraryDir, e.Name(), "metadata.json"))
		if err != nil {
			continue
		}
		metas = append(metas, m)
	}
	return metas, nil
}

func loadMetadata(metaPath string) (document.Metadata, error) {
	data, err := os.ReadFile(metaPath) //nolint:gosec
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
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec
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

func printMetadataList(metas []document.Metadata) error {
	if isTerminal() {
		if len(metas) == 0 {
			fmt.Println("no documents found")
			return nil
		}
		for _, m := range metas {
			printMetadataText(m)
			fmt.Println()
		}
		return nil
	}
	return json.NewEncoder(os.Stdout).Encode(metas)
}

func printMetadataText(m document.Metadata) {
	fmt.Printf("id:      %s\n", m.ID)
	fmt.Printf("type:    %s\n", m.Type)
	fmt.Printf("date:    %s\n", m.DocumentDate)
	fmt.Printf("vendor:  %s\n", m.Vendor)
	fmt.Printf("summary: %s\n", m.Summary)
	if m.Amount != nil {
		fmt.Printf("amount:  %.2f %s\n", *m.Amount, m.Currency)
	}
	if m.DueDate != "" {
		fmt.Printf("due:     %s\n", m.DueDate)
	}
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
