package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "claudeboard ") {
		t.Fatalf("got %q, want it to start with %q", out.String(), "claudeboard ")
	}
}
