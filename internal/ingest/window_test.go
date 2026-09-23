package ingest

import (
	"strings"
	"testing"
	"time"
)

func TestSelectWindowSkipsHealthz(t *testing.T) {
	t.Parallel()
	now := time.Unix(10, 0).UTC()
	spans := []Span{
		{TraceID: "h1", SpanID: "1", ServiceName: "shop", HTTPRoute: "/healthz", HTTPMethod: "GET", StartTime: now},
		{TraceID: "u1", SpanID: "2", ServiceName: "ledger", HTTPRoute: "/invoice", HTTPMethod: "GET", StartTime: now.Add(time.Second)},
		{TraceID: "u1", SpanID: "3", ServiceName: "ledger", Name: "charge", ParentSpanID: "2", StartTime: now.Add(2 * time.Second)},
	}
	got := SelectWindow(spans, 1, "")
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	for _, s := range got {
		if s.TraceID != "u1" {
			t.Fatalf("%+v", got)
		}
	}
}

func TestMemoryListTraceWindowSkipsHealthz(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	now := time.Unix(20, 0).UTC()
	if err := m.UpsertSpans(t.Context(), []Span{
		{TraceID: "h", SpanID: "1", ServiceName: "gateway", HTTPRoute: "/healthz", StartTime: now.Add(time.Minute)},
		{TraceID: "inv", SpanID: "2", ServiceName: "ledger", HTTPRoute: "/invoice", StartTime: now},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := m.ListTraceWindow(t.Context(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TraceID != "inv" {
		t.Fatalf("%+v", got)
	}
}

func TestSelectWindowServiceIgnoresNewerOtherAttach(t *testing.T) {
	t.Parallel()
	now := time.Unix(30, 0).UTC()
	spans := []Span{
		{TraceID: "shop", SpanID: "1", ServiceName: "gateway", HTTPRoute: "/users/{id}", HTTPMethod: "GET", StartTime: now.Add(time.Minute)},
		{TraceID: "app", SpanID: "2", ServiceName: "ledger", HTTPRoute: "/invoice", HTTPMethod: "GET", StartTime: now},
		{TraceID: "app", SpanID: "3", ServiceName: "ledger", Name: "charge", ParentSpanID: "2", StartTime: now.Add(time.Millisecond)},
	}
	got := SelectWindow(spans, 1, "ledger")
	if len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	for _, s := range got {
		if s.TraceID != "app" || s.ServiceName != "ledger" {
			t.Fatalf("%+v", got)
		}
	}
	mixed := SelectWindow(spans, 1, "")
	if len(mixed) == 0 || mixed[0].TraceID != "shop" {
		t.Fatalf("unfiltered should keep newest shop: %+v", mixed)
	}
}

func TestClipServiceBoundsLength(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", maxServiceName+20)
	if got := ClipService(long); len(got) != maxServiceName {
		t.Fatalf("len=%d", len(got))
	}
	if ClipService("  ledger  ") != "ledger" {
		t.Fatal("trim")
	}
}
