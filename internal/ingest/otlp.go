package ingest

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	otelTraceIDLen = 16
	otelSpanIDLen  = 8
)

// ErrUnsupportedType is returned when the OTLP Content-Type is not protobuf or JSON.
var ErrUnsupportedType = errors.New("unsupported content type")

// Report is the result of normalizing one OTLP export. Rejected is seen minus kept.
type Report struct {
	Spans     []Span
	Seen      int
	Dropped   int
	Truncated bool
}

// Rejected is the number of spans not stored from this export.
func (r Report) Rejected() int {
	n := r.Seen - len(r.Spans)
	if n < 0 {
		return 0
	}
	return n
}

// DecodeOTLP unmarshals an OTLP ExportTraceServiceRequest from protobuf or JSON.
func DecodeOTLP(contentType string, body []byte) (*coltracepb.ExportTraceServiceRequest, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	if len(body) > maxOTLPBytes {
		return nil, fmt.Errorf("otlp payload too large")
	}
	req := &coltracepb.ExportTraceServiceRequest{}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch ct {
	case "application/json":
		if err := protojson.Unmarshal(body, req); err != nil {
			return nil, fmt.Errorf("decode otlp json: %w", err)
		}
	case "", "application/x-protobuf", "application/protobuf":
		if err := proto.Unmarshal(body, req); err != nil {
			return nil, fmt.Errorf("decode otlp protobuf: %w", err)
		}
	default:
		return nil, fmt.Errorf("%w %q", ErrUnsupportedType, contentType)
	}
	return req, nil
}

// Normalize extracts allowlisted span metadata. Request bodies are never copied.
func Normalize(req *coltracepb.ExportTraceServiceRequest) []Span {
	return NormalizeReport(req).Spans
}

// NormalizeReport extracts allowlisted metadata and counts dropped or truncated spans.
func NormalizeReport(req *coltracepb.ExportTraceServiceRequest) Report {
	return normalizeBatch(req, maxSpansBatch)
}

func normalizeBatch(req *coltracepb.ExportTraceServiceRequest, limit int) Report {
	out := Report{Spans: []Span{}}
	if req == nil {
		return out
	}
	if limit <= 0 {
		limit = maxSpansBatch
	}
	for _, rs := range req.GetResourceSpans() {
		service := sanitizeText(attrString(rs.GetResource().GetAttributes(), "service.name"), maxString)
		if service == "" {
			service = "unknown"
		}
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				out.Seen++
				span, ok := normalizeSpan(service, sp)
				if !ok {
					out.Dropped++
					continue
				}
				if len(out.Spans) >= limit {
					out.Truncated = true
					continue
				}
				out.Spans = append(out.Spans, span)
			}
		}
	}
	return out
}

func normalizeSpan(service string, sp *tracepb.Span) (Span, bool) {
	if !validID(sp.GetTraceId(), otelTraceIDLen) || !validID(sp.GetSpanId(), otelSpanIDLen) {
		return Span{}, false
	}
	startNS := sp.GetStartTimeUnixNano()
	endNS := sp.GetEndTimeUnixNano()
	if startNS > uint64(math.MaxInt64) || endNS > uint64(math.MaxInt64) {
		return Span{}, false
	}
	attrs := sp.GetAttributes()
	start := time.Unix(0, int64(startNS)).UTC()
	end := time.Unix(0, int64(endNS)).UTC()
	var dur int64
	if !end.Before(start) {
		dur = end.Sub(start).Nanoseconds()
	}
	parent := ""
	if pid := sp.GetParentSpanId(); validID(pid, otelSpanIDLen) {
		parent = hexID(pid)
	}
	route := sanitizeRoute(httpRoute(attrs))
	method := canonicalMethod(attrString(attrs, "http.request.method", "http.method"))
	method, route = splitMethodRoute(method, route)
	return Span{
		TraceID:      hexID(sp.GetTraceId()),
		SpanID:       hexID(sp.GetSpanId()),
		ParentSpanID: parent,
		ServiceName:  service,
		Name:         sanitizeName(sp.GetName()),
		Kind:         spanKind(sp.GetKind()),
		StatusCode:   statusCode(sp.GetStatus()),
		HTTPMethod:   method,
		HTTPRoute:    route,
		HTTPStatus:   canonicalStatus(attrInt(attrs, "http.response.status_code", "http.status_code")),
		CodeFunction: sanitizeText(attrString(attrs, "code.function.name", "code.function"), maxString),
		CodeFile:     sanitizeFile(attrString(attrs, "code.file.path", "code.filepath")),
		StartTime:    start,
		DurationNS:   dur,
	}, true
}

func splitMethodRoute(method, route string) (string, string) {
	method = canonicalMethod(method)
	route = strings.TrimSpace(route)
	if m, rest, ok := strings.Cut(route, " "); ok {
		if cm := canonicalMethod(m); cm != "" {
			if method == "" {
				method = cm
			}
			route = strings.TrimSpace(rest)
		}
	}
	return method, route
}

func httpRoute(attrs []*commonpb.KeyValue) string {
	for _, key := range []string{"http.route", "url.path", "http.target", "url.full", "http.url"} {
		if v := attrString(attrs, key); v != "" {
			return v
		}
	}
	return ""
}

func spanKind(k tracepb.Span_SpanKind) string {
	switch k {
	case tracepb.Span_SPAN_KIND_INTERNAL:
		return "internal"
	case tracepb.Span_SPAN_KIND_SERVER:
		return "server"
	case tracepb.Span_SPAN_KIND_CLIENT:
		return "client"
	case tracepb.Span_SPAN_KIND_PRODUCER:
		return "producer"
	case tracepb.Span_SPAN_KIND_CONSUMER:
		return "consumer"
	default:
		return ""
	}
}

func statusCode(st *tracepb.Status) string {
	if st == nil {
		return ""
	}
	switch st.GetCode() {
	case tracepb.Status_STATUS_CODE_ERROR:
		return "error"
	case tracepb.Status_STATUS_CODE_OK:
		return "ok"
	default:
		return ""
	}
}

func attrString(attrs []*commonpb.KeyValue, keys ...string) string {
	for _, key := range keys {
		for _, kv := range attrs {
			if kv.GetKey() != key {
				continue
			}
			return anyString(kv.GetValue())
		}
	}
	return ""
}

func attrInt(attrs []*commonpb.KeyValue, keys ...string) int {
	for _, key := range keys {
		for _, kv := range attrs {
			if kv.GetKey() != key {
				continue
			}
			v := kv.GetValue()
			if v == nil {
				return 0
			}
			if n := v.GetIntValue(); n != 0 {
				if n < math.MinInt || n > math.MaxInt {
					return 0
				}
				return int(n)
			}
			if s := strings.TrimSpace(v.GetStringValue()); s != "" {
				n, err := strconv.Atoi(s)
				if err == nil {
					return n
				}
			}
			return 0
		}
	}
	return 0
}

func anyString(v *commonpb.AnyValue) string {
	if v == nil {
		return ""
	}
	switch v.Value.(type) {
	case *commonpb.AnyValue_StringValue:
		return v.GetStringValue()
	case *commonpb.AnyValue_IntValue:
		return strconv.FormatInt(v.GetIntValue(), 10)
	case *commonpb.AnyValue_DoubleValue:
		return strconv.FormatFloat(v.GetDoubleValue(), 'f', -1, 64)
	case *commonpb.AnyValue_BoolValue:
		return strconv.FormatBool(v.GetBoolValue())
	default:
		return ""
	}
}

func validID(b []byte, n int) bool {
	if len(b) != n || isZeroHex(b) {
		return false
	}
	return true
}

func hexID(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hexdigits[c>>4]
		out[i*2+1] = hexdigits[c&0x0f]
	}
	return string(out)
}

func isZeroHex(b []byte) bool {
	if len(b) == 0 {
		return true
	}
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
