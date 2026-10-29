package claudecli

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseAgents(t *testing.T) {
	data, err := os.ReadFile("testdata/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAgents(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d sessions, want 5", len(got))
	}
	want := Session{PID: 102, ShortID: "bbbb2222", Cwd: "/work/web", Kind: KindBackground,
		SessionID: "bbbb2222-0000-4000-8000-000000000002", Name: "cb-payments-v2-2",
		Status: "waiting", State: "blocked", WaitingFor: "permission prompt"}
	if got[1] != want {
		t.Errorf("blocked entry = %+v", got[1])
	}
	if got[2].State != "stopped" || got[2].PID != 0 {
		t.Errorf("stopped entry = %+v", got[2])
	}
	if got[4].Kind != KindInteractive || got[4].ShortID != "" || got[4].State != "" {
		t.Errorf("interactive entry = %+v", got[4])
	}
}

func TestParseAgentsEdgeCases(t *testing.T) {
	for _, in := range []string{"", "null", "[]", "  \n"} {
		if got, err := ParseAgents([]byte(in)); err != nil || len(got) != 0 {
			t.Errorf("ParseAgents(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseAgents([]byte("not json")); err == nil {
		t.Error("ParseAgents(garbage) should fail")
	}
}

func TestParseLaunch(t *testing.T) {
	cases := []struct{ out, id, name string }{
		{"Starting background service…\nbackgrounded · 301e18a1 · cb-spike-1\n  claude agents   list sessions\n", "301e18a1", "cb-spike-1"},
		{"backgrounded · bbbc96fd · cb-probe-1 (idle — send a prompt to start)\n", "bbbc96fd", "cb-probe-1"},
		{"note: woke session eaf7dcf2 with its saved options (--name, --model).\nbackgrounded · eaf7dcf2 · cb-verify-1 (idle — send a prompt to start)", "eaf7dcf2", "cb-verify-1"},
		{"note: session eaf7dcf2 is already running in the background, so this started a copy as e56cb95f.\nbackgrounded · e56cb95f (idle — send a prompt to start)", "e56cb95f", ""},
	}
	for _, c := range cases {
		id, name, err := ParseLaunch(c.out)
		if err != nil || id != c.id || name != c.name {
			t.Errorf("ParseLaunch(%q) = %q, %q, %v; want %q, %q", c.out, id, name, err, c.id, c.name)
		}
	}
	if _, _, err := ParseLaunch("Workspace not trusted. Run `claude` in /x once."); err == nil {
		t.Error("ParseLaunch should fail without a backgrounded line")
	}
}

func TestLaunchArgs(t *testing.T) {
	got := LaunchArgs(LaunchOpts{Name: "cb-w-1"})
	if want := []string{"--bg", "--name", "cb-w-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("minimal = %q", got)
	}
	got = LaunchArgs(LaunchOpts{Name: "cb-w-2", Worktree: "w-2", Model: "opus", PermissionMode: "plan", IDE: true, Prompt: "fix it"})
	want := []string{"--bg", "--name", "cb-w-2", "--worktree", "w-2", "--model", "opus", "--permission-mode", "plan", "--ide", "fix it"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full = %q", got)
	}
	got = LaunchArgs(LaunchOpts{Name: "cb-w-4", Resume: "5d02d962-f2cc"})
	if want := []string{"--bg", "--name", "cb-w-4", "--resume", "5d02d962-f2cc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("resume = %q", got)
	}
	got = LaunchArgs(LaunchOpts{Name: "cb-w-3", Prompt: "-rf is scary"})
	if got[len(got)-1] != " -rf is scary" {
		t.Errorf("a prompt starting with - must not look like a flag: %q", got)
	}
}

func TestCleanEnv(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_PID=9",
		"CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=x", "CLAUDE_CODE_SESSION_ID=abc"}
	got := CleanEnv(env)
	want := []string{"PATH=/bin", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CleanEnv = %q, want %q", got, want)
	}
	if len(MarkerVars) != 10 || !slices.Contains(MarkerVars, "CLAUDE_CODE_MESSAGING_TOKEN") {
		t.Errorf("MarkerVars = %q", MarkerVars)
	}
}

func TestVersions(t *testing.T) {
	if v := ParseVersion("2.1.312 (Claude Code)\n"); v != "2.1.312" {
		t.Errorf("ParseVersion = %q", v)
	}
	cases := []struct {
		a, b string
		want int
	}{{"2.1.312", "2.1.312", 0}, {"2.1.311", "2.1.312", -1}, {"2.2.0", "2.1.312", 1}, {"2.1", "2.1.0", 0}, {"3", "2.9.9", 1}}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsUntrusted(t *testing.T) {
	if !IsUntrusted(errors.New("claude --bg: Workspace not trusted. Run `claude` in /x once")) {
		t.Error("should detect the untrusted message")
	}
	if IsUntrusted(nil) || IsUntrusted(errors.New("boom")) {
		t.Error("false positive")
	}
}

func TestAvailable(t *testing.T) {
	if err := (CLI{Bin: "sh"}).Available(); err != nil {
		t.Errorf("sh should be found: %v", err)
	}
	err := CLI{Bin: "claude-definitely-not-installed"}.Available()
	if err == nil || !strings.Contains(err.Error(), "isn't on PATH") {
		t.Errorf("err = %v", err)
	}
}
