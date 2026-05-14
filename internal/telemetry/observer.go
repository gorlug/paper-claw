package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "paper-claw/pipeline"

// SpanObserver implements document.Observer using OpenTelemetry spans.
// Each pipeline stage becomes a child span of the document-level span.
type SpanObserver struct {
	tracer trace.Tracer
}

// NewSpanObserver creates a SpanObserver backed by the global tracer provider.
func NewSpanObserver() *SpanObserver {
	return &SpanObserver{tracer: otel.Tracer(tracerName)}
}

// StageStarted starts a new child span for the given stage and returns a
// context that carries the span.
func (o *SpanObserver) StageStarted(ctx context.Context, stage string) context.Context {
	ctx, _ = o.tracer.Start(ctx, stage, //nolint:errcheck // span stored in ctx
		trace.WithAttributes(attribute.String("pipeline.stage", stage)),
	)
	return ctx
}

// StageEnded ends the span stored in ctx. If err is non-nil the span is
// marked as an error.
func (o *SpanObserver) StageEnded(ctx context.Context, _ string, err error) {
	span := trace.SpanFromContext(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
}

// StartDocumentSpan starts a top-level span for processing a single document.
// The caller should defer the returned end function.
func StartDocumentSpan(ctx context.Context, filename string) (context.Context, func(error)) {
	tracer := otel.Tracer(tracerName)
	ctx, span := tracer.Start(ctx, "process_document",
		trace.WithAttributes(attribute.String("document.filename", filename)),
	)
	return ctx, func(err error) {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
