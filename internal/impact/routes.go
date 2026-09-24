package impact

import (
	"net/http"
	"sort"
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

// RuntimeRoutes returns every "METHOD /path" joined to the change at runtime,
// sorted and deduplicated. Findings without a method and path are omitted.
func RuntimeRoutes(rep Report) []string {
	seen := map[string]struct{}{}
	for _, f := range rep.Runtime {
		raw := strings.TrimSpace(f.Route)
		if raw == "" {
			raw = strings.TrimSpace(f.Name)
		}
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) != 2 || !strings.HasPrefix(strings.TrimSpace(parts[1]), "/") {
			continue
		}
		method, path, ok := splitRoute(raw)
		if !ok || !isMethod(method) || strings.Contains(path, " ") {
			continue
		}
		seen[method+" "+path] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// RouteMatches reports whether a request method and path hit route
// ("METHOD /template"). {name} and :name segments are single-segment
// wildcards, the forms net/http, chi, and gorilla use. Queries are ignored.
func RouteMatches(route, method, path string) bool {
	rm, tmpl, ok := splitRoute(route)
	if !ok || !strings.EqualFold(rm, method) {
		return false
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ts := strings.Split(strings.Trim(tmpl, "/"), "/")
	ps := strings.Split(strings.Trim(path, "/"), "/")
	if len(ts) != len(ps) {
		return false
	}
	for i, t := range ts {
		wild := (strings.HasPrefix(t, "{") && strings.HasSuffix(t, "}")) || strings.HasPrefix(t, ":")
		if wild && ps[i] != "" {
			continue
		}
		if t != ps[i] {
			return false
		}
	}
	return true
}

func isMethod(m string) bool {
	for _, c := range m {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return m != ""
}
