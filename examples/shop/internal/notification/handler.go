package notification

import (
	"net/http"
	"time"

	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
)

type notifyReq struct {
	UserID     string `json:"user_id"`
	CheckoutID string `json:"checkout_id"`
	Kind       string `json:"kind"`
}

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /notify", func(w http.ResponseWriter, r *http.Request) {
		var req notifyReq
		if err := httputil.DecodeJSON(r, &req); err != nil {
			httputil.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if !ids.Valid(req.UserID) || !ids.Valid(req.CheckoutID) || req.Kind != "order_confirmed" {
			httputil.WriteError(w, http.StatusBadRequest, "invalid notify")
			return
		}
		select {
		case <-r.Context().Done():
			httputil.WriteError(w, http.StatusGatewayTimeout, "cancelled")
			return
		case <-time.After(80 * time.Millisecond):
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	})
	return httpx.Wrap("notification", mux)
}
