package document

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// TextExtractFunc is the signature of the OCR function used by the pipeline.
// It is injectable via WithExtractor for tests that must not run pdftotext.
type TextExtractFunc func(ctx context.Context, path string) (string, error)

// Option configures the behaviour of ProcessOne.
type Option func(*pipelineOptions)

type pipelineOptions struct {
	extractor TextExtractFunc
}

func defaultOpts() *pipelineOptions {
	return &pipelineOptions{extractor: ExtractText}
}

func applyOpts(opts []Option) *pipelineOptions {
	o := defaultOpts()
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithExtractor replaces the default pdftotext OCR function. Used in tests to
// avoid subprocess execution and in Phase 2 for OTEL-instrumented extraction.
func WithExtractor(f TextExtractFunc) Option {
	return func(o *pipelineOptions) {
		o.extractor = f
	}
}

// ProcessOne runs the full per-file pipeline for a single inbox PDF:
//
//  1. ReadPDF          → obtain local path + content hash
//  2. IsDuplicate      → skip if already in library
//  3. ExtractText      → OCR transcript (pdftotext)
//  4. Classify         → structured metadata via Claude
//  5. ValidateMetadata → JSON-schema validation
//  6. WriteEntry       → write sidecar trio to library
//  7. MoveToProcessed  → move original out of inbox
//
// On a pipeline failure the file is quarantined and Result.Status ==
// StatusQuarantined. On an IsDuplicate infrastructure error, a non-nil error
// is returned and the caller should abort the run.
func ProcessOne(ctx context.Context, s Storage, c Classifier, name string, opts ...Option) (Result, error) {
	o := applyOpts(opts)

	localPath, hash, cleanup, err := s.ReadPDF(ctx, name)
	if err != nil {
		_ = s.Quarantine(ctx, name, ProcessingError{
			Stage:      "library_write",
			Err:        err,
			OccurredAt: time.Now().UTC(),
		})
		return Result{Status: StatusQuarantined, Stage: "library_write", Err: err}, nil
	}
	defer cleanup()

	dup, err := s.IsDuplicate(ctx, hash)
	if err != nil {
		return Result{}, err
	}
	if dup {
		return Result{Status: StatusSkipped, Hash: hash}, nil
	}

	transcript, err := runOCR(ctx, o, s, name, localPath)
	if err != nil {
		return Result{Status: StatusQuarantined, Hash: hash, Stage: "ocr", Err: err}, nil
	}

	meta, err := runClassify(ctx, s, name, c, transcript, hash)
	if err != nil {
		return Result{Status: StatusQuarantined, Hash: hash, Stage: "classify", Err: err}, nil
	}

	if err := runValidate(ctx, s, name, &meta); err != nil {
		return Result{Status: StatusQuarantined, Hash: hash, Stage: "schema_validate", Err: err}, nil
	}

	entryName, err := runWrite(ctx, s, name, localPath, transcript, meta)
	if err != nil {
		return Result{Status: StatusQuarantined, Hash: hash, Stage: "library_write", Err: err}, nil
	}

	moveErr := s.MoveToProcessed(ctx, name, hash)
	return Result{Status: StatusProcessed, Hash: hash, EntryName: entryName, MoveErr: moveErr}, nil
}

func runOCR(ctx context.Context, o *pipelineOptions, s Storage, name, localPath string) (string, error) {
	transcript, err := o.extractor(ctx, localPath)
	if err != nil {
		_ = s.Quarantine(ctx, name, ProcessingError{Stage: "ocr", Err: err, OccurredAt: time.Now().UTC()})
		return "", err
	}
	return transcript, nil
}

func runClassify(ctx context.Context, s Storage, name string, c Classifier, transcript, hash string) (Metadata, error) {
	meta, err := c.Classify(ctx, transcript, name, hash, time.Now().UTC())
	if err != nil {
		_ = s.Quarantine(ctx, name, ProcessingError{Stage: "classify", Err: err, OccurredAt: time.Now().UTC()})
		return Metadata{}, err
	}
	meta.SourceFilename = name
	return meta, nil
}

func runValidate(ctx context.Context, s Storage, name string, meta *Metadata) error {
	if err := ValidateMetadata(meta); err != nil {
		_ = s.Quarantine(ctx, name, ProcessingError{Stage: "schema_validate", Err: err, OccurredAt: time.Now().UTC()})
		return err
	}
	return nil
}

func runWrite(ctx context.Context, s Storage, name, pdfPath, transcript string, meta Metadata) (string, error) {
	if meta.FileDescription == "" {
		meta.FileDescription = strings.TrimSuffix(name, filepath.Ext(name))
	}
	entryName, err := s.WriteEntry(ctx, Entry{PDFPath: pdfPath, Transcript: transcript, Metadata: meta})
	if err != nil {
		_ = s.Quarantine(ctx, name, ProcessingError{Stage: "library_write", Err: err, OccurredAt: time.Now().UTC()})
		return "", err
	}
	return entryName, nil
}
