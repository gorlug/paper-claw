// Package telemetry configures OpenTelemetry for the paperclaw daemon.
// It exports traces, metrics, and logs to an OTLP/gRPC endpoint configured
// via the standard OTEL_* environment variables.
//
// Graceful degradation: if OTEL_EXPORTER_OTLP_ENDPOINT is unset, Setup
// returns no-op providers and leaves the slog default handler unchanged
// (the Phase 1 stderr JSON handler set in serve.go continues to work).
package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"log/slog"
)

// Setup initialises the global OTEL providers and, when an OTLP endpoint is
// configured, bridges slog to the OTEL log pipeline. It returns a shutdown
// function that must be called before the process exits.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is empty Setup returns a no-op shutdown
// function and leaves all global providers as their defaults.
func Setup(ctx context.Context, serviceName string) (shutdown func(context.Context), err error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) {}, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	// Trace exporter.
	traceExp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Metric exporter.
	metricExp, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Log exporter → bridge to slog.
	logExp, err := otlploggrpc.New(ctx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(lp)
	// Replace the default slog handler with one that bridges to the OTEL log pipeline.
	slog.SetDefault(slog.New(otelslog.NewHandler(serviceName)))

	shutdown = func(shutCtx context.Context) {
		_ = tp.Shutdown(shutCtx)
		_ = mp.Shutdown(shutCtx)
		_ = lp.Shutdown(shutCtx)
	}
	return shutdown, nil
}
