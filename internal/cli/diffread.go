package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/sumedhaerram/aquila/internal/diff"
)

func slurpDiff(stdin io.Reader, path string) ([]byte, error) {
	var raw []byte
	var err error
	if path != "" {
		raw, err = os.ReadFile(path)
	} else {
		raw, err = io.ReadAll(io.LimitReader(stdin, int64(diff.MaxBytes)+1))
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty diff")
	}
	if len(raw) > diff.MaxBytes {
		return nil, fmt.Errorf("diff too large")
	}
	return raw, nil
}

func diffPath(file string, rest []string) (string, error) {
	if file != "" && len(rest) != 0 {
		return "", fmt.Errorf("unexpected argument %q", rest[0])
	}
	if file != "" {
		return file, nil
	}
	if len(rest) == 1 {
		return rest[0], nil
	}
	if len(rest) > 1 {
		return "", fmt.Errorf("unexpected argument %q", rest[1])
	}
	return "", nil
}
