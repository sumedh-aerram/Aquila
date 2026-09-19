package storage

import (
	"context"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
)

func TestOpenRejectsInvalidURL(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := Open(ctx, config.PostgresConfig{
		URL:            "://not-a-url",
		MaxConns:       1,
		ConnectTimeout: time.Second,
	})
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestReadyNilDB(t *testing.T) {
	t.Parallel()
	var db *DB
	if err := db.Ready(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
