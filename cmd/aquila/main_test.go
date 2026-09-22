package main

import (
	"io"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	if err := run([]string{"version"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "aquila ") {
		t.Fatalf("%q", out.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	t.Parallel()
	err := run([]string{"ask"}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("got %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	if err := run([]string{"help"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "observe") || !strings.Contains(out.String(), "impact") || !strings.Contains(out.String(), "env") || !strings.Contains(out.String(), "replay") {
		t.Fatalf("%q", out.String())
	}
}

func TestRunNoArgs(t *testing.T) {
	t.Parallel()
	if err := run(nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}
