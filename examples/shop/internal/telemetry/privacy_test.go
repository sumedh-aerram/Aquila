package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPSpansDoNotRecordBodiesOrSecrets(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
	})
	otel.SetTracerProvider(tp)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	h := otelhttp.NewHandler(inner, "echo")

	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(`{"password":"super-secret"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("expected HTTP span")
	}
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			key := strings.ToLower(string(attr.Key))
			val := strings.ToLower(attr.Value.AsString())
			if key == "http.request.body" || key == "http.response.body" {
				t.Fatalf("span recorded raw body in %s", attr.Key)
			}
			if strings.Contains(val, "super-secret") || strings.Contains(val, "password") {
				t.Fatalf("span leaked secret in %s=%q", attr.Key, attr.Value.AsString())
			}
		}
		for _, ev := range span.Events() {
			for _, attr := range ev.Attributes {
				if strings.Contains(attr.Value.AsString(), "super-secret") {
					t.Fatal("span event leaked secret")
				}
			}
		}
	}
}

func TestOTLPEndpointStripsSchemes(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317")
	if got := otlpEndpoint(); got != "otel-collector:4317" {
		t.Fatalf("got %q", got)
	}
}
