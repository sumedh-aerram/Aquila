package payment

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
	"github.com/sumedhaerram/aquila/examples/shop/internal/svcclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("shop.payment")

type authorizeReq struct {
	UserID      string `json:"user_id"`
	CheckoutID  string `json:"checkout_id"`
	AmountCents int    `json:"amount_cents"`
}

type paymentRow struct {
	ID     string
	Status string
	Amount int
}

type Handler struct {
	pool         *pgxpool.Pool
	processorURL string
}

func NewHandler(pool *pgxpool.Pool, processorURL string) http.Handler {
	h := &Handler{pool: pool, processorURL: strings.TrimRight(processorURL, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("POST /authorize", h.authorize)
	return httpx.Wrap("payment", mux)
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "postgres unreachable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	var req authorizeReq
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !ids.Valid(req.UserID) || !ids.Valid(req.CheckoutID) || req.AmountCents <= 0 || req.AmountCents > 10_000_000 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid authorize request")
		return
	}

	ctx, span := tracer.Start(r.Context(), "payment.Authorize")
	defer span.End()
	span.SetAttributes(
		attribute.String("code.function.name", "Handler.authorize"),
		attribute.String("code.file.path", "examples/shop/internal/payment/handler.go"),
		attribute.String("checkout.id", req.CheckoutID),
	)

	if existing, err := h.getByCheckout(ctx, req.CheckoutID); err == nil {
		h.replayOrConflict(w, existing, req.AmountCents)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	payID := "pay-" + uuid.NewString()
	tag, err := h.pool.Exec(ctx,
		`INSERT INTO payments (id, checkout_id, user_id, amount_cents, status) VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (checkout_id) DO NOTHING`,
		payID, req.CheckoutID, req.UserID, req.AmountCents, "pending",
	)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tag.RowsAffected() == 0 {
		existing, getErr := h.getByCheckout(ctx, req.CheckoutID)
		if getErr != nil {
			httputil.WriteError(w, http.StatusConflict, "authorization in progress")
			return
		}
		h.replayOrConflict(w, existing, req.AmountCents)
		return
	}

	if err := h.chargeProcessor(ctx, req); err != nil {
		_, _ = h.pool.Exec(ctx, `DELETE FROM payments WHERE checkout_id = $1 AND status = 'pending'`, req.CheckoutID)
		httputil.WriteError(w, http.StatusBadGateway, "processor declined")
		return
	}

	_, err = h.pool.Exec(ctx,
		`UPDATE payments SET status = 'authorized' WHERE checkout_id = $1 AND status = 'pending'`,
		req.CheckoutID,
	)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"id": payID, "status": "authorized"})
}

func (h *Handler) replayOrConflict(w http.ResponseWriter, existing paymentRow, amount int) {
	if existing.Status == "authorized" {
		if existing.Amount != amount {
			httputil.WriteError(w, http.StatusConflict, "amount mismatch")
			return
		}
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"id": existing.ID, "status": "authorized"})
		return
	}
	httputil.WriteError(w, http.StatusConflict, "authorization in progress")
}

func (h *Handler) getByCheckout(ctx context.Context, checkoutID string) (paymentRow, error) {
	var row paymentRow
	err := h.pool.QueryRow(ctx,
		`SELECT id, status, amount_cents FROM payments WHERE checkout_id = $1`, checkoutID,
	).Scan(&row.ID, &row.Status, &row.Amount)
	return row, err
}

func (h *Handler) chargeProcessor(ctx context.Context, req authorizeReq) error {
	// INTENTIONAL DEFECT D1: new HTTP client on every authorize (DEFECTS.md).
	client := svcclient.NewEphemeral()
	return svcclient.PostJSON(ctx, client, h.processorURL+"/charge", map[string]any{
		"checkout_id":  req.CheckoutID,
		"amount_cents": req.AmountCents,
	}, nil)
}
