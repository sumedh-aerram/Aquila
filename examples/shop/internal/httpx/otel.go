package httpx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Wrap instruments a mux. Span names use the ServeMux pattern to avoid
// high-cardinality paths such as /users/{id}.
func Wrap(operation string, h http.Handler) http.Handler {
	return otelhttp.NewHandler(h, operation, otelhttp.WithSpanNameFormatter(SpanName))
}

func SpanName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method + " " + r.URL.Path
}
