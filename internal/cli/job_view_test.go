package cli

import (
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestWriteJobIsScannable(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	writeJob(&b, jobs.Job{
		ID: "fd0c86f96f3239a6", Status: "complete", Service: "billing-api",
		Baseline: "http://127.0.0.1:29898", Patch: "http://127.0.0.1:29900",
		Tasks: []jobs.Task{{
			Kind: plan.KindBehavior, State: "succeeded", Fence: 2, AttemptID: "b7e6",
			Result: plan.StepResult{Verdict: "incomplete", Notes: []string{"GET /networks/all 401/401 auth_rejected"}},
		}},
	})
	out := b.String()
	for _, want := range []string{"status=complete", "service  billing-api", "verdict=incomplete", "fence=2", "auth_rejected", "validated  false"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{") {
		t.Fatalf("job view must not be a JSON dump:\n%s", out)
	}
}

func TestWriteJobTagsAuthRejectedLatency(t *testing.T) {
	t.Parallel()
	s := replay.Summary{N: 20, MedNS: 100_000, P95NS: 100_000, HasP95: true}
	var b strings.Builder
	writeJob(&b, jobs.Job{ID: "j", Tasks: []jobs.Task{
		{Kind: plan.KindBehavior, Result: plan.StepResult{Steps: []replay.Delta{
			{Method: "GET", Path: "/user/me", BaselineStatus: 401, PatchStatus: 401, Notes: []string{replay.NoteAuthRejected}},
			{Method: "GET", Path: "/health", BaselineStatus: 200, PatchStatus: 200},
		}}},
		{Kind: plan.KindLatency, Result: plan.StepResult{Latency: []replay.StepLatency{
			{Method: "GET", Path: "/user/me", Baseline: s, Patch: s},
			{Method: "GET", Path: "/health", Baseline: s, Patch: s},
		}}},
	}})
	for _, line := range strings.Split(b.String(), "\n") {
		switch {
		case strings.Contains(line, "/user/me") && !strings.HasSuffix(line, "auth_rejected"):
			t.Fatalf("401 latency row not tagged: %q", line)
		case strings.Contains(line, "/health") && strings.Contains(line, "auth_rejected"):
			t.Fatalf("200 row tagged: %q", line)
		}
	}
}
