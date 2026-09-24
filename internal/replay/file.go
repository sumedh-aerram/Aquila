package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	maxFileBytes = 64 << 10
	maxBodyBytes = 8 << 10

	// ProvenanceFile marks a step taken from an operator workload file, not from traces.
	ProvenanceFile = "workload_file"
)

type fileDoc struct {
	Steps []fileStep `json:"steps"`
}

type fileStep struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    json.RawMessage   `json:"body"`
	Headers map[string]string `json:"headers"`
}

// ReadFile loads an operator workload. Bodies come from the file, never from spans.
func ReadFile(path string) (Workload, error) {
	if strings.TrimSpace(path) == "" {
		return Workload{}, fmt.Errorf("replay: workload file path is empty")
	}
	if strings.Contains(path, "://") {
		return Workload{}, fmt.Errorf("replay: workload file must be a local path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Workload{}, fmt.Errorf("replay: read workload: %w", err)
	}
	if len(raw) > maxFileBytes {
		return Workload{}, fmt.Errorf("replay: workload file exceeds %d bytes", maxFileBytes)
	}
	var doc fileDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Workload{}, fmt.Errorf("replay: parse workload: %w", err)
	}
	if len(doc.Steps) == 0 {
		return Workload{Steps: []Step{}}, fmt.Errorf("replay: workload file has no steps")
	}
	if len(doc.Steps) > maxSteps {
		return Workload{}, fmt.Errorf("replay: workload file exceeds %d steps", maxSteps)
	}
	out := Workload{Steps: make([]Step, 0, len(doc.Steps))}
	for i, st := range doc.Steps {
		method := strings.ToUpper(strings.TrimSpace(st.Method))
		p := strings.TrimSpace(st.Path)
		if !safeFileStep(method, p) {
			return Workload{}, fmt.Errorf("replay: workload step %d is not replayable", i)
		}
		body, err := decodeBody(st.Body)
		if err != nil {
			return Workload{}, fmt.Errorf("replay: workload step %d: %w", i, err)
		}
		hdr, err := fileHeaders(st.Headers)
		if err != nil {
			return Workload{}, fmt.Errorf("replay: workload step %d: %w", i, err)
		}
		out.Steps = append(out.Steps, Step{
			Method:     method,
			Path:       p,
			Body:       body,
			Headers:    hdr,
			Provenance: ProvenanceFile,
		})
	}
	return out, nil
}

func decodeBody(raw json.RawMessage) ([]byte, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	trim := bytes.TrimSpace(raw)
	if len(trim) > 0 && trim[0] == '"' {
		var s string
		if err := json.Unmarshal(trim, &s); err != nil {
			return nil, err
		}
		if len(s) > maxBodyBytes {
			return nil, fmt.Errorf("body exceeds %d bytes", maxBodyBytes)
		}
		return []byte(s), nil
	}
	if len(trim) > maxBodyBytes {
		return nil, fmt.Errorf("body exceeds %d bytes", maxBodyBytes)
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trim); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeFileStep(method, path string) bool {
	if !isHTTPMethod(method) {
		return false
	}
	return safePath(path)
}

func safePath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	if strings.Contains(path, "..") || strings.Contains(path, "\\") || strings.Contains(path, "@") {
		return false
	}
	if strings.ContainsAny(path, "{}*") {
		return false
	}
	if strings.Contains(path, "://") {
		return false
	}
	return true
}

func safeReplay(method, path string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return false
	}
	return safePath(path)
}
