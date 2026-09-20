package ingest

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

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
		return nil, fmt.Errorf("unsupported content type %q", contentType)
	}
	return req, nil
}

// Normalize extracts allowlisted span metadata. Request bodies are never copied.
func Normalize(req *coltracepb.ExportTraceServiceRequest) []Span {
	if req == nil {
		return []Span{}
	}
	out := make([]Span, 0, 16)
	for _, rs := range req.GetResourceSpans() {
		service := clip(attrString(rs.GetResource().GetAttributes(), "service.name"), maxString)
		if service == "" {
			service = "unknown"
		}
		for _, ss := range rs.GetScopeSpans() {
			for _, sp := range ss.GetSpans() {
				span, ok := normalizeSpan(service, sp)
				if !ok {
					continue
				}
				out = append(out, span)
				if len(out) >= maxSpansBatch {
					return out
				}
			}
		}
	}
	return out
}

func normalizeSpan(service string, sp *tracepb.Span) (Span, bool) {
	traceID := hex.EncodeToString(sp.GetTraceId())
	spanID := hex.EncodeToString(sp.GetSpanId())
	if traceID == "" || spanID == "" || isZeroHex(sp.GetTraceId()) || isZeroHex(sp.GetSpanId()) {
		return Span{}, false
	}
	attrs := sp.GetAttributes()
	start := time.Unix(0, int64(sp.GetStartTimeUnixNano())).UTC()
	end := time.Unix(0, int64(sp.GetEndTimeUnixNano())).UTC()
	var dur int64
	if !end.Before(start) {
		dur = end.Sub(start).Nanoseconds()
	}
	parent := ""
	if !isZeroHex(sp.GetParentSpanId()) {
		parent = hex.EncodeToString(sp.GetParentSpanId())
	}
	route := stripQuery(httpRoute(attrs))
	return Span{
		TraceID:      clip(traceID, 32),
		SpanID:       clip(spanID, 16),
		ParentSpanID: clip(parent, 16),
		ServiceName:  service,
		Name:         clip(stripQuery(sp.GetName()), maxString),
		Kind:         spanKind(sp.GetKind()),
		StatusCode:   statusCode(sp.GetStatus()),
		HTTPMethod:   clip(attrString(attrs, "http.request.method", "http.method"), 16),
		HTTPRoute:    clip(route, maxString),
		HTTPStatus:   attrInt(attrs, "http.response.status_code", "http.status_code"),
		CodeFunction: clip(attrString(attrs, "code.function.name", "code.function"), maxString),
		CodeFile:     clip(attrString(attrs, "code.file.path", "code.filepath"), maxString),
		StartTime:    start,
		DurationNS:   dur,
	}, true
}

func httpRoute(attrs []*commonpb.KeyValue) string {
	if v := attrString(attrs, "http.route"); v != "" {
		return v
	}
	return attrString(attrs, "url.path", "http.target")
}

func stripQuery(s string) string {
	if i := strings.IndexByte(s, '?'); i >= 0 {
		return s[:i]
	}
	return s
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
