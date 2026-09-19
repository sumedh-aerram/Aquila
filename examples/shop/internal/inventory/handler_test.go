package inventory

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetItemRejectsUnsafeSKU(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/items/1%3Bdrop", nil))
	if rec.Code == http.StatusOK || rec.Code >= 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
