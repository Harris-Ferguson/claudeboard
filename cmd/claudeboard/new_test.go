package main

import (
	"bytes"
	"errors"
	"flag"
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
		{[]string{"fix login", "-r", "api"}, newArgs{repo: "api", prompt: "fix login"}}, // flags may follow the prompt
		{[]string{"-w", "bugs", "fix", "--worktree", "it"}, newArgs{workflow: "bugs", worktree: true, prompt: "fix it"}},
		{[]string{"--", "-w", "is", "a", "flag"}, newArgs{prompt: "-w is a flag"}}, // after --, everything is prompt
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
	if _, err := parseNewArgs([]string{"-r"}); err == nil {
		t.Error("-r without a value should fail")
	}
	if _, err := parseNewArgs([]string{"help"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("a lone \"help\" should ask for help, not become a prompt: %v", err)
	}
}

func TestHelpNew(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"help", "new"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "usage: claudeboard new") {
		t.Errorf("code %d, stdout %q", code, out.String())
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
