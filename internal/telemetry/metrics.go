package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const meterName = "paper-claw"

// Metrics holds the instruments used by the daemon. Call Init once after
// telemetry.Setup, then pass the returned *Metrics to the components that
// need to record measurements.
type Metrics struct {
	DocumentsProcessed   metric.Int64Counter
	DocumentsSkipped     metric.Int64Counter
	DocumentsQuarantined metric.Int64Counter
	PollCount            metric.Int64Counter
	WebhookCount         metric.Int64Counter
	ProcessingDuration   metric.Float64Histogram
}

// Init creates and registers all metric instruments against the global meter.
func Init() (*Metrics, error) {
	meter := otel.GetMeterProvider().Meter(meterName)

	processed, err := meter.Int64Counter("documents.processed",
		metric.WithDescription("Number of documents successfully processed"))
	if err != nil {
		return nil, err
	}
	skipped, err := meter.Int64Counter("documents.skipped",
		metric.WithDescription("Number of documents skipped as duplicates"))
	if err != nil {
		return nil, err
	}
	quarantined, err := meter.Int64Counter("documents.quarantined",
		metric.WithDescription("Number of documents moved to quarantine"))
	if err != nil {
		return nil, err
	}
	polls, err := meter.Int64Counter("poll.count",
		metric.WithDescription("Number of inbox poll cycles completed"))
	if err != nil {
		return nil, err
	}
	webhooks, err := meter.Int64Counter("webhook.count",
		metric.WithDescription("Number of Drive push notifications received"))
	if err != nil {
		return nil, err
	}
	dur, err := meter.Float64Histogram("document.processing.duration",
		metric.WithDescription("Time taken to process a single PDF, in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		DocumentsProcessed:   processed,
		DocumentsSkipped:     skipped,
		DocumentsQuarantined: quarantined,
		PollCount:            polls,
		WebhookCount:         webhooks,
		ProcessingDuration:   dur,
	}, nil
}

// RecordProcessed records one processed document and its processing duration.
func (m *Metrics) RecordProcessed(ctx context.Context, durationSecs float64) {
	m.DocumentsProcessed.Add(ctx, 1)
	m.ProcessingDuration.Record(ctx, durationSecs)
}

// RecordSkipped records one skipped (duplicate) document.
func (m *Metrics) RecordSkipped(ctx context.Context) {
	m.DocumentsSkipped.Add(ctx, 1)
}

// RecordQuarantined records one quarantined document.
func (m *Metrics) RecordQuarantined(ctx context.Context) {
	m.DocumentsQuarantined.Add(ctx, 1)
}

// RecordPoll records one poll cycle.
func (m *Metrics) RecordPoll(ctx context.Context) {
	m.PollCount.Add(ctx, 1)
}
