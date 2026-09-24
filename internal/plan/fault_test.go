package plan

import (
	"testing"

	"github.com/sumedhaerram/aquila/internal/replay"
)

func burst(statuses ...int) []replay.Result {
	r := replay.Result{}
	for _, s := range statuses {
		r.Steps = append(r.Steps, replay.Observation{Method: "GET", Path: "/x", Status: s})
	}
	return []replay.Result{r}
}

func TestCompareBurstIsSymmetric(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		base, patch []replay.Result
		want        string
	}{
		{"same errors", burst(200, 500), burst(200, 500), replay.VerdictMatch},
		{"patch more", burst(200, 200), burst(200, 500), replay.VerdictDiffer},
		{"patch less", burst(500, 500), burst(404, 404), replay.VerdictDiffer},
		{"no samples", nil, burst(200), replay.VerdictIncomplete},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compareBurst(tc.base, tc.patch); got.Verdict != tc.want {
				t.Fatalf("verdict=%s notes=%v", got.Verdict, got.Notes)
			}
		})
	}
}
