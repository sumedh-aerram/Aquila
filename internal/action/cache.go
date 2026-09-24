package action

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sumedhaerram/aquila/internal/cas"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
)

// Key identifies a replay action. It does not include wall-clock time.
type Key struct {
	Kind        string `json:"kind"`
	Baseline    string `json:"baseline"`
	Patch       string `json:"patch"`
	BaselineSHA string `json:"baseline_sha"`
	N           int    `json:"n"`
	Workload    string `json:"workload"`
	HealthPath  string `json:"health_path,omitempty"`
}

// Digest is the SHA-256 of the canonical key.
func Digest(k Key) string {
	raw, _ := json.Marshal(k)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// FromLease builds a cache key for one leased task.
func FromLease(lease jobs.Lease) Key {
	w, _ := json.Marshal(lease.Job.Workload)
	n := lease.N
	if n < 1 {
		n = 1
	}
	return Key{
		Kind:        lease.Task.Kind,
		Baseline:    lease.Job.Baseline,
		Patch:       lease.Job.Patch,
		BaselineSHA: lease.Job.BaselineSHA,
		N:           n,
		Workload:    string(w),
		HealthPath:  lease.Job.Plan.HealthPath,
	}
}

// Cache maps action digests to CAS object digests.
type Cache struct {
	dir *cas.Dir
	idx string
}

// Open returns a cache using objects in casRoot.
func Open(casRoot string) (*Cache, error) {
	d, err := cas.Open(casRoot)
	if err != nil {
		return nil, err
	}
	idx := filepath.Join(casRoot, "cache")
	if err := os.MkdirAll(idx, 0o755); err != nil {
		return nil, fmt.Errorf("action: %w", err)
	}
	return &Cache{dir: d, idx: idx}, nil
}

func (c *Cache) file(key Key) string {
	return filepath.Join(c.idx, Digest(key))
}

// Get returns a cached step result.
func (c *Cache) Get(ctx context.Context, key Key) (plan.StepResult, bool, error) {
	if c == nil {
		return plan.StepResult{}, false, nil
	}
	raw, err := os.ReadFile(c.file(key))
	if err != nil {
		if os.IsNotExist(err) {
			return plan.StepResult{}, false, nil
		}
		return plan.StepResult{}, false, fmt.Errorf("action: %w", err)
	}
	id := cas.Digest(strings.TrimSpace(string(raw)))
	body, err := c.dir.Get(ctx, id)
	if err != nil {
		return plan.StepResult{}, false, err
	}
	var res plan.StepResult
	if err := json.Unmarshal(body, &res); err != nil {
		return plan.StepResult{}, false, fmt.Errorf("action: %w", err)
	}
	if res.Verdict == "pass" || res.Verdict == "validated" {
		return plan.StepResult{}, false, fmt.Errorf("action: cached pass")
	}
	return res, true, nil
}

// Put stores res under key. Incomplete and failed verdicts are not cached.
func (c *Cache) Put(ctx context.Context, key Key, res plan.StepResult) error {
	if c == nil {
		return nil
	}
	if res.Verdict == "" || res.Verdict == "incomplete" || res.Verdict == "pass" || res.Verdict == "validated" {
		return nil
	}
	body, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("action: %w", err)
	}
	id, err := c.dir.Put(ctx, body)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.idx, ".idx-*")
	if err != nil {
		return fmt.Errorf("action: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(string(id)); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("action: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("action: %w", err)
	}
	return os.Rename(name, c.file(key))
}
