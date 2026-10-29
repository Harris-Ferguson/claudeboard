package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
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

// stubCLI stands in for claudecli.CLI in `claudeboard new` tests.
type stubCLI struct {
	missing   error
	untrusted int // how many launches fail because the folder isn't trusted
	live      []claudecli.Session
	launched  []claudecli.LaunchOpts
	attached  []string
	trusted   []string
}

func (s *stubCLI) Available() error { return s.missing }

func (s *stubCLI) Launch(_ context.Context, o claudecli.LaunchOpts) (string, error) {
	if s.untrusted > 0 {
		s.untrusted--
		return "", errors.New("claude --bg: Workspace not trusted. Run `claude` in " + o.Dir + " once and accept the trust prompt, then retry.")
	}
	s.launched = append(s.launched, o)
	id := fmt.Sprintf("abcd%04d", len(s.launched))
	s.live = append(s.live, claudecli.Session{ShortID: id, SessionID: "sess-" + id, Name: o.Name, Kind: claudecli.KindBackground})
	return id, nil
}

func (s *stubCLI) List(context.Context) ([]claudecli.Session, error) { return s.live, nil }

func (s *stubCLI) AttachCmd(id string) *exec.Cmd {
	s.attached = append(s.attached, id)
	return exec.Command("true")
}

func (s *stubCLI) TrustCmd(dir string) *exec.Cmd {
	s.trusted = append(s.trusted, dir)
	return exec.Command("true")
}

func testEnv(t *testing.T, cli *stubCLI, terminal bool, answers string) newEnv {
	t.Helper()
	return newEnv{cli: cli, home: t.TempDir(), cwd: t.TempDir(), stdin: strings.NewReader(answers),
		terminal: terminal, now: func() time.Time { return time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) }}
}

func runSession(a newArgs, env newEnv) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = newSession(a, env, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestNewOpensSessionAndSaysHowToGetBack(t *testing.T) {
	cli := &stubCLI{}
	code, out, errOut := runSession(newArgs{}, testEnv(t, cli, true, ""))
	if code != 0 || len(cli.attached) != 1 || cli.attached[0] != "abcd0001" {
		t.Fatalf("code %d, attached %v, stderr %q", code, cli.attached, errOut)
	}
	for _, want := range []string{"opening cb-quick-1 (abcd0001)", "cb-quick-1 is still running", "claude attach abcd0001"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout is missing %q:\n%s", want, out)
		}
	}
}

func TestNewWithPromptStartsInBackground(t *testing.T) {
	cli := &stubCLI{}
	code, out, _ := runSession(newArgs{prompt: "fix it"}, testEnv(t, cli, false, ""))
	if code != 0 || len(cli.attached) != 0 || cli.launched[0].Prompt != "fix it" {
		t.Fatalf("code %d, attached %v, launched %+v", code, cli.attached, cli.launched)
	}
	if !strings.Contains(out, "started cb-quick-1 (abcd0001)") || !strings.Contains(out, "claude attach abcd0001") {
		t.Errorf("stdout:\n%s", out)
	}
}

func TestNewWithoutTerminalOrPromptRefusesBeforeLaunching(t *testing.T) {
	cli := &stubCLI{}
	code, _, errOut := runSession(newArgs{}, testEnv(t, cli, false, ""))
	if code != 1 || len(cli.launched) != 0 || !strings.Contains(errOut, "pass a prompt") {
		t.Errorf("code %d, launched %v, stderr %q", code, cli.launched, errOut)
	}
}

func TestNewWithoutClaude(t *testing.T) {
	cli := &stubCLI{missing: errors.New("Claude Code (claude) isn't on PATH in this terminal")}
	code, _, errOut := runSession(newArgs{}, testEnv(t, cli, true, ""))
	if code != 1 || len(cli.launched) != 0 || !strings.Contains(errOut, "isn't on PATH") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}

func TestNewOffersToTrustTheFolder(t *testing.T) {
	cli := &stubCLI{untrusted: 1}
	env := testEnv(t, cli, true, "\n") // Enter accepts the default (yes)
	code, out, errOut := runSession(newArgs{}, env)
	if code != 0 || len(cli.trusted) != 1 || cli.trusted[0] != env.cwd || len(cli.launched) != 1 {
		t.Fatalf("code %d, trusted %v, launched %v, stderr %q", code, cli.trusted, cli.launched, errOut)
	}
	if !strings.Contains(out, "isn't trusted") {
		t.Errorf("no question asked:\n%s", out)
	}
}

func TestNewTrustDeclinedExplainsWhatToRun(t *testing.T) {
	cli := &stubCLI{untrusted: 1}
	env := testEnv(t, cli, true, "n\n")
	code, _, errOut := runSession(newArgs{}, env)
	if code != 1 || len(cli.trusted) != 0 || !strings.Contains(errOut, "cd '"+env.cwd+"' && claude") {
		t.Errorf("code %d, trusted %v, stderr %q", code, cli.trusted, errOut)
	}
}

func TestNewUntrustedWithoutTerminalDoesNotAsk(t *testing.T) {
	cli := &stubCLI{untrusted: 1}
	env := testEnv(t, cli, false, "y\n")
	code, out, errOut := runSession(newArgs{prompt: "go"}, env)
	if code != 1 || len(cli.trusted) != 0 || strings.Contains(out, "?") || !strings.Contains(errOut, "&& claude") {
		t.Errorf("code %d, trusted %v, stdout %q, stderr %q", code, cli.trusted, out, errOut)
	}
}
