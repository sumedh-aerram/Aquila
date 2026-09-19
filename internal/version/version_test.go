package version

import "testing"

func TestVersionDefault(t *testing.T) {
	t.Parallel()
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}
