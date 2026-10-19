package runs

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sumedhaerram/aquila/internal/evidence"
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
	a := isolatePostgresArtifact(t, sampleArtifact(t, now, replay.VerdictDiffer))
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
	listed, err := p.List(t.Context(), 50, a.Service)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range listed {
		if r.ID == first.ID {
			found = true
			if !slices.Equal(r.Blockers, first.Blockers) {
				t.Fatalf("list blockers=%+v, want %+v", r.Blockers, first.Blockers)
			}
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

func TestPostgresLegacyRowNotValidated(t *testing.T) {
	p := openTestPostgres(t)
	a := isolatePostgresArtifact(t, legacyArtifact(t))
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	id := a.ArtifactDigest[:idLen]
	_, err = p.pool.Exec(t.Context(), `INSERT INTO aquila.runs (id,recorded_at,baseline_sha,dirty,workload_digest,artifact_digest,overall,validated,artifact) VALUES ($1,$2,$3,$4,$5,$6,$7,true,$8)`, id, a.Recorded, a.BaselineSHA, a.Dirty, a.WorkloadDigest, a.ArtifactDigest, a.Result.Overall, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.runs WHERE id=$1`, id) })
	got, err := p.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validated || len(got.Blockers) != 1 || got.Blockers[0].Code != "legacy_schema" {
		t.Fatalf("%+v", got)
	}
	list, err := p.List(t.Context(), 50, a.Service)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range list {
		if r.ID == id {
			found = true
			if r.Validated {
				t.Fatal("legacy listed validated")
			}
		}
	}
	if !found {
		t.Fatal("legacy row missing")
	}
}

func TestPostgresM1V2NoRevisions(t *testing.T) {
	p := openTestPostgres(t)
	raw, err := os.ReadFile("../evidence/testdata/v2_shop_m1_validated.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := evidence.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	a = isolatePostgresArtifact(t, a)
	raw, err = json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	id := a.ArtifactDigest[:idLen]
	_, err = p.pool.Exec(t.Context(), `INSERT INTO aquila.runs (id,recorded_at,baseline_sha,dirty,workload_digest,artifact_digest,overall,validated,artifact) VALUES ($1,$2,$3,$4,$5,$6,$7,true,$8)`, id, a.Recorded, a.BaselineSHA, a.Dirty, a.WorkloadDigest, a.ArtifactDigest, a.Result.Overall, raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.runs WHERE id=$1`, id) })
	got, err := p.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Validated || len(got.Blockers) != 2 || got.Blockers[0].Code != "unverified_revision" || !got.Artifact.Validated {
		t.Fatalf("%+v", got)
	}
	list, err := p.List(t.Context(), 50, a.Service)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range list {
		if r.ID == id {
			found = true
			if r.Validated || !slices.Equal(r.Blockers, got.Blockers) {
				t.Fatalf("%+v", r)
			}
		}
	}
	if !found {
		t.Fatal("M1 v2 row missing")
	}
}

// isolatePostgresArtifact gives each fixture a private service and content ID,
// preserving its schema and historical validation claims.
func isolatePostgresArtifact(t *testing.T, a evidence.Artifact) evidence.Artifact {
	t.Helper()
	a.Service = "postgres-test-" + rand.Text()
	a.ArtifactDigest = ""
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	a.ArtifactDigest = hex.EncodeToString(sum[:])
	if err := evidence.Verify(a); err != nil {
		t.Fatal(err)
	}
	return a
}
