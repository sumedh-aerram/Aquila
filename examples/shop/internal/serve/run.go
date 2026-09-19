package serve

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/sumedhaerram/aquila/examples/shop/internal/httpserver"
	"github.com/sumedhaerram/aquila/examples/shop/internal/observability"
	"github.com/sumedhaerram/aquila/examples/shop/internal/telemetry"
)

// Run starts OTel and an HTTP server, blocking until SIGINT/SIGTERM.
func Run(service, addr string, build func(ctx context.Context, log *slog.Logger) (http.Handler, func(), error)) {
	log := observability.Logger(service)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTel, err := telemetry.Setup(ctx, service)
	if err != nil {
		log.Error("telemetry", "err", err)
		os.Exit(1)
	}
	defer func() { _ = shutdownTel(context.Background()) }()

	handler, cleanup, err := build(ctx, log)
	if err != nil {
		log.Error("startup", "err", err)
		os.Exit(1)
	}
	if cleanup != nil {
		defer cleanup()
	}

	srv := httpserver.NewServer(addr, httpserver.Middleware(log, handler))
	log.Info("listening", "addr", addr)
	if err := httpserver.ListenAndServe(ctx, srv); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
