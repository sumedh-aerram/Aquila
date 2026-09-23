package jobs

import (
	"context"
	"errors"
	"fmt"
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
	var job Job
	for i := 0; i < 8; i++ {
		n := time.Now().UnixNano() + int64(i)*9973
		base := fmt.Sprintf("http://127.0.0.1:%d", 20000+int(n%20000))
		patch := fmt.Sprintf("http://127.0.0.1:%d", 40000+int((n/20000)%20000))
		job, err = st.Create(ctx, sampleOptsAt(t, base, patch))
		if err == nil {
			break
		}
		if !errors.Is(err, ErrBusy) {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clean, err := storage.Open(context.Background(), cfg)
		if err != nil {
			return
		}
		defer clean.Close()
		_, _ = NewPostgres(clean.Pool()).Cancel(context.Background(), job.ID)
	})
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
