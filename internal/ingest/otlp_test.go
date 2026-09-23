package ingest

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func TestNormalizeKeepsRouteAndDropsBody(t *testing.T) {
	t.Parallel()
	traceID, err := hex.DecodeString("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := hex.DecodeString("bbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1, 0).UTC()
	end := start.Add(25 * time.Millisecond)
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				strKV("service.name", "checkout"),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId:           traceID,
					SpanId:            spanID,
					Name:              "POST /checkout",
					Kind:              tracepb.Span_SPAN_KIND_SERVER,
					StartTimeUnixNano: uint64(start.UnixNano()),
					EndTimeUnixNano:   uint64(end.UnixNano()),
					Status:            &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK},
					Attributes: []*commonpb.KeyValue{
						strKV("http.route", "POST /checkout"),
						strKV("http.request.method", "POST"),
						intKV("http.response.status_code", 200),
						strKV("code.function.name", "Handler.create"),
						strKV("code.file.path", "examples/shop/internal/checkout/handler.go"),
						strKV("http.request.body", `{"password":"super-secret"}`),
						strKV("http.target", "/checkout?token=secret"),
					},
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 1 {
		t.Fatalf("len=%d", len(spans))
	}
	s := spans[0]
	if s.ServiceName != "checkout" || s.HTTPMethod != "POST" || s.HTTPRoute != "/checkout" || s.HTTPStatus != 200 {
		t.Fatalf("%+v", s)
	}
	if s.DurationNS != int64(25*time.Millisecond) {
		t.Fatalf("duration=%d", s.DurationNS)
	}
	if s.CodeFunction != "Handler.create" {
		t.Fatalf("function=%q", s.CodeFunction)
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(s.HTTPRoute, "token") || strings.Contains(s.Name, "super-secret") {
		t.Fatal("secret leaked into normalized span")
	}
	decoded, err := DecodeOTLP("application/x-protobuf", raw)
	if err != nil {
		t.Fatal(err)
	}
	again := Normalize(decoded)
	if len(again) != 1 || again[0].TraceID != s.TraceID {
		t.Fatalf("round trip %+v", again)
	}
}

func TestNormalizeStripsQueryFromHTTPRouteAndName(t *testing.T) {
	t.Parallel()
	traceID, _ := hex.DecodeString("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	spanID, _ := hex.DecodeString("ffffffffffffffff")
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: traceID,
					SpanId:  spanID,
					Name:    "GET /users/user-1?session=abc",
					Attributes: []*commonpb.KeyValue{
						strKV("http.route", "/users/user-1?session=abc"),
					},
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 1 {
		t.Fatalf("%+v", spans)
	}
	if spans[0].HTTPRoute != "/users/user-1" || spans[0].Name != "GET /users/user-1" {
		t.Fatalf("%+v", spans[0])
	}
	if strings.Contains(spans[0].HTTPRoute, "?") || strings.Contains(spans[0].Name, "?") {
		t.Fatal("query remained")
	}
}

func TestNormalizeStripsQueryFromTarget(t *testing.T) {
	t.Parallel()
	traceID, _ := hex.DecodeString("cccccccccccccccccccccccccccccccc")
	spanID, _ := hex.DecodeString("dddddddddddddddd")
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: traceID,
					SpanId:  spanID,
					Name:    "GET",
					Attributes: []*commonpb.KeyValue{
						strKV("http.target", "/users/user-1?session=abc"),
					},
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 1 || spans[0].HTTPRoute != "/users/user-1" {
		t.Fatalf("%+v", spans)
	}
	if spans[0].ServiceName != "unknown" {
		t.Fatalf("service=%q", spans[0].ServiceName)
	}
}

func TestDecodeOTLPRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := DecodeOTLP("text/plain", []byte("nope"))
	if err == nil || !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("err=%v", err)
	}
}

func TestNormalizeDropsInvalidIDs(t *testing.T) {
	t.Parallel()
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{
					{TraceId: []byte{1}, SpanId: bytesRepeat(0x22, 8), Name: "short-trace"},
					{TraceId: bytesRepeat(0x11, 16), SpanId: []byte{2}, Name: "short-span"},
					{TraceId: make([]byte, 16), SpanId: bytesRepeat(0x22, 8), Name: "zero-trace"},
					{TraceId: bytesRepeat(0x11, 16), SpanId: bytesRepeat(0x22, 8), Name: "ok"},
				},
			}},
		}},
	}
	rep := NormalizeReport(req)
	if len(rep.Spans) != 1 || rep.Spans[0].Name != "ok" {
		t.Fatalf("%+v", rep)
	}
	if rep.Seen != 4 || rep.Dropped != 3 || rep.Rejected() != 3 {
		t.Fatalf("%+v", rep)
	}
}

func TestNormalizeStripsUserinfoAndFragment(t *testing.T) {
	t.Parallel()
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytesRepeat(0xab, 16),
					SpanId:  bytesRepeat(0xcd, 8),
					Name:    "GET https://user:secret@shop.internal/checkout?token=abc#frag",
					Attributes: []*commonpb.KeyValue{
						strKV("http.target", "https://user:secret@shop.internal/checkout?token=abc#frag"),
						strKV("http.status_code", "200"),
						strKV("http.request.method", "GET\nX-Injected: 1"),
					},
					Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK, Message: "password=super-secret"},
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 1 {
		t.Fatalf("%+v", spans)
	}
	s := spans[0]
	if strings.Contains(s.HTTPRoute, "secret") || strings.Contains(s.Name, "secret") || strings.Contains(s.HTTPRoute, "token") {
		t.Fatalf("credential leaked: %+v", s)
	}
	if s.HTTPRoute != "/checkout" {
		t.Fatalf("route=%q", s.HTTPRoute)
	}
	if s.HTTPMethod != "GET" {
		t.Fatalf("method=%q", s.HTTPMethod)
	}
	if s.StatusCode != "ok" {
		t.Fatalf("status=%q", s.StatusCode)
	}
}

func TestNormalizeDropsTraversalCodeFileAndControls(t *testing.T) {
	t.Parallel()
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				strKV("service.name", "pay\x00ment\n"),
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytesRepeat(0x11, 16),
					SpanId:  bytesRepeat(0x22, 8),
					Name:    "authorize",
					Attributes: []*commonpb.KeyValue{
						strKV("code.function.name", "Handler.authorize"),
						strKV("code.file.path", "../../etc/passwd"),
						intKV("http.response.status_code", 9999),
					},
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 1 {
		t.Fatalf("%+v", spans)
	}
	if spans[0].ServiceName != "payment" {
		t.Fatalf("service=%q", spans[0].ServiceName)
	}
	if spans[0].CodeFile != "" {
		t.Fatalf("file=%q", spans[0].CodeFile)
	}
	if spans[0].HTTPStatus != 0 {
		t.Fatalf("status=%d", spans[0].HTTPStatus)
	}
}

func TestNormalizeKeepsParentOnlyWhenValid(t *testing.T) {
	t.Parallel()
	parent := bytesRepeat(0x33, 8)
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId:      bytesRepeat(0x11, 16),
					SpanId:       bytesRepeat(0x22, 8),
					ParentSpanId: []byte{0x01},
					Name:         "child",
				}, {
					TraceId:      bytesRepeat(0x11, 16),
					SpanId:       bytesRepeat(0x44, 8),
					ParentSpanId: parent,
					Name:         "ok-child",
				}},
			}},
		}},
	}
	spans := Normalize(req)
	if len(spans) != 2 {
		t.Fatalf("%d", len(spans))
	}
	if spans[0].ParentSpanID != "" {
		t.Fatalf("invalid parent kept %q", spans[0].ParentSpanID)
	}
	if spans[1].ParentSpanID != hex.EncodeToString(parent) {
		t.Fatalf("parent=%q", spans[1].ParentSpanID)
	}
}

func TestNormalizeTruncatesBatch(t *testing.T) {
	t.Parallel()
	spans := make([]*tracepb.Span, 0, 3)
	for i := 0; i < 3; i++ {
		spans = append(spans, &tracepb.Span{
			TraceId: bytesRepeat(0x11, 16),
			SpanId:  []byte{0, 0, 0, 0, 0, 0, 0, byte(i + 1)},
			Name:    "s",
		})
	}
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{Spans: spans}},
		}},
	}
	rep := normalizeBatch(req, 2)
	if !rep.Truncated || len(rep.Spans) != 2 || rep.Seen != 3 || rep.Rejected() != 1 {
		t.Fatalf("%+v", rep)
	}
}

func TestDecodeOTLPJSON(t *testing.T) {
	t.Parallel()
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytesRepeat(0x11, 16),
					SpanId:  bytesRepeat(0x22, 8),
					Name:    "GET /healthz",
				}},
			}},
		}},
	}
	raw, err := protojson.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeOTLP("application/json; charset=utf-8", raw)
	if err != nil {
		t.Fatal(err)
	}
	spans := Normalize(got)
	if len(spans) != 1 || spans[0].Name != "GET /healthz" {
		t.Fatalf("%+v", spans)
	}
}

func TestMemoryUpsertIsIdempotent(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	s := Span{TraceID: "aa", SpanID: "bb", ServiceName: "gateway", Name: "GET", StartTime: time.Unix(1, 0).UTC()}
	if err := m.UpsertSpans(t.Context(), []Span{s}); err != nil {
		t.Fatal(err)
	}
	s.Name = "GET /healthz"
	if err := m.UpsertSpans(t.Context(), []Span{s}); err != nil {
		t.Fatal(err)
	}
	got, err := m.ListSpans(t.Context(), ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "GET /healthz" {
		t.Fatalf("%+v", got)
	}
}

func TestMemoryListTraceWindowKeepsCompleteTraces(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	older := time.Unix(1, 0).UTC()
	newer := time.Unix(2, 0).UTC()
	if err := m.UpsertSpans(t.Context(), []Span{
		{TraceID: "old", SpanID: "1", ServiceName: "a", StartTime: older},
		{TraceID: "new", SpanID: "2", ServiceName: "b", StartTime: newer},
		{TraceID: "new", SpanID: "3", ParentSpanID: "2", ServiceName: "c", StartTime: newer.Add(time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.ListTraceWindow(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	for _, s := range got {
		if s.TraceID != "new" {
			t.Fatalf("%+v", got)
		}
	}
}

func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func intKV(k string, n int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: n}}}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}
