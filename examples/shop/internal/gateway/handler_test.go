package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayDoesNotExposeInternalServices(t *testing.T) {
	t.Parallel()
	h := NewHandler("http://users:8080", "http://checkout:8080")
	for _, path := range []string{"/charge", "/authorize", "/reserve", "/notify", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s exposed: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	h := NewHandler("http://users:8080", "http://checkout:8080")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}
