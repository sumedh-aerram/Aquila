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
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/observability"
	"github.com/sumedhaerram/aquila/internal/runs"
	"github.com/sumedhaerram/aquila/internal/source"
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

	src, err := source.Open(ctx, cfg.Source)
	if err != nil {
		log.Error("open source graph", "err", err)
		os.Exit(1)
	}
	if src != nil {
		log.Info("source graph loaded", "module", src.Module(), "nodes", src.NodeCount())
	}

	srv := api.NewServer(cfg, log, api.Dependencies{
		Ready:  db,
		Spans:  ingest.NewPostgres(db.Pool()),
		Source: src,
		Runs:   runs.NewPostgres(db.Pool()),
		Jobs:   jobs.NewPostgres(db.Pool()),
	})
	if err := srv.ListenAndServe(ctx); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}
