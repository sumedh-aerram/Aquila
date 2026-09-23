package ingest

import (
	"testing"
	"time"
)

func TestAttachesFromNewestServiceFirst(t *testing.T) {
	t.Parallel()
	now := time.Unix(200, 0).UTC()
	got := attachesFrom([]Span{
		{ServiceName: "shop", StartTime: now.Add(-time.Hour)},
		{ServiceName: "ledger", StartTime: now},
		{ServiceName: "ledger", StartTime: now.Add(-time.Minute)},
		{ServiceName: "  ", StartTime: now},
	}, 20)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].Service != "ledger" || got[0].Spans != 2 {
		t.Fatalf("newest=%+v", got[0])
	}
	if got[1].Service != "shop" || got[1].Spans != 1 {
		t.Fatalf("older=%+v", got[1])
	}
}

func TestAttachesFromClipsLimit(t *testing.T) {
	t.Parallel()
	var spans []Span
	for i := 0; i < 3; i++ {
		spans = append(spans, Span{ServiceName: string(rune('a' + i)), StartTime: time.Unix(int64(i), 0).UTC()})
	}
	got := attachesFrom(spans, 1)
	if len(got) != 1 {
		t.Fatalf("%+v", got)
	}
}
