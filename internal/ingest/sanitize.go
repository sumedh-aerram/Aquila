package ingest

import (
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Terminal escape sequences are removed whole; dropping only the ESC byte
// would leave "[31m" in operator output.
var ansiSeq = regexp.MustCompile("(\x1b\\[|\u009b)[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(\x07|\x1b\\\\)?|\x1b[@-Z\\\\-_]")

func sanitizeText(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	s = ansiSeq.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
	return clipRunes(strings.TrimSpace(s), max)
}

func sanitizeName(s string) string {
	s = stripQueryAndFragment(sanitizeText(s, maxString))
	return stripAbsoluteURL(s)
}

func sanitizeRoute(s string) string {
	s = sanitizeText(s, maxString)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		return stripAbsoluteURL(s)
	}
	return stripQueryAndFragment(s)
}

func sanitizeFile(s string) string {
	s = sanitizeText(s, maxString)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Clean(s)
	if s == "." || s == "/" || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") || strings.Contains(s, "..") {
		return ""
	}
	return s
}

func stripQueryAndFragment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i]
	}
	return s
}

func stripAbsoluteURL(s string) string {
	idx := strings.Index(s, "://")
	if idx < 0 {
		return s
	}
	start := idx
	for start > 0 {
		c := s[start-1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			start--
			continue
		}
		break
	}
	raw := s[start:]
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	p := u.EscapedPath()
	if p == "" {
		p = "/"
	}
	prefix := strings.TrimSpace(s[:start])
	if prefix == "" {
		return p
	}
	return strings.TrimSpace(prefix + " " + p)
}

func canonicalMethod(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if i := strings.IndexAny(s, " \t\r\n"); i >= 0 {
		s = s[:i]
	}
	switch s {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return s
	default:
		return ""
	}
}

func canonicalStatus(n int) int {
	if n < 100 || n > 599 {
		return 0
	}
	return n
}

func clipRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}
