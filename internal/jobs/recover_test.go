package jobs

import (
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/storage"
)

func TestPostgresSurvivesReconnect(t *testing.T) {
	ctx := t.Context()
	cfg := config.PostgresConfig{
		URL:            config.DefaultPostgresURL,
		MaxConns:       2,
		ConnectTimeout: 2 * time.Second,
	}
	db, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Skip(err.Error())
	}
	st := NewPostgres(db.Pool())
	job, err := st.Create(ctx, sampleOpts(t))
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	db2, err := storage.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	got, err := NewPostgres(db2.Pool()).Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != job.ID || got.Validated || len(got.Tasks) == 0 {
		t.Fatalf("%+v", got)
	}
}
