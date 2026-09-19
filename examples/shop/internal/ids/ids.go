package ids

import "regexp"

var valid = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Valid reports whether s is a safe identifier for path parameters and foreign keys.
func Valid(s string) bool {
	return valid.MatchString(s)
}
