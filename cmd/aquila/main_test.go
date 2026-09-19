package main

import (
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	if err := run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	err := run([]string{"ask"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("got %v", err)
	}
}

func TestRunHelp(t *testing.T) {
	if err := run([]string{"help"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunNoArgs(t *testing.T) {
	if err := run(nil); err != nil {
		t.Fatal(err)
	}
}
