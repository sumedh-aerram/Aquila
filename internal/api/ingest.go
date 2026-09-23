package api

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
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
		if isTooLarge(err) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "otlp payload too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid otlp payload"})
		return
	}
	req, err := ingest.DecodeOTLP(r.Header.Get("Content-Type"), body)
	if err != nil {
		if errors.Is(err, ingest.ErrUnsupportedType) {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "invalid otlp payload"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid otlp payload"})
		return
	}
	rep := ingest.NormalizeReport(req)
	if err := s.spans.UpsertSpans(r.Context(), rep.Spans); err != nil {
		s.log.Error("ingest spans", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
		return
	}
	out := &coltracepb.ExportTraceServiceResponse{}
	if n := rep.Rejected(); n > 0 {
		out.PartialSuccess = &coltracepb.ExportTracePartialSuccess{
			RejectedSpans: int64(n),
			ErrorMessage:  "invalid or truncated spans",
		}
	}
	if err := writeOTLPResponse(w, r.Header.Get("Content-Type"), out); err != nil {
		s.log.Error("encode otlp response", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "ingest failed"})
	}
}

func writeOTLPResponse(w http.ResponseWriter, contentType string, out *coltracepb.ExportTraceServiceResponse) error {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if ct == "application/json" {
		raw, err := protojson.Marshal(out)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
		return nil
	}
	raw, err := proto.Marshal(out)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
	return nil
}

func (s *Server) ingestAuthorized(r *http.Request) bool {
	want := s.cfg.Ingest.Token
	if want == "" {
		return true
	}
	got := r.Header.Get(ingestTokenHeader)
	sumGot := sha256.Sum256([]byte(got))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

func readOTLPBody(r *http.Request) ([]byte, error) {
	limited := http.MaxBytesReader(nil, r.Body, otlpMaxBytes)
	src := io.Reader(limited)
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
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
		return nil, errOTLPTooLarge
	}
	return body, nil
}

var errOTLPTooLarge = errors.New("otlp payload too large")

func isTooLarge(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errOTLPTooLarge) {
		return true
	}
	var maxBytes *http.MaxBytesError
	return errors.As(err, &maxBytes)
}

func (s *Server) handleListSpans(w http.ResponseWriter, r *http.Request) {
	if s.spans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingest unavailable"})
		return
	}
	if raw := r.URL.Query().Get("traces"); raw != "" {
		n, service, err := parseWindowQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		spans, err := s.spans.ListTraceWindow(r.Context(), n, service)
		if err != nil {
			s.log.Error("list trace window", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list failed"})
			return
		}
		if spans == nil {
			spans = []ingest.Span{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"spans": spans})
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

func parseWindowQuery(r *http.Request) (int, string, error) {
	service := ingest.ClipService(r.URL.Query().Get("service"))
	raw := r.URL.Query().Get("traces")
	if raw == "" {
		return 0, service, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, "", errors.New("invalid traces")
	}
	return n, service, nil
}
