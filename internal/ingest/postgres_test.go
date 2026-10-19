package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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
	h := sha256.Sum256([]byte(t.Name() + rand.Text()))
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

func TestPostgresListTraceWindow(t *testing.T) {
	p := openTestPostgres(t)
	h := sha256.Sum256([]byte(t.Name() + rand.Text()))
	traceID := hex.EncodeToString(h[:16])
	parentID := hex.EncodeToString(h[:8])
	childID := hex.EncodeToString(h[8:16])
	t.Cleanup(func() {
		_, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.spans WHERE trace_id = $1`, traceID)
	})
	service := "checkout-test-" + rand.Text()
	start := time.Unix(10, 0).UTC()
	if err := p.UpsertSpans(t.Context(), []Span{
		{TraceID: traceID, SpanID: parentID, ServiceName: service, StartTime: start},
		{TraceID: traceID, SpanID: childID, ParentSpanID: parentID, ServiceName: "payment", StartTime: start.Add(time.Millisecond)},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := p.ListTraceWindow(t.Context(), 50, service)
	if err != nil {
		t.Fatal(err)
	}
	var found int
	for _, s := range got {
		if s.TraceID == traceID {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("found=%d window=%d", found, len(got))
	}
}

func TestPostgresListAttaches(t *testing.T) {
	p := openIsolatedTestPostgres(t)
	h := sha256.Sum256([]byte(t.Name() + rand.Text()))
	traceID := hex.EncodeToString(h[:16])
	service := "ledger-test-" + rand.Text()
	now := time.Unix(50, 0).UTC()
	if err := p.UpsertSpans(t.Context(), []Span{
		{TraceID: traceID, SpanID: hex.EncodeToString(h[:8]), ServiceName: service, StartTime: now},
		{TraceID: traceID, SpanID: hex.EncodeToString(h[8:16]), ServiceName: service, StartTime: now.Add(time.Second)},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := p.ListAttaches(t.Context(), 20)
	if err != nil {
		t.Fatal(err)
	}
	var found Attach
	for _, a := range got {
		if a.Service == service {
			found = a
			break
		}
	}
	if len(got) != 1 || found.Spans != 2 || !found.LastSeen.Equal(now.Add(time.Second)) {
		t.Fatalf("%+v", got)
	}
}

func TestPostgresListTraceWindowServiceDropsOtherAttach(t *testing.T) {
	p := openTestPostgres(t)
	h := sha256.Sum256([]byte(t.Name() + rand.Text()))
	shopID := hex.EncodeToString(h[:16])
	appID := hex.EncodeToString(h[16:32])
	t.Cleanup(func() {
		_, _ = p.pool.Exec(context.Background(), `DELETE FROM aquila.spans WHERE trace_id IN ($1, $2)`, shopID, appID)
	})
	service := "ledger-test-" + rand.Text()
	now := time.Unix(40, 0).UTC()
	if err := p.UpsertSpans(t.Context(), []Span{
		{TraceID: shopID, SpanID: hex.EncodeToString(h[:8]), ServiceName: "gateway", HTTPRoute: "/users/{id}", HTTPMethod: "GET", StartTime: now.Add(time.Minute)},
		{TraceID: appID, SpanID: hex.EncodeToString(h[8:16]), ServiceName: service, HTTPRoute: "/invoice", HTTPMethod: "GET", StartTime: now},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := p.ListTraceWindow(t.Context(), 1, service)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TraceID != appID {
		t.Fatalf("%+v", got)
	}
}

// openIsolatedTestPostgres isolates queries that have no service filter or cursor.
func openIsolatedTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	admin := openTestPostgres(t).pool
	name := "aquila_test_" + rand.Text()
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+quoted+" TEMPLATE template0"); err != nil {
		t.Fatalf("create test database (requires CREATEDB): %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})
	cfg := admin.Config()
	cfg.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, migration := range []string{"000001_init.up.sql", "000002_spans.up.sql"} {
		raw, err := os.ReadFile("../../migrations/" + migration)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(t.Context(), string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	return NewPostgres(pool)
}
