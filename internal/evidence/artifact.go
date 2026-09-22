package evidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const (
	SchemaV1 = "aquila.evidence.v1"
	MaxBytes = 1 << 20
)

// Artifact is the durable record of one local experiment. It is never a pass.
type Artifact struct {
	Schema    string        `json:"schema"`
	Recorded  time.Time     `json:"recorded_at"`
	Baseline  string        `json:"baseline"`
	Patch     string        `json:"patch"`
	Workload  []Request     `json:"workload"`
	Impact    ImpactSummary `json:"impact"`
	Plan      plan.DAG      `json:"plan"`
	Result    plan.Evidence `json:"result"`
	Validated bool          `json:"validated"`
}

// Request is one workload step without a body.
type Request struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Provenance string `json:"provenance,omitempty"`
}

// ImpactSummary is blast-radius counts and names. It does not keep hunk text.
type ImpactSummary struct {
	Files      int      `json:"files"`
	Direct     int      `json:"direct"`
	Likely     int      `json:"likely"`
	Runtime    int      `json:"runtime"`
	Unobserved int      `json:"unobserved"`
	Changed    []string `json:"changed_files,omitempty"`
	DirectName []string `json:"direct_names,omitempty"`
}

// Input is the executed experiment pieces needed to persist evidence.
type Input struct {
	Now      time.Time
	Baseline string
	Patch    string
	Workload replay.Workload
	Impact   impact.Report
	Plan     plan.DAG
	Result   plan.Evidence
}

// Build constructs an artifact. Validated is always false. Bodies are omitted.
func Build(in Input) Artifact {
	now := in.Now.UTC().Truncate(time.Second)
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Second)
	}
	return Artifact{
		Schema:    SchemaV1,
		Recorded:  now,
		Baseline:  in.Baseline,
		Patch:     in.Patch,
		Workload:  requests(in.Workload),
		Impact:    summarizeImpact(in.Impact),
		Plan:      in.Plan,
		Result:    in.Result,
		Validated: false,
	}
}

func requests(w replay.Workload) []Request {
	out := make([]Request, 0, len(w.Steps))
	for _, s := range w.Steps {
		out = append(out, Request{Method: s.Method, Path: s.Path, Provenance: s.Provenance})
	}
	return out
}

func summarizeImpact(rep impact.Report) ImpactSummary {
	names := make([]string, 0, len(rep.Direct))
	for _, f := range rep.Direct {
		if f.Name != "" {
			names = append(names, f.Name)
		}
	}
	return ImpactSummary{
		Files:      len(rep.Files),
		Direct:     len(rep.Direct),
		Likely:     len(rep.Likely),
		Runtime:    len(rep.Runtime),
		Unobserved: len(rep.Unobserved),
		Changed:    append([]string(nil), rep.Files...),
		DirectName: names,
	}
}

// Marshal encodes a checked artifact as indented JSON.
func Marshal(a Artifact) ([]byte, error) {
	if err := Check(a); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > MaxBytes {
		return nil, fmt.Errorf("evidence: artifact too large")
	}
	return raw, nil
}

// Decode reads a JSON artifact. It rejects pass/validated claims.
func Decode(r io.Reader) (Artifact, error) {
	raw, err := io.ReadAll(io.LimitReader(r, int64(MaxBytes)+1))
	if err != nil {
		return Artifact{}, fmt.Errorf("evidence: %w", err)
	}
	if len(raw) == 0 {
		return Artifact{}, fmt.Errorf("evidence: empty artifact")
	}
	if len(raw) > MaxBytes {
		return Artifact{}, fmt.Errorf("evidence: artifact too large")
	}
	var a Artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		return Artifact{}, fmt.Errorf("evidence: %w", err)
	}
	if err := Check(a); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// Check rejects unsupported schemas and any pass/validated claim.
func Check(a Artifact) error {
	if a.Schema != SchemaV1 {
		return fmt.Errorf("evidence: unsupported schema %q", a.Schema)
	}
	if a.Validated {
		return fmt.Errorf("evidence: validated must be false")
	}
	switch strings.ToLower(strings.TrimSpace(a.Result.Overall)) {
	case replay.VerdictMatch, replay.VerdictDiffer, replay.VerdictIncomplete:
		return nil
	case "pass", "validated", "ok", "fail", "failed", "success":
		return fmt.Errorf("evidence: overall %q is not a legal verdict", a.Result.Overall)
	case "":
		return fmt.Errorf("evidence: missing overall")
	default:
		return fmt.Errorf("evidence: unknown overall %q", a.Result.Overall)
	}
}

// EqualJSON reports whether two artifacts encode to the same bytes.
func EqualJSON(a, b Artifact) bool {
	ra, err := Marshal(a)
	if err != nil {
		return false
	}
	rb, err := Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}
