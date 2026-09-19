package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/payment"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
	"github.com/sumedhaerram/aquila/examples/shop/internal/shopdb"
)

func main() {
	serve.Run("payment", envcfg.Get("HTTP_ADDR", ":8080"), func(ctx context.Context, _ *slog.Logger) (http.Handler, func(), error) {
		pool, err := shopdb.Open(ctx, envcfg.Get("POSTGRES_URL", "postgres://shop:shop@127.0.0.1:15433/shop?sslmode=disable"))
		if err != nil {
			return nil, nil, err
		}
		h := payment.NewHandler(pool, envcfg.Get("PROCESSOR_URL", "http://processor:8080"))
		return h, pool.Close, nil
	})
}
