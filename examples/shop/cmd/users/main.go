package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
	"github.com/sumedhaerram/aquila/examples/shop/internal/users"

	"github.com/sumedhaerram/aquila/examples/shop/internal/shopdb"
)

func main() {
	serve.Run("users", envcfg.Get("HTTP_ADDR", ":8080"), func(ctx context.Context, log *slog.Logger) (http.Handler, func(), error) {
		_ = log
		pool, err := shopdb.Open(ctx, envcfg.Get("POSTGRES_URL", "postgres://shop:shop@127.0.0.1:15433/shop?sslmode=disable"))
		if err != nil {
			return nil, nil, err
		}
		return users.NewHandler(users.NewStore(pool)), pool.Close, nil
	})
}
