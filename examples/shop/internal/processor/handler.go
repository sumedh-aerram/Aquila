package processor

import (
	"net/http"
	"time"

	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
)

type chargeReq struct {
	CheckoutID  string `json:"checkout_id"`
	AmountCents int    `json:"amount_cents"`
}

type Handler struct {
	delay time.Duration
}

func NewHandler() http.Handler {
	h := &Handler{delay: 60 * time.Millisecond}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /charge", h.charge)
	return httpx.Wrap("processor", mux)
}

func (h *Handler) charge(w http.ResponseWriter, r *http.Request) {
	var req chargeReq
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !ids.Valid(req.CheckoutID) || req.AmountCents <= 0 || req.AmountCents > 10_000_000 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid charge")
		return
	}
	select {
	case <-r.Context().Done():
		httputil.WriteError(w, http.StatusGatewayTimeout, "cancelled")
		return
	case <-time.After(h.delay):
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "auth_code": "ok"})
}
