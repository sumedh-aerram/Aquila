package impact

import (
	"context"
	"embed"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

//go:embed testdata/*.diff
var evalDiffs embed.FS

var shopOnce = sync.OnceValues(func() (*source.Graph, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return source.Load(ctx, shopDir())
})

func loadedShop(t *testing.T) *source.Graph {
	t.Helper()
	g, err := shopOnce()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestScoreSynthetic(t *testing.T) {
	t.Parallel()
	rep := Report{
		Direct: []Finding{{Name: "chargeProcessor", Reason: "changed_lines"}},
		Likely: []Finding{{Name: "authorize", Reason: "caller"}},
	}
	cs := Score(rep, Label{ID: "D1", Direct: []string{"chargeProcessor"}, Likely: []string{"authorize"}, ForbidLikely: []string{"NewEphemeral"}})
	if cs.DirectHit != 1 || cs.LikelyHit != 1 || len(cs.Misses) != 0 || len(cs.ForbiddenLikely) != 0 {
		t.Fatalf("%+v", cs)
	}
	m := Aggregate([]CaseScore{cs})
	if m.DirectRecall != 1 || m.LikelyRecall != 1 || m.DirectPrecision != 1 {
		t.Fatalf("%+v", m)
	}
}

func TestScoreMissAndForbidden(t *testing.T) {
	t.Parallel()
	rep := Report{
		Direct: []Finding{{Name: "other", Reason: "changed_lines"}},
		Likely: []Finding{{Name: "NewEphemeral", Reason: "caller"}},
	}
	cs := Score(rep, Label{ID: "D1", Direct: []string{"chargeProcessor"}, ForbidLikely: []string{"NewEphemeral"}})
	if len(cs.Misses) == 0 || len(cs.ForbiddenLikely) == 0 {
		t.Fatalf("%+v", cs)
	}
}

func TestEvalShopDefects(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	cases := []struct {
		file string
		lab  Label
	}{
		{"d1.diff", Label{ID: "D1", Direct: []string{"chargeProcessor"}, Likely: []string{"authorize"}, ForbidLikely: []string{"NewEphemeral"}}},
		{"d2.diff", Label{ID: "D2", Direct: []string{"GetUser"}}},
		{"d3.diff", Label{ID: "D3", Direct: []string{"mutate"}, Likely: []string{"reserve", "release"}}},
		{"d4.diff", Label{ID: "D4", Direct: []string{"create"}}},
		{"d5.diff", Label{ID: "D5", Direct: []string{"create"}}},
		{"d6.diff", Label{ID: "D6", Direct: []string{"mutate"}, Likely: []string{"reserve", "release"}}},
		{"unknown_sql.diff", Label{ID: "sql", Unobserved: "unknown_file"}},
	}
	var scores []CaseScore
	for _, tc := range cases {
		raw, err := evalDiffs.ReadFile(path.Join("testdata", tc.file))
		if err != nil {
			t.Fatal(err)
		}
		d, err := diff.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.file, err)
		}
		rep := Analyze(g, d, locate.Snapshot{}, graph.Snapshot{})
		cs := Score(rep, tc.lab)
		if len(cs.Misses) > 0 || len(cs.ForbiddenLikely) > 0 {
			t.Errorf("%s report=%+v score=%+v", tc.file, rep, cs)
		}
		if tc.lab.ID == "D4" || tc.lab.ID == "D5" {
			if !hasFinding(rep.Direct, "create") {
				t.Errorf("%s should map onto create; got %+v", tc.file, rep.Direct)
			}
		}
		scores = append(scores, cs)
	}
	m := Aggregate(scores)
	if m.DirectRecall != 1 {
		t.Fatalf("direct recall=%v cases=%+v", m.DirectRecall, m.Cases)
	}
	if m.LikelyRecall != 1 {
		t.Fatalf("likely recall=%v cases=%+v", m.LikelyRecall, m.Cases)
	}
}

func TestEvalD4D5ShareCreate(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	d4 := mustParseEval(t, "d4.diff")
	d5 := mustParseEval(t, "d5.diff")
	r4 := Analyze(g, d4, locate.Snapshot{}, graph.Snapshot{})
	r5 := Analyze(g, d5, locate.Snapshot{}, graph.Snapshot{})
	if !hasFinding(r4.Direct, "create") || !hasFinding(r5.Direct, "create") {
		t.Fatalf("d4=%+v d5=%+v", r4.Direct, r5.Direct)
	}
}

func mustParseEval(t *testing.T, name string) diff.Diff {
	t.Helper()
	raw, err := evalDiffs.ReadFile(path.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	d, err := diff.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
