package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/gateway"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
)

func main() {
	serve.Run("gateway", envcfg.Get("HTTP_ADDR", ":8080"), func(context.Context, *slog.Logger) (http.Handler, func(), error) {
		return gateway.NewHandler(
			envcfg.Get("USERS_URL", "http://users:8080"),
			envcfg.Get("CHECKOUT_URL", "http://checkout:8080"),
		), nil, nil
	})
}
