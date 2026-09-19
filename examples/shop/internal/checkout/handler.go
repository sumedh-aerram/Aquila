package checkout

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
	"github.com/sumedhaerram/aquila/examples/shop/internal/svcclient"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("shop.checkout")

type Handler struct {
	pool            *pgxpool.Pool
	http            *http.Client
	usersURL        string
	inventoryURL    string
	paymentURL      string
	notificationURL string
}

type itemReq struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

type checkoutReq struct {
	UserID string    `json:"user_id"`
	Items  []itemReq `json:"items"`
}

type reserved struct {
	sku   string
	qty   int
	price int
}

func NewHandler(pool *pgxpool.Pool, usersURL, inventoryURL, paymentURL, notificationURL string) http.Handler {
	h := &Handler{
		pool:            pool,
		http:            svcclient.Shared(),
		usersURL:        trim(usersURL),
		inventoryURL:    trim(inventoryURL),
		paymentURL:      trim(paymentURL),
		notificationURL: trim(notificationURL),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("POST /checkout", h.create)
	mux.HandleFunc("GET /checkout/{id}", h.get)
	return httpx.Wrap("checkout", mux)
}

func trim(s string) string {
	if n := len(s); n > 0 && s[n-1] == '/' {
		return s[:n-1]
	}
	return s
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "postgres unreachable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req checkoutReq
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !ids.Valid(req.UserID) || len(req.Items) == 0 || len(req.Items) > 8 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid checkout")
		return
	}
	for _, it := range req.Items {
		if !ids.Valid(it.SKU) || it.Qty < 1 || it.Qty > 10 {
			httputil.WriteError(w, http.StatusBadRequest, "invalid item")
			return
		}
	}

	ctx, span := tracer.Start(r.Context(), "checkout.Create")
	defer span.End()
	span.SetAttributes(
		attribute.String("code.function.name", "Handler.create"),
		attribute.String("code.file.path", "examples/shop/internal/checkout/handler.go"),
	)

	var user map[string]any
	if err := svcclient.GetJSON(ctx, h.http, h.usersURL+"/users/"+req.UserID, &user); err != nil {
		httputil.WriteError(w, http.StatusBadGateway, "users unavailable")
		return
	}

	checkoutID := "chk-" + uuid.NewString()
	total := 0
	var reservedItems []reserved
	for _, it := range req.Items {
		var item struct {
			SKU        string `json:"sku"`
			PriceCents int    `json:"price_cents"`
		}
		if err := svcclient.GetJSON(ctx, h.http, h.inventoryURL+"/items/"+it.SKU, &item); err != nil {
			h.releaseAll(ctx, checkoutID, reservedItems)
			httputil.WriteError(w, http.StatusBadGateway, "inventory unavailable")
			return
		}
		if err := svcclient.PostJSON(ctx, h.http, h.inventoryURL+"/reserve", map[string]any{
			"sku": it.SKU, "qty": it.Qty, "checkout_id": checkoutID,
		}, nil); err != nil {
			h.releaseAll(ctx, checkoutID, reservedItems)
			httputil.WriteError(w, http.StatusConflict, "reserve failed")
			return
		}
		reservedItems = append(reservedItems, reserved{sku: it.SKU, qty: it.Qty, price: item.PriceCents})
		total += item.PriceCents * it.Qty
	}

	// INTENTIONAL D4: retry payment 5 times on any error with no backoff.
	var payErr error
	for range 5 {
		payErr = svcclient.PostJSON(ctx, h.http, h.paymentURL+"/authorize", map[string]any{
			"user_id": req.UserID, "checkout_id": checkoutID, "amount_cents": total,
		}, nil)
		if payErr == nil {
			break
		}
	}
	if payErr != nil {
		h.releaseAll(ctx, checkoutID, reservedItems)
		httputil.WriteError(w, http.StatusBadGateway, "payment failed")
		return
	}

	// INTENTIONAL D5: synchronous notification on the checkout critical path.
	_, err := h.pool.Exec(ctx,
		`INSERT INTO checkouts (id, user_id, status, total_cents) VALUES ($1,$2,$3,$4)`,
		checkoutID, req.UserID, "confirmed", total,
	)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, it := range reservedItems {
		_, _ = h.pool.Exec(ctx,
			`INSERT INTO checkout_items (checkout_id, sku, qty, price_cents) VALUES ($1,$2,$3,$4)`,
			checkoutID, it.sku, it.qty, it.price,
		)
	}

	// INTENTIONAL D5: synchronous notification still sits on the critical path
	// for latency. Payment/inventory success is already committed so a notify
	// failure cannot silently drop a paid order.
	_ = svcclient.PostJSON(ctx, h.http, h.notificationURL+"/notify", map[string]any{
		"user_id": req.UserID, "checkout_id": checkoutID, "kind": "order_confirmed",
	}, nil)

	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"id":          checkoutID,
		"status":      "confirmed",
		"total_cents": total,
		"user_id":     req.UserID,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !ids.Valid(id) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var status string
	var total int
	var userID string
	err := h.pool.QueryRow(r.Context(),
		`SELECT user_id, status, total_cents FROM checkouts WHERE id = $1`, id,
	).Scan(&userID, &status, &total)
	if err != nil {
		httputil.WriteError(w, http.StatusNotFound, "checkout not found")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{
		"id": id, "user_id": userID, "status": status, "total_cents": total,
	})
}

func (h *Handler) releaseAll(ctx context.Context, checkoutID string, items []reserved) {
	for _, it := range items {
		_ = svcclient.PostJSON(ctx, h.http, h.inventoryURL+"/release", map[string]any{
			"sku": it.sku, "qty": it.qty, "checkout_id": checkoutID,
		}, nil)
	}
}
