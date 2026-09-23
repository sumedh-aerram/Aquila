package impact

import (
	"net/http"
	"strings"
)

// ReplayableRoutes returns GET/HEAD/OPTIONS routes joined to the local edit.
// Observed paths without a method and path are omitted. Nothing is guessed.
func ReplayableRoutes(rep Report) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range rep.Runtime {
		label, ok := replayLabel(f)
		if !ok {
			continue
		}
		if _, dup := seen[label]; dup {
			continue
		}
		seen[label] = struct{}{}
		out = append(out, label)
	}
	return out
}

func replayLabel(f Finding) (string, bool) {
	raw := strings.TrimSpace(f.Route)
	if raw == "" {
		raw = strings.TrimSpace(f.Name)
	}
	method, path, ok := splitRoute(raw)
	if !ok {
		return "", false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
	default:
		return "", false
	}
	if strings.ContainsAny(path, "{}*") || strings.Contains(path, "..") {
		return "", false
	}
	return method + " " + path, true
}

func splitRoute(raw string) (method, path string, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(raw), " ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	method = strings.ToUpper(strings.TrimSpace(parts[0]))
	path = strings.TrimSpace(parts[1])
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if method == "" || path == "" {
		return "", "", false
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return method, path, true
}
