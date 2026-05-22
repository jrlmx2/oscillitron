// CLAUDE GENERATED
package otel

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestNew_ConstructsAndShuts confirms the Provider can be built
// against a fake OTLP HTTP endpoint and shut down without error.
// This is a smoke test of the wiring — not of OTLP protocol
// correctness (which is the upstream SDK's job).
func TestNew_ConstructsAndShuts(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", srv.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_INSECURE", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	provider, err := New(context.Background(), "oscillitron-test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if provider.Logger == nil {
		t.Fatal("Logger is nil")
	}
	// Emit one log; rely on Shutdown to flush.
	provider.Logger.Info("test.event", slog.String("k", "v"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := provider.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
	// We don't assert received > 0 because the batch processor may
	// not flush synchronously in all cases. The point of this test
	// is verifying construction + shutdown wiring; emit correctness
	// is the SDK's contract.
}

func TestNew_RequiresServiceName(t *testing.T) {
	_, err := New(context.Background(), "")
	if err == nil {
		t.Error("expected error with empty serviceName")
	}
}

func TestShutdown_NilProviderIsNoop(t *testing.T) {
	var p *Provider
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("nil Provider Shutdown returned err: %v", err)
	}
}
