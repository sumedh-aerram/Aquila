package main

import (
	"io"
	"strings"
	"testing"
	"time"
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
	got := out.String()
	for _, want := range []string{"observe", "impact", "env", "replay", "fault", "plan", "experiment", "report"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "aquila ask") {
		t.Fatal("must not list ask as a working command")
	}
}

func TestRunNoArgs(t *testing.T) {
	t.Parallel()
	if err := run(nil, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestCommandTimeoutFaultIsUnlimited(t *testing.T) {
	t.Parallel()
	if commandTimeout([]string{"fault", "-target", "http://127.0.0.1:18180"}) != 0 {
		t.Fatal("fault must not have a process timeout")
	}
}

func TestCommandTimeoutReplayScalesWithN(t *testing.T) {
	t.Parallel()
	if got := commandTimeout([]string{"replay"}); got != 45*time.Second {
		t.Fatalf("default n=1 timeout=%s", got)
	}
	if got := commandTimeout([]string{"replay", "-n", "2"}); got != 90*time.Second {
		t.Fatalf("n=2 timeout=%s", got)
	}
	if got := commandTimeout([]string{"replay", "-n=100"}); got != 8*time.Minute {
		t.Fatalf("n=100 must cap, got %s", got)
	}
}

func TestCommandTimeoutExperimentUsesPlanDefaultN(t *testing.T) {
	t.Parallel()
	if got := commandTimeout([]string{"experiment"}); got != 8*time.Minute {
		t.Fatalf("default n=20 must cap, got %s", got)
	}
	if got := commandTimeout([]string{"experiment", "-n", "2"}); got != 90*time.Second {
		t.Fatalf("n=2 timeout=%s", got)
	}
}
