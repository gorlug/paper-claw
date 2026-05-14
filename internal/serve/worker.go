// Package serve implements the serialised scan worker that processes PDFs from
// the inbox one at a time.
package serve

import (
	"context"
	"log/slog"
	"time"

	"paper-claw/internal/document"
	"paper-claw/internal/store"
	"paper-claw/internal/telemetry"
)

const (
	// scanChanCap is the capacity of the coalescing scan-request channel.
	// A full buffer means another scan is already queued; new signals are dropped.
	scanChanCap = 1

	// perFileTimeout is the maximum time allowed for processing a single PDF.
	perFileTimeout = 10 * time.Minute
)

// Worker owns the serialised scan loop. Only one PDF is processed at a time;
// multiple concurrent trigger signals coalesce into a single scan.
type Worker struct {
	storage    document.Storage
	classifier document.Classifier
	store      *store.DB
	metrics    *telemetry.Metrics // nil when telemetry is disabled
	observer   document.Observer  // nil when tracing is disabled
	requests   chan struct{}
}

// New creates a Worker. Call Enqueue to trigger a scan and Run to start the loop.
// Pass nil metrics / observer when running without telemetry (e.g. local CLI).
func New(storage document.Storage, classifier document.Classifier, st *store.DB, metrics *telemetry.Metrics, observer document.Observer) *Worker {
	return &Worker{
		storage:    storage,
		classifier: classifier,
		store:      st,
		metrics:    metrics,
		observer:   observer,
		requests:   make(chan struct{}, scanChanCap),
	}
}

// Enqueue sends a non-blocking scan request. If one is already queued the
// request is dropped — the pending scan will cover the new inbox contents.
func (w *Worker) Enqueue() {
	select {
	case w.requests <- struct{}{}:
	default:
	}
}

// Run starts the scan loop. It processes one scan request at a time and
// returns when ctx is cancelled. The in-flight file processing uses a separate
// per-file timeout so graceful shutdown is not delayed by a stalled PDF.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.requests:
			w.runScan(ctx)
		}
	}
}

func (w *Worker) runScan(ctx context.Context) {
	names, err := w.storage.ListInbox(ctx)
	if err != nil {
		slog.Error("listing inbox", "err", err)
		_ = w.store.RecordError(ctx, err.Error())
		return
	}

	for _, name := range names {
		// Check for shutdown between files so we don't start a new one during
		// graceful shutdown.
		select {
		case <-ctx.Done():
			return
		default:
		}

		w.processOne(ctx, name)
	}

	if err := w.store.RecordPoll(ctx); err != nil {
		slog.Warn("recording poll", "err", err)
	}
	if w.metrics != nil {
		w.metrics.RecordPoll(ctx)
	}
}

func (w *Worker) processOne(ctx context.Context, name string) {
	fileCtx, cancel := context.WithTimeout(context.Background(), perFileTimeout)
	defer cancel()

	start := time.Now()

	// When tracing is enabled, wrap the whole pipeline in a document-level
	// span. Stage spans (ocr, classify, library_write) are added as children
	// by the observer passed to ProcessOne.
	var spanErr error
	var opts []document.Option
	if w.observer != nil {
		var endSpan func(error)
		fileCtx, endSpan = telemetry.StartDocumentSpan(fileCtx, name)
		defer func() { endSpan(spanErr) }()
		opts = append(opts, document.WithObserver(w.observer))
	}

	result, err := document.ProcessOne(fileCtx, w.storage, w.classifier, name, opts...)
	if err != nil {
		spanErr = err
		slog.Error("processing pdf", "name", name, "err", err)
		_ = w.store.RecordError(ctx, err.Error())
		return
	}

	elapsed := time.Since(start).Seconds()

	switch result.Status {
	case document.StatusProcessed:
		slog.Info("processed", "name", name, "entry", result.EntryName)
		_ = w.store.BumpCounter(ctx, "processed")
		if w.metrics != nil {
			w.metrics.RecordProcessed(ctx, elapsed)
		}
	case document.StatusSkipped:
		slog.Info("skipped (duplicate)", "name", name, "hash", result.Hash[:12])
		_ = w.store.BumpCounter(ctx, "skipped")
		if w.metrics != nil {
			w.metrics.RecordSkipped(ctx)
		}
	case document.StatusQuarantined:
		spanErr = result.Err
		slog.Warn("quarantined", "name", name, "stage", result.Stage, "err", result.Err)
		_ = w.store.BumpCounter(ctx, "quarantine")
		if w.metrics != nil {
			w.metrics.RecordQuarantined(ctx)
		}
	}
	if result.MoveErr != nil {
		slog.Warn("could not move to processed", "name", name, "err", result.MoveErr)
	}
}
