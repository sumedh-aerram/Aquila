package runs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/evidence"
)

const (
	DefaultList    = 20
	MaxList        = 50
	idLen          = 16
	maxServiceName = 128
)

// ErrNotFound is returned when a run id does not exist.
var ErrNotFound = errors.New("runs: not found")

// Record is one persisted experiment. Validated is earned, not claimed.
type Record struct {
	ID             string
	Recorded       time.Time
	Service        string
	BaselineSHA    string
	Dirty          bool
	WorkloadDigest string
	ArtifactDigest string
	Overall        string
	Validated      bool
	Artifact       evidence.Artifact
}

// Store persists executed experiment evidence.
type Store interface {
	Insert(ctx context.Context, rec Record) (Record, error)
	Get(ctx context.Context, id string) (Record, error)
	List(ctx context.Context, limit int, service string) ([]Record, error)
}

// FromArtifact builds a record from checked evidence. The id is the artifact digest prefix.
func FromArtifact(a evidence.Artifact) (Record, error) {
	if err := evidence.Check(a); err != nil {
		return Record{}, err
	}
	if a.ArtifactDigest == "" {
		return Record{}, fmt.Errorf("runs: missing artifact digest")
	}
	id, ok := NormalizeID(a.ArtifactDigest[:min(len(a.ArtifactDigest), idLen)])
	if !ok {
		return Record{}, fmt.Errorf("runs: invalid artifact digest")
	}
	return Record{
		ID:             id,
		Recorded:       a.Recorded.UTC(),
		Service:        a.Service,
		BaselineSHA:    a.BaselineSHA,
		Dirty:          a.Dirty,
		WorkloadDigest: a.WorkloadDigest,
		ArtifactDigest: a.ArtifactDigest,
		Overall:        a.Result.Overall,
		Validated:      a.Validated,
		Artifact:       a,
	}, nil
}

// NormalizeID accepts a lowercase hex run id.
func NormalizeID(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if len(s) < idLen || len(s) > 64 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	if len(s) > idLen {
		s = s[:idLen]
	}
	return s, true
}

func clipLimit(n int) int {
	if n <= 0 {
		return DefaultList
	}
	if n > MaxList {
		return MaxList
	}
	return n
}

func clipService(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxServiceName {
		s = s[:maxServiceName]
	}
	return s
}
