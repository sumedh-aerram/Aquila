package replay

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

const (
	VerdictMatch      = "match"
	VerdictDiffer     = "differ"
	VerdictIncomplete = "incomplete"
)

// Delta is one step comparison. There is no pass score.
type Delta struct {
	Method string   `json:"method"`
	Path   string   `json:"path"`
	Status string   `json:"status"`
	Notes  []string `json:"notes,omitempty"`
}

// Report compares two replays of the same workload.
type Report struct {
	Verdict string  `json:"verdict"`
	Steps   []Delta `json:"steps"`
}

var volatileKeys = map[string]struct{}{
	"id":          {},
	"timestamp":   {},
	"request_id":  {},
	"trace_id":    {},
	"span_id":     {},
	"created_at":  {},
	"updated_at":  {},
	"checkout_id": {},
}

// Compare returns match, differ, or incomplete. Timeout and transport errors
// are incomplete, not a match. Extra JSON fields are a difference.
func Compare(base, patch Result) Report {
	n := len(base.Steps)
	if len(patch.Steps) < n {
		n = len(patch.Steps)
	}
	rep := Report{Steps: make([]Delta, 0, n)}
	incomplete := false
	differ := false
	if len(base.Steps) != len(patch.Steps) {
		incomplete = true
	}
	for i := 0; i < n; i++ {
		d := compareObs(base.Steps[i], patch.Steps[i])
		rep.Steps = append(rep.Steps, d)
		for _, note := range d.Notes {
			if strings.HasPrefix(note, "error:") {
				incomplete = true
			}
		}
		if d.Status == VerdictDiffer {
			differ = true
		}
		if d.Status == VerdictIncomplete {
			incomplete = true
		}
	}
	switch {
	case incomplete:
		rep.Verdict = VerdictIncomplete
	case differ:
		rep.Verdict = VerdictDiffer
	default:
		rep.Verdict = VerdictMatch
	}
	return rep
}

func compareObs(a, b Observation) Delta {
	d := Delta{Method: a.Method, Path: a.Path, Status: VerdictMatch}
	if a.Path != b.Path || a.Method != b.Method {
		d.Status = VerdictIncomplete
		d.Notes = append(d.Notes, "step mismatch")
		return d
	}
	if a.Err != "" || b.Err != "" {
		d.Status = VerdictIncomplete
		if a.Err != "" {
			d.Notes = append(d.Notes, "error:baseline:"+a.Err)
		}
		if b.Err != "" {
			d.Notes = append(d.Notes, "error:patch:"+b.Err)
		}
		return d
	}
	if a.Status != b.Status {
		d.Status = VerdictDiffer
		d.Notes = append(d.Notes, "status")
	}
	notes := jsonNotes(a.Body, b.Body)
	if len(notes) > 0 {
		d.Status = VerdictDiffer
		d.Notes = append(d.Notes, notes...)
	}
	return d
}

func jsonNotes(a, b []byte) []string {
	va, oka := decodeJSON(a)
	vb, okb := decodeJSON(b)
	if !oka && !okb {
		if bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b)) {
			return nil
		}
		return []string{"opaque body"}
	}
	if oka != okb {
		return []string{"json vs opaque"}
	}
	na := stripVolatile(va)
	nb := stripVolatile(vb)
	if reflect.DeepEqual(na, nb) {
		return nil
	}
	return []string{"json"}
}

func decodeJSON(raw []byte) (any, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	return v, true
}

func stripVolatile(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if _, skip := volatileKeys[k]; skip {
				continue
			}
			out[k] = stripVolatile(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = stripVolatile(val)
		}
		return out
	default:
		return v
	}
}
