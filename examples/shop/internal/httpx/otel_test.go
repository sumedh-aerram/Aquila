package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWrapUsesMuxPatternNotRawPath(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
	})
	otel.SetTracerProvider(tp)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := Wrap("users", mux)

	req := httptest.NewRequest(http.MethodGet, "/users/user-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	spans := sr.Ended()
	if len(spans) == 0 {
		t.Fatal("expected HTTP span")
	}
	var name, route string
	for _, span := range spans {
		if span.Name() != "" {
			name = span.Name()
		}
		for _, attr := range span.Attributes() {
			if string(attr.Key) == "http.route" {
				route = attr.Value.AsString()
			}
		}
	}
	if name != "GET /users/{id}" {
		t.Fatalf("name=%q", name)
	}
	if route != "GET /users/{id}" {
		t.Fatalf("route=%q", route)
	}
	if name == "GET /users/user-1" || route == "/users/user-1" {
		t.Fatal("raw path must not be the span identity")
	}
}
