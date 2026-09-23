package httpx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Wrap instruments a mux. After the mux matches, the span name and http.route
// are the ServeMux pattern so ingest sees templates, not /users/user-1.
func Wrap(operation string, h http.Handler) http.Handler {
	labeled := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		if r.Pattern == "" {
			return
		}
		span := trace.SpanFromContext(r.Context())
		if !span.IsRecording() {
			return
		}
		span.SetName(r.Pattern)
		span.SetAttributes(attribute.String("http.route", r.Pattern))
	})
	return otelhttp.NewHandler(labeled, operation, otelhttp.WithSpanNameFormatter(SpanName))
}

func SpanName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method + " " + r.URL.Path
}
