package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseNewArgs(t *testing.T) {
	cases := []struct {
		args []string
		want newArgs
	}{
		{nil, newArgs{}},
		{[]string{"-w", "payments", "-r", "api", "--worktree", "fix", "the", "bug"},
			newArgs{workflow: "payments", repo: "api", worktree: true, prompt: "fix the bug"}},
		{[]string{"fix", "-w", "x"}, newArgs{prompt: "fix -w x"}}, // flags after the prompt are prompt text
	}
	for _, c := range cases {
		got, err := parseNewArgs(c.args)
		if err != nil || got != c.want {
			t.Errorf("parseNewArgs(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	if _, err := parseNewArgs([]string{"-x"}); err == nil {
		t.Error("unknown flag should fail")
	}
}

func TestNewHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"new", "-h"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "usage: claudeboard new") {
		t.Errorf("code %d, stdout %q", code, out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"bogus"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: claudeboard") {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
}
