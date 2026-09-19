package inventory

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httputil"
	"github.com/sumedhaerram/aquila/examples/shop/internal/httpx"
	"github.com/sumedhaerram/aquila/examples/shop/internal/ids"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("shop.inventory")

type Handler struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

type reserveReq struct {
	SKU        string `json:"sku"`
	Qty        int    `json:"qty"`
	CheckoutID string `json:"checkout_id"`
}

type Item struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
	Stock      int    `json:"stock"`
}

func NewHandler(pool *pgxpool.Pool, rdb *redis.Client) http.Handler {
	h := &Handler{pool: pool, rdb: rdb}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /items/{sku}", h.getItem)
	mux.HandleFunc("POST /reserve", h.reserve)
	mux.HandleFunc("POST /release", h.release)
	return httpx.Wrap("inventory", mux)
}

func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	if err := h.pool.Ping(r.Context()); err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "postgres unreachable")
		return
	}
	if err := h.rdb.Ping(r.Context()).Err(); err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "redis unreachable")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) getItem(w http.ResponseWriter, r *http.Request) {
	sku := r.PathValue("sku")
	if !ids.Valid(sku) {
		httputil.WriteError(w, http.StatusBadRequest, "invalid sku")
		return
	}
	item, err := h.loadItem(r.Context(), sku)
	if errors.Is(err, pgx.ErrNoRows) {
		httputil.WriteError(w, http.StatusNotFound, "item not found")
		return
	}
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "reserve")
}

func (h *Handler) release(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, "release")
}

func (h *Handler) mutate(w http.ResponseWriter, r *http.Request, kind string) {
	var req reserveReq
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.WriteError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !ids.Valid(req.SKU) || !ids.Valid(req.CheckoutID) || req.Qty < 1 || req.Qty > 20 {
		httputil.WriteError(w, http.StatusBadRequest, "invalid reserve")
		return
	}

	ctx, span := tracer.Start(r.Context(), "inventory."+kind)
	defer span.End()
	span.SetAttributes(
		attribute.String("code.function.name", "Handler.mutate"),
		attribute.String("code.file.path", "examples/shop/internal/inventory/handler.go"),
		attribute.String("item.sku", req.SKU),
	)

	ok, err := h.rdb.SetNX(ctx, "inventory:global", req.CheckoutID, 3*time.Second).Result()
	if err != nil {
		httputil.WriteError(w, http.StatusServiceUnavailable, "lock unavailable")
		return
	}
	if !ok {
		httputil.WriteError(w, http.StatusConflict, "inventory busy")
		return
	}
	defer func() { _ = h.rdb.Del(context.Background(), "inventory:global").Err() }()

	// INTENTIONAL D3: full scan of inventory_events by sku with no index.
	_, _ = h.pool.Exec(ctx, `SELECT COUNT(*) FROM inventory_events WHERE sku = $1`, req.SKU)

	delta := req.Qty
	if kind == "release" {
		delta = -req.Qty
	}

	tag, err := h.pool.Exec(ctx,
		`UPDATE items SET stock = stock - $1 WHERE sku = $2 AND stock - $1 >= 0`,
		delta, req.SKU,
	)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if tag.RowsAffected() == 0 {
		httputil.WriteError(w, http.StatusConflict, "insufficient stock")
		return
	}
	_, err = h.pool.Exec(ctx,
		`INSERT INTO inventory_events (sku, checkout_id, kind, qty) VALUES ($1,$2,$3,$4)`,
		req.SKU, req.CheckoutID, kind, req.Qty,
	)
	if err != nil {
		httputil.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": kind + "d"})
}

func (h *Handler) loadItem(ctx context.Context, sku string) (*Item, error) {
	var item Item
	err := h.pool.QueryRow(ctx,
		`SELECT sku, name, price_cents, stock FROM items WHERE sku = $1`, sku,
	).Scan(&item.SKU, &item.Name, &item.PriceCents, &item.Stock)
	return &item, err
}
