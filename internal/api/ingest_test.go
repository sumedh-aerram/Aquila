package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestOTLPTracesPersistsNormalizedSpan(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store})

	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
				{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "gateway"}}},
			}},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId:           bytes.Repeat([]byte{0xab}, 16),
					SpanId:            bytes.Repeat([]byte{0xcd}, 8),
					Name:              "GET /users/{id}",
					Kind:              tracepb.Span_SPAN_KIND_SERVER,
					StartTimeUnixNano: 1e9,
					EndTimeUnixNano:   1e9 + 1e6,
					Attributes: []*commonpb.KeyValue{
						{Key: "http.route", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "GET /users/{id}"}}},
						{Key: "http.request.body", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "secret"}}},
					},
				}},
			}},
		}},
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(raw))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/spans?service=gateway", nil)
	listRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var out struct {
		Spans []ingest.Span `json:"spans"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Spans) != 1 {
		t.Fatalf("spans=%d", len(out.Spans))
	}
	if out.Spans[0].ServiceName != "gateway" || out.Spans[0].HTTPRoute != "/users/{id}" {
		t.Fatalf("%+v", out.Spans[0])
	}
	if bytes.Contains(listRec.Body.Bytes(), []byte("secret")) {
		t.Fatal("body leaked")
	}
}

func TestOTLPTracesUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte{1})))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestOTLPTracesRequiresTokenWhenConfigured(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	srv := NewServer(config.Config{
		Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second},
		Ingest: config.IngestConfig{Token: "correct-token"},
	}, nil, Dependencies{Ready: stubReady{}, Spans: store})

	raw := marshalMinimalOTLP(t)
	post := func(token string) *httptest.ResponseRecorder {
		httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(raw))
		httpReq.Header.Set("Content-Type", "application/x-protobuf")
		if token != "" {
			httpReq.Header.Set(ingestTokenHeader, token)
		}
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, httpReq)
		return rec
	}

	if rec := post(""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d", rec.Code)
	}
	if rec := post("wrong-token"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d", rec.Code)
	}
	if rec := post("correct-token"); rec.Code != http.StatusOK {
		t.Fatalf("valid token status=%d body=%s", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/spans", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d", listRec.Code)
	}
}

func TestOTLPTracesRejectsOversizedBody(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(bytes.Repeat([]byte{'x'}, otlpMaxBytes+1)))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestOTLPTracesRejectsUnknownContentType(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader([]byte(`{}`)))
	httpReq.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestOTLPTracesAcceptsGzipAndJSON(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store})
	raw := marshalMinimalOTLP(t)

	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(gz.Bytes()))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "GZIP")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("gzip status=%d body=%s", rec.Code, rec.Body.String())
	}

	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytes.Repeat([]byte{0x33}, 16),
					SpanId:  bytes.Repeat([]byte{0x44}, 8),
					Name:    "GET /healthz",
				}},
			}},
		}},
	}
	js, err := protojson.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	jsonReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(js))
	jsonReq.Header.Set("Content-Type", "application/json")
	jsonRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(jsonRec, jsonReq)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("json status=%d body=%s", jsonRec.Code, jsonRec.Body.String())
	}
	if ct := jsonRec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Fatalf("content-type=%q", ct)
	}
}

func TestOTLPTracesDoesNotPersistUserinfo(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store})
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytes.Repeat([]byte{0xab}, 16),
					SpanId:  bytes.Repeat([]byte{0xcd}, 8),
					Name:    "GET",
					Attributes: []*commonpb.KeyValue{
						{Key: "url.full", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "https://user:leak@host/v1/x?token=1"}}},
					},
				}},
			}},
		}},
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(raw))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	listRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/spans", nil))
	if bytes.Contains(listRec.Body.Bytes(), []byte("leak")) || bytes.Contains(listRec.Body.Bytes(), []byte("token=")) {
		t.Fatalf("secret leaked: %s", listRec.Body.String())
	}
}

func TestOTLPTracesPartialSuccessOnInvalidIDs(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{
					{TraceId: []byte{1}, SpanId: bytes.Repeat([]byte{2}, 8), Name: "bad"},
					{TraceId: bytes.Repeat([]byte{1}, 16), SpanId: bytes.Repeat([]byte{2}, 8), Name: "ok"},
				},
			}},
		}},
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/traces", bytes.NewReader(raw))
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp coltracepb.ExportTraceServiceResponse
	if err := proto.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.GetPartialSuccess().GetRejectedSpans() != 1 {
		t.Fatalf("%+v", resp.GetPartialSuccess())
	}
}

func TestListSpansClipsQueryParams(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	long := strings.Repeat("a", 4096)
	req := httptest.NewRequest(http.MethodGet, "/v1/spans?service="+long+"&trace_id="+long, nil)
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestListSpansTraceWindow(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	older := time.Unix(1, 0).UTC()
	newer := time.Unix(2, 0).UTC()
	if err := store.UpsertSpans(t.Context(), []ingest.Span{
		{TraceID: "old", SpanID: "1", ServiceName: "a", HTTPMethod: "GET", HTTPRoute: "/old", StartTime: older},
		{TraceID: "new", SpanID: "2", ServiceName: "b", HTTPMethod: "GET", HTTPRoute: "/new", StartTime: newer},
		{TraceID: "new", SpanID: "3", ParentSpanID: "2", ServiceName: "c", HTTPMethod: "GET", HTTPRoute: "/child", StartTime: newer.Add(time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/spans?traces=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Spans []ingest.Span `json:"spans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Spans) != 2 {
		t.Fatalf("spans=%d", len(out.Spans))
	}
	for _, s := range out.Spans {
		if s.TraceID != "new" {
			t.Fatalf("%+v", out.Spans)
		}
	}
}

func TestListSpansInvalidTraces(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/spans?traces=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func marshalMinimalOTLP(t *testing.T) []byte {
	t.Helper()
	req := &coltracepb.ExportTraceServiceRequest{
		ResourceSpans: []*tracepb.ResourceSpans{{
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{
					TraceId: bytes.Repeat([]byte{0x11}, 16),
					SpanId:  bytes.Repeat([]byte{0x22}, 8),
					Name:    "GET /healthz",
				}},
			}},
		}},
	}
	raw, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
