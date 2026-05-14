// Package serve implements the serialised scan worker that processes PDFs from
// the inbox one at a time.
package serve

import (
	"context"
	"log/slog"
	"time"

	"paper-claw/internal/document"
	"paper-claw/internal/store"
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
	requests   chan struct{}
}

// New creates a Worker. Call Enqueue to trigger a scan and Run to start the loop.
func New(storage document.Storage, classifier document.Classifier, st *store.DB) *Worker {
	return &Worker{
		storage:    storage,
		classifier: classifier,
		store:      st,
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
}

func (w *Worker) processOne(ctx context.Context, name string) {
	fileCtx, cancel := context.WithTimeout(context.Background(), perFileTimeout)
	defer cancel()

	result, err := document.ProcessOne(fileCtx, w.storage, w.classifier, name)
	if err != nil {
		slog.Error("processing pdf", "name", name, "err", err)
		_ = w.store.RecordError(ctx, err.Error())
		return
	}

	switch result.Status {
	case document.StatusProcessed:
		slog.Info("processed", "name", name, "entry", result.EntryName)
		_ = w.store.BumpCounter(ctx, "processed")
	case document.StatusSkipped:
		slog.Info("skipped (duplicate)", "name", name, "hash", result.Hash[:12])
		_ = w.store.BumpCounter(ctx, "skipped")
	case document.StatusQuarantined:
		slog.Warn("quarantined", "name", name, "stage", result.Stage, "err", result.Err)
		_ = w.store.BumpCounter(ctx, "quarantine")
	}
	if result.MoveErr != nil {
		slog.Warn("could not move to processed", "name", name, "err", result.MoveErr)
	}
}
