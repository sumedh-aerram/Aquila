package main

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/sumedhaerram/aquila/examples/shop/internal/envcfg"
	"github.com/sumedhaerram/aquila/examples/shop/internal/processor"
	"github.com/sumedhaerram/aquila/examples/shop/internal/serve"
)

func main() {
	serve.Run("processor", envcfg.Get("HTTP_ADDR", ":8080"), func(context.Context, *slog.Logger) (http.Handler, func(), error) {
		return processor.NewHandler(), nil, nil
	})
}
