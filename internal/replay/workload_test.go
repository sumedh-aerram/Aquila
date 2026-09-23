package replay

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestFromSpansKeepsGatewayGET(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "gateway", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/healthz"},
		{ServiceName: "users", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/users/user-1"},
		{ServiceName: "checkout", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/checkout"},
	})
	if len(w.Steps) != 2 {
		t.Fatalf("%+v", w.Steps)
	}
	if w.Steps[0].Path != "/healthz" || w.Steps[1].Path != "/users/user-1" {
		t.Fatalf("%+v", w.Steps)
	}
	for _, s := range w.Steps {
		if s.Provenance != ProvenanceSpanRoute || len(s.Body) != 0 {
			t.Fatalf("%+v", s)
		}
	}
}

func TestFromSpansKeepsForeignGET(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "api", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/v1/foo"},
		{ServiceName: "api", Kind: "server", HTTPMethod: "HEAD", HTTPRoute: "/v1/foo"},
	})
	if len(w.Steps) != 2 {
		t.Fatalf("%+v", w.Steps)
	}
	if w.Steps[0].Method != http.MethodGet || w.Steps[0].Path != "/v1/foo" {
		t.Fatalf("%+v", w.Steps[0])
	}
}

func TestFromSpansSkipsPOSTAndTemplates(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "api", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/v1/foo"},
		{ServiceName: "api", Kind: "server", HTTPMethod: "PUT", HTTPRoute: "/v1/foo"},
		{ServiceName: "gateway", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/users/{id}"},
		{ServiceName: "checkout", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/checkout/{id}"},
	})
	if len(w.Steps) != 0 {
		t.Fatalf("mutating or parameterized routes leaked: %+v", w.Steps)
	}
}

func TestFromSpansSkipsInternalAndClientHops(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "payment", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/authorize"},
		{ServiceName: "processor", Kind: "internal", HTTPMethod: "GET", HTTPRoute: "/debug"},
		{ServiceName: "checkout", Kind: "client", HTTPMethod: "GET", HTTPRoute: "/checkout"},
		{ServiceName: "gateway", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/users/user-1?session=secret"},
	})
	if len(w.Steps) != 1 || w.Steps[0].Path != "/users/user-1" {
		t.Fatalf("internal hops leaked: %+v", w.Steps)
	}
	if strings.Contains(w.Steps[0].Path, "?") || strings.Contains(w.Steps[0].Path, "secret") {
		t.Fatal("query leaked into workload")
	}
}

func TestFromSpansEmpty(t *testing.T) {
	t.Parallel()
	w := FromSpans(nil)
	if len(w.Steps) != 0 {
		t.Fatalf("%+v", w.Steps)
	}
}

func TestFromSpansDoesNotInventFromServiceNames(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "checkout", Kind: "server", Name: "checkout.Create"},
		{ServiceName: "payment", Kind: "server", Name: "payment.authorize"},
	})
	if len(w.Steps) != 0 {
		t.Fatalf("invented routes from names: %+v", w.Steps)
	}
}

func TestFromSpansCapsAtMaxSteps(t *testing.T) {
	t.Parallel()
	spans := make([]ingest.Span, 0, maxSteps+5)
	for i := 0; i < maxSteps+5; i++ {
		spans = append(spans, ingest.Span{
			ServiceName: "api",
			Kind:        "server",
			HTTPMethod:  http.MethodGet,
			HTTPRoute:   fmt.Sprintf("/v1/r%d", i),
		})
	}
	w := FromSpans(spans)
	if len(w.Steps) != maxSteps {
		t.Fatalf("steps=%d want %d", len(w.Steps), maxSteps)
	}
}

func TestRestrictToRoutesKeepsOverlap(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "ledger", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/invoice"},
		{ServiceName: "ledger", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/healthz"},
	})
	got := RestrictToRoutes(w, []string{"GET /invoice"}, ProvenanceChangedLines)
	if len(got.Steps) != 1 || got.Steps[0].Path != "/invoice" || got.Steps[0].Provenance != ProvenanceChangedLines {
		t.Fatalf("%+v", got.Steps)
	}
}

func TestRestrictToRoutesFallsBackWhenNoOverlap(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "ledger", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/invoice"},
	})
	got := RestrictToRoutes(w, []string{"GET /missing"}, ProvenanceChangedLines)
	if len(got.Steps) != 1 || got.Steps[0].Path != "/invoice" || got.Steps[0].Provenance != ProvenanceSpanRoute {
		t.Fatalf("must keep window: %+v", got.Steps)
	}
}

func TestShopFixtureHasNoTraceProvenance(t *testing.T) {
	t.Parallel()
	w := ShopFixture()
	if len(w.Steps) != 3 {
		t.Fatalf("%+v", w.Steps)
	}
	for _, s := range w.Steps {
		if s.Provenance != ProvenanceShopFixture {
			t.Fatalf("%+v", s)
		}
	}
}
