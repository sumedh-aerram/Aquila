package ingest

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
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
	if s.ServiceName != "checkout" || s.HTTPRoute != "POST /checkout" || s.HTTPStatus != 200 {
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
	if spans[0].HTTPRoute != stripQuery(spans[0].HTTPRoute) {
		t.Fatal("query remained on http.route")
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
	if err == nil {
		t.Fatal("expected error")
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

func strKV(k, v string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: v}}}
}

func intKV(k string, n int64) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: k, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: n}}}
}
