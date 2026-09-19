package ids

import (
	"strings"
	"testing"
)

func TestValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"user-1", true},
		{"sku_widget", true},
		{"", false},
		{"user 1", false},
		{"1;DROP TABLE users", false},
		{"../etc/passwd", false},
		{"user-1/../../x", false},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
	}
	for _, tc := range cases {
		if got := Valid(tc.in); got != tc.want {
			t.Fatalf("Valid(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}
