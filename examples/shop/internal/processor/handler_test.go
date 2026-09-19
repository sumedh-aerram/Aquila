package processor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChargeRejectsBadInput(t *testing.T) {
	t.Parallel()
	h := NewHandler()
	cases := []string{
		`{}`,
		`{"checkout_id":"bad id","amount_cents":1}`,
		`{"checkout_id":"chk-1","amount_cents":0}`,
		`{"checkout_id":"chk-1","amount_cents":1,"processor_url":"http://127.0.0.1"}`,
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(body))
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestChargeOK(t *testing.T) {
	t.Parallel()
	h := NewHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/charge", strings.NewReader(`{"checkout_id":"chk-1","amount_cents":2500}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "approved" {
		t.Fatalf("got %#v", got)
	}
}
