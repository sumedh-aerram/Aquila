package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/inventory"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
	"github.com/sumedhaerram/aquila/examples/shop/internal/shopdb"
	"github.com/sumedhaerram/aquila/examples/shop/internal/shopredis"
)

func main() {
	serve.Run("inventory", envcfg.Get("HTTP_ADDR", ":8080"), func(ctx context.Context, _ *slog.Logger) (http.Handler, func(), error) {
		pool, err := shopdb.Open(ctx, envcfg.Get("POSTGRES_URL", "postgres://shop:shop@127.0.0.1:15433/shop?sslmode=disable"))
		if err != nil {
			return nil, nil, err
		}
		rdb, err := shopredis.Open(ctx, envcfg.Get("REDIS_URL", "redis://127.0.0.1:16379/0"))
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		return inventory.NewHandler(pool, rdb), func() { pool.Close(); _ = rdb.Close() }, nil
	})
}
