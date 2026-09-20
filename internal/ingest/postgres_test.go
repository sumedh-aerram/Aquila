package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func openTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	dsn := os.Getenv("AQUILA_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("AQUILA_TEST_POSTGRES_URL not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping postgres: %v", err)
	}
	return NewPostgres(pool)
}

func TestPostgresUpsertIsIdempotent(t *testing.T) {
	p := openTestPostgres(t)
	h := sha256.Sum256([]byte(t.Name()))
	traceID := hex.EncodeToString(h[:16])
	spanID := hex.EncodeToString(h[:8])
	t.Cleanup(func() {
		_, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.spans WHERE trace_id = $1`, traceID)
	})

	s := Span{
		TraceID:     traceID,
		SpanID:      spanID,
		ServiceName: "checkout",
		Name:        "POST /checkout",
		HTTPRoute:   "/checkout",
		HTTPStatus:  200,
		StartTime:   time.Unix(1, 0).UTC(),
		DurationNS:  int64(time.Millisecond),
	}
	if err := p.UpsertSpans(t.Context(), []Span{s}); err != nil {
		t.Fatal(err)
	}
	s.Name = "POST /checkout retry"
	s.DurationNS = int64(2 * time.Millisecond)
	if err := p.UpsertSpans(t.Context(), []Span{s}); err != nil {
		t.Fatal(err)
	}

	got, err := p.ListSpans(t.Context(), ListQuery{TraceID: traceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Name != "POST /checkout retry" || got[0].DurationNS != int64(2*time.Millisecond) {
		t.Fatalf("%+v", got[0])
	}
	if got[0].ServiceName != "checkout" || got[0].HTTPRoute != "/checkout" {
		t.Fatalf("%+v", got[0])
	}
}
