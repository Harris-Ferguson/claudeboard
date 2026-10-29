package board

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/config"
)

func labels(cs []choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.label
	}
	return out
}

func TestNewSessionInBusyRepoOffersWorktreeThenOpens(t *testing.T) {
	m, src := testModel(t, 100, 16) // cursor on payments-v2
	m, _ = press(t, m, "n")
	if m.mode != modePickWorkflow || strings.Join(labels(m.flow.choices), ",") != "payments-v2,q4-search,quick,+ new workflow…" || m.flow.cursor != 0 {
		t.Fatalf("workflow picker: mode %v, %q, cursor %d", m.mode, labels(m.flow.choices), m.flow.cursor)
	}
	m, _ = press(t, m, "enter")
	if m.mode != modePickRepo || m.flow.cursor != 0 || m.flow.choices[0].value != "/work/api" {
		t.Fatalf("repo picker: mode %v, %q, cursor %d (want api, the workflow's last repo)", m.mode, labels(m.flow.choices), m.flow.cursor)
	}
	m, _ = press(t, m, "enter") // cb-payments-v2-1 is working in /work/api
	if m.mode != modeAskWorktree || !strings.Contains(m.footer(100), "api already has a session running") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer(100))
	}
	m, cmd := press(t, m, "y")
	if cmd == nil || m.mode != modeList {
		t.Fatalf("y: cmd %v, mode %v", cmd, m.mode)
	}
	msg := cmd()
	if len(src.launched) != 1 {
		t.Fatalf("launched %v", src.launched)
	}
	if o := src.launched[0]; o.Name != "cb-payments-v2-3" || o.Worktree != "payments-v2-3" || o.Dir != "/work/api" {
		t.Errorf("launch options = %+v", o)
	}
	m, cmd = send(m, msg)
	if cmd == nil || len(src.attached) != 1 || src.attached[0] != "new00001" || m.message != "started cb-payments-v2-3" {
		t.Errorf("after launch: attached %v, message %q", src.attached, m.message)
	}
}

func TestNewWorkflowByName(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m, _ = press(t, m, "n", "down", "down", "down", "enter")
	if m.mode != modeNameWorkflow {
		t.Fatalf("mode %v, want the name prompt", m.mode)
	}
	m.flow.input.SetValue("!!!")
	m, _ = press(t, m, "enter")
	if m.mode != modeNameWorkflow || !strings.Contains(m.message, "letter or digit") {
		t.Fatalf("bad name accepted: mode %v, message %q", m.mode, m.message)
	}
	m.flow.input.SetValue("Bug Bash")
	m, _ = press(t, m, "enter")
	if m.mode != modePickRepo || m.flow.req.Workflow != "Bug Bash" {
		t.Fatalf("mode %v, req %+v", m.mode, m.flow.req)
	}
	if got := m.flow.choices[m.flow.cursor].value; got != "/work" {
		t.Errorf("default folder for a new workflow = %q, want the current folder", got)
	}
	m, cmd := press(t, m, "down", "enter") // scratch: cb-quick-1 runs there, but scratch never asks about worktrees
	if cmd == nil {
		t.Fatal("no launch command")
	}
	cmd()
	if o := src.launched[0]; o.Name != "cb-bug-bash-1" || o.Dir != "/home/.claudeboard/scratch" || o.Worktree != "" {
		t.Errorf("launch options = %+v", o)
	}
}

func TestQuickOneOff(t *testing.T) {
	m, src := testModel(t, 100, 16)
	_, cmd := press(t, m, "s")
	if cmd == nil {
		t.Fatal("no command")
	}
	cmd()
	if o := src.launched[0]; o.Name != "cb-quick-2" || o.Dir != "/home/.claudeboard/scratch" {
		t.Errorf("launch options = %+v", o)
	}
}

func TestUntrustedFolderOffersToOpenClaude(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.launchErr = errors.New("claude --bg: Workspace not trusted. Run `claude` in /home/.claudeboard/scratch once and accept the trust prompt, then retry.")
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())
	if m.mode != modeAskTrust || !strings.Contains(m.footer(100), "not trusted by Claude yet") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer(100))
	}
	m2, cmd := press(t, m, "n")
	if cmd != nil || m2.message != "cancelled" {
		t.Errorf("n: cmd %v, message %q", cmd, m2.message)
	}
	m, cmd = press(t, m, "y")
	if cmd == nil || m.mode != modeList || len(src.trusted) != 1 || src.trusted[0] != "/home/.claudeboard/scratch" {
		t.Fatalf("y: cmd %v, trusted %v", cmd, src.trusted)
	}
	src.launchErr = nil
	m, cmd = send(m, trustDoneMsg{req: m.flow.req})
	if cmd == nil {
		t.Fatal("no retry after trusting")
	}
	cmd()
	if len(src.launched) != 1 {
		t.Errorf("retry didn't launch: %v", src.launched)
	}
}

func TestOtherLaunchErrorsAreShown(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.launchErr = errors.New("claude --bg: something broke")
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())
	if m.mode != modeList || !strings.Contains(m.message, "something broke") {
		t.Errorf("mode %v, message %q", m.mode, m.message)
	}
}

func TestEscCancelsNewSession(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "n", "esc")
	if m.mode != modeList || m.message != "cancelled" {
		t.Errorf("mode %v, message %q", m.mode, m.message)
	}
	m, _ = press(t, m, "n", "enter", "esc")
	if m.mode != modeList {
		t.Errorf("esc in the repo picker: mode %v", m.mode)
	}
}

func TestWorkflowNameBoxShowsItsPlaceholder(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "n", "down", "down", "down", "enter")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "e.g. payments-v2") {
		t.Errorf("placeholder not shown:\n%s", out)
	}
}

func TestRepoPickerPointsToConfigWhenNoReposAreSet(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.deps.Cfg = config.Config{ScratchDir: "/home/.claudeboard/scratch", Path: "/home/.claudeboard/config.toml"}
	m, _ = press(t, m, "n", "enter")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "config.toml") {
		t.Errorf("no pointer to config.toml:\n%s", out)
	}
}

func TestPreviewKeyExplainsWhenTheScreenIsTooNarrow(t *testing.T) {
	m, _ := testModel(t, 50, 16)
	m, _ = press(t, m, "p")
	if !strings.Contains(m.message, "too narrow") {
		t.Errorf("message %q", m.message)
	}
}

func TestTrustDeclinedInClaudeDoesNotAskAgain(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.launchErr = errors.New("claude --bg: Workspace not trusted. Run `claude` in /home/.claudeboard/scratch once")
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())
	m, _ = press(t, m, "y")                         // opens claude; the user presses Esc there
	m, cmd = send(m, trustDoneMsg{req: m.flow.req}) // still untrusted, so the retry fails
	m, _ = send(m, cmd())
	if m.mode != modeList || !strings.Contains(m.message, "still isn't trusted") {
		t.Errorf("mode %v, message %q", m.mode, m.message)
	}
}

func TestCursorMovesToTheNewSession(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())                  // launched; the board opens it
	m, _ = send(m, doRefresh(m.deps, nil)) // back from the session, the next refresh lists it
	if r, _ := m.selected(); r.Session.Name != "cb-quick-2" {
		t.Errorf("cursor on %q, want the new session cb-quick-2", r.Name())
	}
}
