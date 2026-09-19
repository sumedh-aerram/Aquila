package checkout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateRejectsBadInput(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, "http://users", "http://inventory", "http://payment", "http://notify")
	bodies := []string{
		`{}`,
		`{"user_id":"user-1","items":[]}`,
		`{"user_id":"bad id","items":[{"sku":"sku-widget","qty":1}]}`,
		`{"user_id":"user-1","items":[{"sku":"sku-widget","qty":99}]}`,
		`{"user_id":"user-1","items":[{"sku":"sku-widget","qty":1}],"payment_url":"http://evil"}`,
	}
	for _, body := range bodies {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/checkout", strings.NewReader(body)))
		if rec.Code == http.StatusOK || rec.Code >= 500 {
			t.Fatalf("body %s status=%d resp=%s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestGetRejectsUnsafeID(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, "http://users", "http://inventory", "http://payment", "http://notify")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/checkout/../payments", nil))
	if rec.Code == http.StatusOK || rec.Code >= 500 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
