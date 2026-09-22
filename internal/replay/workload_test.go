package replay

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestFromSpansKeepsGatewayRoutes(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "gateway", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/healthz"},
		{ServiceName: "users", Kind: "server", HTTPMethod: "GET", HTTPRoute: "/users/user-1"},
		{ServiceName: "checkout", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/checkout"},
	})
	if len(w.Steps) != 3 {
		t.Fatalf("%+v", w.Steps)
	}
	post := w.Steps[2]
	if post.Method != http.MethodPost || !strings.Contains(post.Provenance, "fixture_body") {
		t.Fatalf("%+v", post)
	}
	if !bytes.Equal(post.Body, checkoutBody) {
		t.Fatal("checkout body must be the shop fixture, not a span payload")
	}
}

func TestFromSpansSkipsInternalAndClientHops(t *testing.T) {
	t.Parallel()
	w := FromSpans([]ingest.Span{
		{ServiceName: "payment", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/authorize"},
		{ServiceName: "processor", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/charge"},
		{ServiceName: "inventory", Kind: "server", HTTPMethod: "POST", HTTPRoute: "/reserve"},
		{ServiceName: "checkout", Kind: "client", HTTPMethod: "POST", HTTPRoute: "/checkout"},
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
