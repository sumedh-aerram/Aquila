package replay

import (
	"net/http"
	"strings"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

const (
	maxSteps = 20

	ProvenanceSpanRoute   = "span_route"
	ProvenanceSpanAndBody = "span_route+fixture_body"
	ProvenanceShopFixture = "shop_fixture"
)

// Step is one HTTP request in a workload. Bodies are never taken from spans.
type Step struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Body       []byte `json:"-"`
	Provenance string `json:"provenance"`
	Service    string `json:"service,omitempty"`
}

// Workload is an ordered list of requests to send to a gateway.
type Workload struct {
	Steps []Step `json:"steps"`
}

// ShopFixture is the documented shop smoke traffic. It is not derived from traces.
func ShopFixture() Workload {
	return Workload{Steps: []Step{
		{Method: http.MethodGet, Path: "/healthz", Provenance: ProvenanceShopFixture},
		{Method: http.MethodGet, Path: "/users/user-1", Provenance: ProvenanceShopFixture},
		{Method: http.MethodPost, Path: "/checkout", Body: checkoutBody, Provenance: ProvenanceShopFixture},
	}}
}

var checkoutBody = []byte(`{"user_id":"user-1","items":[{"sku":"sku-widget","qty":1}]}`)

// FromSpans builds gateway requests from allowlisted server routes. Client hops
// and internal URLs are omitted. POST /checkout uses the shop fixture body.
func FromSpans(spans []ingest.Span) Workload {
	out := Workload{Steps: []Step{}}
	if len(spans) == 0 {
		return out
	}
	seen := map[string]struct{}{}
	for _, s := range spans {
		if strings.EqualFold(s.Kind, "client") {
			continue
		}
		method, path, ok := routeParts(s)
		if !ok || !gatewayRoute(method, path) {
			continue
		}
		key := method + " " + path
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		st := Step{Method: method, Path: path, Provenance: ProvenanceSpanRoute, Service: s.ServiceName}
		if method == http.MethodPost && path == "/checkout" {
			st.Body = append([]byte(nil), checkoutBody...)
			st.Provenance = ProvenanceSpanAndBody
		} else if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
			continue
		}
		out.Steps = append(out.Steps, st)
		if len(out.Steps) >= maxSteps {
			break
		}
	}
	return out
}

func routeParts(s ingest.Span) (method, path string, ok bool) {
	method = strings.ToUpper(strings.TrimSpace(s.HTTPMethod))
	path = strings.TrimSpace(s.HTTPRoute)
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if i := strings.IndexByte(path, ' '); i >= 0 {
		left, right := strings.ToUpper(path[:i]), path[i+1:]
		if isHTTPMethod(left) {
			if method == "" {
				method = left
			}
			path = right
		}
	}
	if method == "" || path == "" {
		return "", "", false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") || strings.Contains(path, "@") {
		return "", "", false
	}
	return method, path, true
}

func gatewayRoute(method, path string) bool {
	switch method {
	case http.MethodGet:
		return path == "/healthz" || strings.HasPrefix(path, "/users/") || path == "/checkout" || strings.HasPrefix(path, "/checkout/")
	case http.MethodPost:
		return path == "/checkout"
	default:
		return false
	}
}

func isHTTPMethod(s string) bool {
	switch s {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
		return true
	default:
		return false
	}
}
