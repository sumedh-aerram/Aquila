package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/checkout"
	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
	"github.com/sumedhaerram/aquila/examples/shop/internal/shopdb"
)

func main() {
	serve.Run("checkout", envcfg.Get("HTTP_ADDR", ":8080"), func(ctx context.Context, _ *slog.Logger) (http.Handler, func(), error) {
		pool, err := shopdb.Open(ctx, envcfg.Get("POSTGRES_URL", "postgres://shop:shop@127.0.0.1:15433/shop?sslmode=disable"))
		if err != nil {
			return nil, nil, err
		}
		h := checkout.NewHandler(
			pool,
			envcfg.Get("USERS_URL", "http://users:8080"),
			envcfg.Get("INVENTORY_URL", "http://inventory:8080"),
			envcfg.Get("PAYMENT_URL", "http://payment:8080"),
			envcfg.Get("NOTIFICATION_URL", "http://notification:8080"),
		)
		return h, pool.Close, nil
	})
}
