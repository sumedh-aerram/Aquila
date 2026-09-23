package replay

import (
	"net/http"
	"strings"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

const (
	maxSteps = 20

	// ProvenanceSpanRoute marks a step taken from a stored server-span route.
	ProvenanceSpanRoute = "span_route"
	// ProvenanceShopFixture marks a step from the shop smoke fixture, not from traces.
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

// FromSpans builds GET/HEAD/OPTIONS requests from server-span routes.
// Client hops, mutating methods, and parameterized templates are omitted.
// Bodies are never taken from spans.
func FromSpans(spans []ingest.Span) Workload {
	out := Workload{Steps: []Step{}}
	if len(spans) == 0 {
		return out
	}
	seen := map[string]struct{}{}
	for _, s := range spans {
		if skipKind(s.Kind) {
			continue
		}
		method, path, ok := routeParts(s)
		if !ok || !safeReplay(method, path) {
			continue
		}
		key := method + " " + path
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out.Steps = append(out.Steps, Step{
			Method:     method,
			Path:       path,
			Provenance: ProvenanceSpanRoute,
			Service:    s.ServiceName,
		})
		if len(out.Steps) >= maxSteps {
			break
		}
	}
	return out
}

func skipKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "client", "producer", "consumer", "internal":
		return true
	default:
		return false
	}
}

func safeReplay(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}
	if !strings.HasPrefix(path, "/") {
		return false
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") || strings.Contains(path, "@") {
		return false
	}
	if strings.ContainsAny(path, "{}*") {
		return false
	}
	return true
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
	return method, path, true
}

func isHTTPMethod(s string) bool {
	switch s {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
