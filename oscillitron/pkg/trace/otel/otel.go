// CLAUDE GENERATED
// Package otel bridges Oscillitron's trace events to the OpenTelemetry
// Logs SDK. Operators ship logs to Datadog/Splunk/etc. by running an
// OTel collector that ingests OTLP and routes to their backend —
// Oscillitron itself stays backend-agnostic.
//
// The bridge piggybacks on slog: the otelslog handler converts a
// *slog.Logger into an OTel log emitter. Constructing a trace.Slog
// with that handler-backed logger means every trace event flows
// through unchanged code paths into the OTel pipeline.
//
// Configuration is via standard OTel env vars (no Go-side config):
//
//	OTEL_EXPORTER_OTLP_ENDPOINT  (default https://localhost:4318)
//	OTEL_EXPORTER_OTLP_PROTOCOL  (http/protobuf | http/json)
//	OTEL_EXPORTER_OTLP_HEADERS   (auth headers, e.g., DD-API-KEY=...)
//	OTEL_SERVICE_NAME            (the value passed to New is sent
//	                              as service.name)
//
// pkg/trace stays stdlib-only; the OTel dependency lives entirely
// here, so importing pkg/trace doesn't pull in OTel.
package otel

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// Provider bundles the OTel-backed slog.Logger with the lifecycle
// handles the operator needs to flush + shut down cleanly.
type Provider struct {
	// Logger is the otelslog-backed slog.Logger. Wrap in trace.Slog{}
	// to get a trace.Tracer that emits to OTel.
	Logger *slog.Logger

	provider *sdklog.LoggerProvider
}

// New constructs a Provider wired to the OTLP HTTP exporter, installs
// it as the global OTel LoggerProvider, and returns a slog.Logger
// that routes through it.
//
// serviceName is sent as the OTLP resource attribute service.name —
// shows up in Datadog as service:<name>, in Splunk as the source. Use
// a stable per-binary name (e.g., "oscillitron-bench").
//
// Returns an error if the OTLP exporter can't initialize (typically
// because OTEL_EXPORTER_OTLP_ENDPOINT is unset and there's no
// collector on localhost). The operator is responsible for ensuring
// a collector is reachable; we don't try to swallow the failure
// because silently dropping logs to a misconfigured pipeline is
// the worst-of-both-worlds outcome.
func New(ctx context.Context, serviceName string) (*Provider, error) {
	if serviceName == "" {
		return nil, fmt.Errorf("trace/otel: serviceName is required")
	}
	exporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("trace/otel: build OTLP HTTP exporter: %w", err)
	}
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(),
	)
	if err != nil {
		return nil, fmt.Errorf("trace/otel: build resource: %w", err)
	}
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(res),
	)
	global.SetLoggerProvider(provider)

	handler := otelslog.NewHandler(serviceName)
	return &Provider{
		Logger:   slog.New(handler),
		provider: provider,
	}, nil
}

// Shutdown flushes any pending logs in the batch processor and
// shuts down the provider. Call this from the parent program's
// defer or signal handler — without it, recent events may not be
// exported before the process exits.
//
// Idempotent on the OTel side; safe to call multiple times.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}
