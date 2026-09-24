package replay

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
)

const (
	maxHeaders     = 16
	maxHeaderValue = 4096
)

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Headers that control framing or routing are owned by the HTTP client; an
// operator workload must not be able to smuggle requests or retarget them.
var forbiddenHeaders = map[string]struct{}{
	"Host":              {},
	"Content-Length":    {},
	"Transfer-Encoding": {},
	"Connection":        {},
	"Upgrade":           {},
	"Te":                {},
	"Trailer":           {},
	"Keep-Alive":        {},
}

// fileHeaders validates operator headers. ${NAME} anywhere in a value (for
// example "Bearer ${TOKEN}") is read from the environment so tokens can stay
// out of committed workload files.
func fileHeaders(raw map[string]string) (http.Header, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > maxHeaders {
		return nil, fmt.Errorf("more than %d headers", maxHeaders)
	}
	out := make(http.Header, len(raw))
	for name, value := range raw {
		key := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if key == "" || strings.ContainsAny(key, " \t\r\n:") {
			return nil, fmt.Errorf("invalid header name %q", name)
		}
		if _, bad := forbiddenHeaders[key]; bad || strings.HasPrefix(key, "Proxy-") {
			return nil, fmt.Errorf("header %s is not allowed", key)
		}
		var unset string
		value = envRef.ReplaceAllStringFunc(strings.TrimSpace(value), func(ref string) string {
			name := envRef.FindStringSubmatch(ref)[1]
			v, ok := os.LookupEnv(name)
			if !ok && unset == "" {
				unset = name
			}
			return v
		})
		if unset != "" {
			return nil, fmt.Errorf("header %s references unset $%s", key, unset)
		}
		if len(value) > maxHeaderValue {
			return nil, fmt.Errorf("header %s exceeds %d bytes", key, maxHeaderValue)
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("header %s contains a control character", key)
		}
		out.Set(key, value)
	}
	return out, nil
}

// HeaderNames returns the sorted header names on s. Values are never exposed.
func HeaderNames(s Step) []string {
	if len(s.Headers) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.Headers))
	for k := range s.Headers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
