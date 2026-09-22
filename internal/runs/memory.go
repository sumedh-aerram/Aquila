package runs

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Memory is an in-process Store for tests.
type Memory struct {
	mu    sync.Mutex
	byID  map[string]Record
	order []string
}

// NewMemory returns an empty Memory store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]Record)}
}

// Insert implements Store. Duplicate ids with the same digest are idempotent.
func (m *Memory) Insert(ctx context.Context, rec Record) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	prepared, err := prepareInsert(rec)
	if err != nil {
		return Record{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = make(map[string]Record)
	}
	if old, ok := m.byID[prepared.ID]; ok {
		if old.ArtifactDigest != prepared.ArtifactDigest {
			return Record{}, fmt.Errorf("runs: id collision")
		}
		return old, nil
	}
	m.byID[prepared.ID] = prepared
	m.order = append(m.order, prepared.ID)
	return prepared, nil
}

// Get implements Store.
func (m *Memory) Get(ctx context.Context, id string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	id, ok := NormalizeID(id)
	if !ok {
		return Record{}, fmt.Errorf("runs: invalid id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.byID[id]
	if !ok {
		return Record{}, ErrNotFound
	}
	return rec, nil
}

// List implements Store. Newest recorded_at first. Artifact bodies are omitted.
func (m *Memory) List(ctx context.Context, limit int) ([]Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit = clipLimit(limit)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Recorded.Equal(out[j].Recorded) {
			return out[i].ID > out[j].ID
		}
		return out[i].Recorded.After(out[j].Recorded)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return summaries(out), nil
}

func summaries(in []Record) []Record {
	out := make([]Record, len(in))
	for i, r := range in {
		out[i] = Record{
			ID:             r.ID,
			Recorded:       r.Recorded,
			BaselineSHA:    r.BaselineSHA,
			Dirty:          r.Dirty,
			WorkloadDigest: r.WorkloadDigest,
			ArtifactDigest: r.ArtifactDigest,
			Overall:        r.Overall,
			Validated:      false,
		}
	}
	return out
}

func prepareInsert(rec Record) (Record, error) {
	if rec.Artifact.Schema == "" {
		return Record{}, fmt.Errorf("runs: missing artifact")
	}
	built, err := FromArtifact(rec.Artifact)
	if err != nil {
		return Record{}, err
	}
	if rec.ID != "" && rec.ID != built.ID {
		return Record{}, fmt.Errorf("runs: id does not match artifact")
	}
	return built, nil
}

var _ Store = (*Memory)(nil)
