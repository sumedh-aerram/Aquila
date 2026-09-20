package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/sumedhaerram/aquila/internal/api"
	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/observability"
	"github.com/sumedhaerram/aquila/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	log := observability.NewLogger(cfg.Log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := storage.Open(ctx, cfg.Postgres)
	if err != nil {
		log.Error("open postgres", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	srv := api.NewServer(cfg, log, db, ingest.NewPostgres(db.Pool()))
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}
