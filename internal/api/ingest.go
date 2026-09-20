package api

import (
	"compress/gzip"
	"crypto/subtle"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

const (
	otlpMaxBytes      = 4 << 20
	ingestTokenHeader = "X-Aquila-Ingest-Token"
	maxQueryTraceID   = 64
	maxQueryService   = 128
)

func (s *Server) handleOTLPTraces(w http.ResponseWriter, r *http.Request) {
	if !s.ingestAuthorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if s.spans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingest unavailable"})
		return
	}
	body, err := readOTLPBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid otlp payload"})
		return
	}
	req, err := ingest.DecodeOTLP(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "invalid otlp payload"})
		return
	}
	spans := ingest.Normalize(req)
	if err := s.spans.UpsertSpans(r.Context(), spans); err != nil {
		s.log.Error("ingest spans", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
		return
	}
	resp, err := proto.Marshal(&coltracepb.ExportTraceServiceResponse{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
}

func (s *Server) ingestAuthorized(r *http.Request) bool {
	want := s.cfg.Ingest.Token
	if want == "" {
		return true
	}
	got := r.Header.Get(ingestTokenHeader)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func readOTLPBody(r *http.Request) ([]byte, error) {
	limited := http.MaxBytesReader(nil, r.Body, otlpMaxBytes)
	src := io.Reader(limited)
	if r.Header.Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(limited)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		src = io.LimitReader(gr, otlpMaxBytes+1)
	}
	body, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	if len(body) > otlpMaxBytes {
		return nil, io.ErrUnexpectedEOF
	}
	return body, nil
}

func (s *Server) handleListSpans(w http.ResponseWriter, r *http.Request) {
	if s.spans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingest unavailable"})
		return
	}
	q := ingest.ListQuery{
		TraceID: clipQuery(r.URL.Query().Get("trace_id"), maxQueryTraceID),
		Service: clipQuery(r.URL.Query().Get("service"), maxQueryService),
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		q.Limit = n
	}
	spans, err := s.spans.ListSpans(r.Context(), q)
	if err != nil {
		s.log.Error("list spans", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
		return
	}
	if spans == nil {
		spans = []ingest.Span{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
}

func clipQuery(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
