package users

import (
	"context"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
)

type Service interface {
	GetUser(ctx context.Context, id string) (*User, error)
	Ping(ctx context.Context) error
}

type Handler struct {
	svc Service
}

func NewHandler(svc Service) http.Handler {
	h := &Handler{svc: svc}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /users/{id}", h.getUser)
	return httpx.Wrap("users", mux)
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Ping(r.Context()); err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "postgres unreachable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.Valid(id) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}
	user, err := h.svc.GetUser(r.Context(), id)
	if err == ErrNotFound {
		httputil.WriteError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, user)
}
