package impact

import (
	"strings"
	"testing"
)

func TestReplayableRoutesKeepsSafeGET(t *testing.T) {
	t.Parallel()
	got := ReplayableRoutes(Report{Runtime: []Finding{
		{Route: "GET /invoice", Reason: "bound_span"},
		{Name: "POST /authorize", Reason: "bound_span"},
		{Path: "ledger", Reason: "observed_path"},
		{Route: "GET /users/{id}", Reason: "bound_span"},
	}})
	if len(got) != 1 || got[0] != "GET /invoice" {
		t.Fatalf("%v", got)
	}
}

func TestRouteMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		route, method, path string
		want                bool
	}{
		{"GET /latency", "GET", "/latency?token=x", true},
		{"GET /users/{id}", "GET", "/users/42", true},
		{"GET /users/:id", "get", "/users/42", true},
		{"GET /users/{id}", "GET", "/users/", false},
		{"POST /latency", "GET", "/latency", false},
		{"GET /a/b", "GET", "/a", false},
	}
	for _, tc := range tests {
		if got := RouteMatches(tc.route, tc.method, tc.path); got != tc.want {
			t.Errorf("RouteMatches(%q, %q, %q) = %v", tc.route, tc.method, tc.path, got)
		}
	}
}

func TestRuntimeRoutesSkipsPathFindings(t *testing.T) {
	t.Parallel()
	got := RuntimeRoutes(Report{Runtime: []Finding{
		{Name: "path gateway -> payment"},
		{Route: "GET /users/{id}"},
		{Name: "GET /latency?x=1"},
		{Route: "GET /users/{id}"},
	}})
	if strings.Join(got, ",") != "GET /latency,GET /users/{id}" {
		t.Fatalf("got %v", got)
	}
}
