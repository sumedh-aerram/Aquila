package api

import (
	"bytes"
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
	if out.Spans[0].ServiceName != "gateway" || out.Spans[0].HTTPRoute != "GET /users/{id}" {
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
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
