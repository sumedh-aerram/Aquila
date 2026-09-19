package payment

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizeRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	h := NewHandler(nil, "http://processor.invalid")
	bodies := []string{
		`{"user_id":"user-1","checkout_id":"chk-1","amount_cents":1,"processor_url":"http://169.254.169.254/"}`,
		`{"user_id":"user 1","checkout_id":"chk-1","amount_cents":1}`,
		`{"user_id":"user-1","checkout_id":"chk-1","amount_cents":-5}`,
	}
	for _, body := range bodies {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/authorize", strings.NewReader(body)))
		if rec.Code == http.StatusOK {
			t.Fatalf("accepted %s", body)
		}
		if rec.Code >= 500 {
			t.Fatalf("status %d for %s body=%s", rec.Code, body, rec.Body.String())
		}
	}
}
