package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sumedhaerram/aquila/internal/config"
)

// DB is the PostgreSQL pool used as Aquila's durable coordination store.
type DB struct {
	pool *pgxpool.Pool
}

// Open establishes a connection pool and verifies connectivity.
func Open(ctx context.Context, cfg config.PostgresConfig) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse postgres url: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns

	dialCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(dialCtx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(dialCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Ready reports whether PostgreSQL is reachable.
func (db *DB) Ready(ctx context.Context) error {
	if db == nil || db.pool == nil {
		return fmt.Errorf("postgres pool is not initialized")
	}
	return db.pool.Ping(ctx)
}

// Pool returns the underlying connection pool.
func (db *DB) Pool() *pgxpool.Pool {
	if db == nil {
		return nil
	}
	return db.pool
}

// Close releases the pool.
func (db *DB) Close() {
	if db != nil && db.pool != nil {
		db.pool.Close()
	}
}
