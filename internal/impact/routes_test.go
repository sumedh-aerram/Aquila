package impact

import (
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
