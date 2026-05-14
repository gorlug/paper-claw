package telemetry_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"

	"paper-claw/internal/telemetry"
)

func TestSetup_NoEndpoint_ReturnsNoop(t *testing.T) {
	// Ensure the env var is not set.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	shutdown, err := telemetry.Setup(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Setup with no endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown func must not be nil")
	}

	// Providers should be the global no-op defaults — calling them must not panic.
	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	span.End()

	// Shutdown should be a no-op and not error.
	shutdown(context.Background())
}

func TestSetup_NoEndpoint_LeavesProvidersAsDefault(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	_, err := telemetry.Setup(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	// The global meter provider should work (no-op — just must not panic).
	meter := otel.GetMeterProvider().Meter("test")
	counter, err := meter.Int64Counter("test.counter")
	if err != nil {
		t.Fatalf("creating counter: %v", err)
	}
	counter.Add(context.Background(), 1)
}

func TestMetrics_Init_Noop(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	_, err := telemetry.Setup(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	m, err := telemetry.Init()
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx := context.Background()
	// All record calls must be no-ops and not panic.
	m.RecordProcessed(ctx, 1.5)
	m.RecordSkipped(ctx)
	m.RecordQuarantined(ctx)
	m.RecordPoll(ctx)
}

func TestSpanObserver_NoopWhenOtelDisabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	_, err := telemetry.Setup(context.Background(), "test-service")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}

	obs := telemetry.NewSpanObserver()
	ctx := context.Background()

	// Must not panic even without a real tracer provider.
	ctx = obs.StageStarted(ctx, "ocr")
	obs.StageEnded(ctx, "ocr", nil)

	ctx2 := obs.StageStarted(ctx, "classify")
	obs.StageEnded(ctx2, "classify", context.Canceled)
}
