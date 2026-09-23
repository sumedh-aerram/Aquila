package runs

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sumedhaerram/aquila/internal/replay"
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

func TestPostgresInsertIsIdempotent(t *testing.T) {
	p := openTestPostgres(t)
	h := sha256.Sum256([]byte(t.Name()))
	now := time.Unix(1_700_000_000+int64(h[0]), 0).UTC()
	a := sampleArtifact(t, now, replay.VerdictDiffer)
	t.Cleanup(func() {
		_, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.runs WHERE id = $1`, a.ArtifactDigest[:idLen])
	})
	first, err := p.Insert(t.Context(), Record{Artifact: a})
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Insert(t.Context(), Record{Artifact: a})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Validated || second.Validated {
		t.Fatalf("%+v %+v", first, second)
	}
	got, err := p.Get(t.Context(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Overall != replay.VerdictDiffer || got.Artifact.Result.Overall != replay.VerdictDiffer {
		t.Fatalf("%+v", got)
	}
	listed, err := p.List(t.Context(), 50, "")
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range listed {
		if r.ID == first.ID {
			found = true
			if r.Artifact.Schema != "" {
				t.Fatal("list must omit artifact bodies")
			}
		}
	}
	if !found {
		t.Fatal("inserted run missing from list")
	}
}

func TestPostgresRejectsValidated(t *testing.T) {
	p := openTestPostgres(t)
	a := sampleArtifact(t, time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC), replay.VerdictMatch)
	a.Validated = true
	_, err := p.Insert(t.Context(), Record{Artifact: a})
	if err == nil {
		t.Fatal("expected error")
	}
}
