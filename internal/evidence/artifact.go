package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/validate"
)

const (
	// Evidence schema versions retain historical digests and distinguish strict writes.
	SchemaV3 = "aquila.evidence.v3"
	SchemaV2 = "aquila.evidence.v2"
	SchemaV1 = "aquila.evidence.v1"
	MaxBytes = 1 << 20
)

// Artifact records one local experiment, including its classified changes,
// executed results and validation decision.
type Artifact struct {
	BaselineRev    gitrev.Revision    `json:"baseline_rev,omitzero"`
	PatchRev       gitrev.Revision    `json:"patch_rev,omitzero"`
	Schema         string             `json:"schema"`
	Recorded       time.Time          `json:"recorded_at"`
	Baseline       string             `json:"baseline"`
	Patch          string             `json:"patch"`
	BaselineSHA    string             `json:"baseline_sha"`
	Dirty          bool               `json:"dirty"`
	Service        string             `json:"service,omitempty"`
	WorkloadDigest string             `json:"workload_digest"`
	ArtifactDigest string             `json:"artifact_digest"`
	Workload       []Request          `json:"workload"`
	Impact         ImpactSummary      `json:"impact"`
	Coverage       *Coverage          `json:"coverage,omitempty"`
	Plan           plan.DAG           `json:"plan"`
	Result         plan.Evidence      `json:"result"`
	Validated      bool               `json:"validated"`
	Blockers       []validate.Blocker `json:"blockers,omitempty"`
}

// Request is one workload step without a body. Headers lists names only;
// values such as bearer tokens are never persisted.
type Request struct {
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Provenance string   `json:"provenance,omitempty"`
	Headers    []string `json:"headers,omitempty"`
}

// ImpactSummary is blast-radius counts and names. It does not keep hunk text.
type ImpactSummary struct {
	Changes    impact.Changes `json:"changes,omitzero"`
	Files      int            `json:"files"`
	Direct     int            `json:"direct"`
	Likely     int            `json:"likely"`
	Runtime    int            `json:"runtime"`
	Unobserved int            `json:"unobserved"`
	Changed    []string       `json:"changed_files,omitempty"`
	DirectName []string       `json:"direct_names,omitempty"`
}

// Input is the executed experiment pieces needed to persist evidence.
type Input struct {
	BaselineRev gitrev.Revision
	PatchRev    gitrev.Revision
	Now         time.Time
	Baseline    string
	Patch       string
	Service     string
	Workload    replay.Workload
	Impact      impact.Report
	Plan        plan.DAG
	Result      plan.Evidence
}

// Build constructs an artifact. Bodies are omitted. Validated is earned, not claimed.
func Build(in Input) Artifact {
	now := in.Now.UTC().Truncate(time.Second)
	if now.IsZero() {
		now = time.Now().UTC().Truncate(time.Second)
	}
	a := Artifact{
		Schema:      SchemaV3,
		Recorded:    now,
		Baseline:    in.Baseline,
		Patch:       in.Patch,
		BaselineSHA: in.BaselineRev.Commit,
		BaselineRev: in.BaselineRev,
		PatchRev:    in.PatchRev,
		Dirty:       in.PatchRev.Dirty,
		Service:     clipService(in.Service),
		Workload:    requests(in.Workload),
		Impact:      summarizeImpact(in.Impact),
		Plan:        in.Plan,
		Result:      in.Result,
	}
	a.Coverage = coverageOf(in.Impact, a.Workload, in.Result)
	d := a.Decision()
	a.Validated, a.Blockers = d.Validated, d.Blockers
	a.WorkloadDigest = digestWorkload(a.Workload)
	a.ArtifactDigest = digestArtifact(a)
	return a
}

func requests(w replay.Workload) []Request {
	out := make([]Request, 0, len(w.Steps))
	for _, s := range w.Steps {
		out = append(out, Request{Method: s.Method, Path: s.Path, Provenance: s.Provenance, Headers: replay.HeaderNames(s)})
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
		Changes:    impact.Classify(rep),
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

// Decode reads checked evidence while preserving legacy validation claims as history.
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
	// Unknown fields would be dropped and then fail the digest; naming them
	// turns CLI/server version skew into an actionable error.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var a Artifact
	if err := dec.Decode(&a); err != nil {
		return Artifact{}, fmt.Errorf("evidence: %w", err)
	}
	check := Verify
	if a.Schema == SchemaV3 {
		check = Check
	}
	if err := check(a); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// Check requires current-schema evidence whose claims agree with grading policy.
func Check(a Artifact) error {
	if err := Verify(a); err != nil {
		return err
	}
	if a.Schema != SchemaV3 {
		return fmt.Errorf("evidence: writes require schema %s", SchemaV3)
	}
	d := a.Decision()
	if a.Validated != d.Validated {
		return fmt.Errorf("evidence: validated is unearned")
	}
	if !slices.Equal(a.Blockers, d.Blockers) {
		return fmt.Errorf("evidence: blockers do not match decision")
	}
	return nil
}

// Verify checks schema, verdict and content integrity without applying policy.
func Verify(a Artifact) error {
	if a.Schema != SchemaV1 && a.Schema != SchemaV2 && a.Schema != SchemaV3 {
		return fmt.Errorf("evidence: unsupported schema %q", a.Schema)
	}
	switch strings.ToLower(strings.TrimSpace(a.Result.Overall)) {
	case replay.VerdictMatch, replay.VerdictDiffer, replay.VerdictIncomplete:
	case "pass", "validated", "ok", "fail", "failed", "success":
		return fmt.Errorf("evidence: overall %q is not a legal verdict", a.Result.Overall)
	case "":
		return fmt.Errorf("evidence: missing overall")
	default:
		return fmt.Errorf("evidence: unknown overall %q", a.Result.Overall)
	}
	if (a.Schema != SchemaV1 || a.WorkloadDigest != "") && a.WorkloadDigest != digestWorkload(a.Workload) {
		return fmt.Errorf("evidence: workload digest mismatch")
	}
	if (a.Schema != SchemaV1 || a.ArtifactDigest != "") && a.ArtifactDigest != digestArtifact(a) {
		return fmt.Errorf("evidence: artifact digest mismatch")
	}
	return nil
}

// Decision derives current policy without changing persisted evidence.
func (a Artifact) Decision() validate.Decision {
	if a.Schema == SchemaV1 {
		return validate.Legacy()
	}
	if a.Schema == SchemaV2 {
		return validate.Decision{Blockers: []validate.Blocker{
			{Code: validate.CodeUnverifiedRevision, Subject: "baseline"},
			{Code: validate.CodeUnverifiedRevision, Subject: "patch"},
		}}
	}
	in := validate.Input{
		BaselineRev: a.BaselineRev,
		PatchRev:    a.PatchRev,
		Baseline:    a.Baseline,
		Patch:       a.Patch,
		Plan:        a.Plan,
		Result:      a.Result,
	}
	if a.Coverage != nil {
		in.Impacted = a.Coverage.Impacted
	}
	in.Changes = a.Impact.Changes
	for _, r := range a.Workload {
		in.Workload = append(in.Workload, replay.Step{Method: r.Method, Path: r.Path})
	}
	return validate.Decide(in)
}

func digestWorkload(rs []Request) string {
	h := sha256.New()
	for _, r := range rs {
		if len(r.Headers) == 0 {
			_, _ = fmt.Fprintf(h, "%s %s %s\n", r.Method, r.Path, r.Provenance)
			continue
		}
		_, _ = fmt.Fprintf(h, "%s %s %s h=%s\n", r.Method, r.Path, r.Provenance, strings.Join(r.Headers, ","))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func clipService(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 128 {
		s = s[:128]
	}
	return s
}

func digestArtifact(a Artifact) string {
	cp := a
	cp.ArtifactDigest = ""
	raw, err := json.Marshal(cp)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
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
