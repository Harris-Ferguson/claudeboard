package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// withOthers is the fixture with a conversation and folder for the interactive Other
// session, plus a stopped background session claudeboard didn't start.
func withOthers(t *testing.T) (Model, *stubSource) {
	t.Helper()
	m, src := testModel(t, 100, 16)
	bg := claudecli.Session{ShortID: "bbbb0001", SessionID: "id-bg", Kind: claudecli.KindBackground, State: "stopped", Cwd: "/work/api"}
	for _, live := range []*[]claudecli.Session{&m.live, &src.live} {
		(*live)[4].Cwd = "/work/arbybot"
		*live = append(*live, bg)
	}
	m.snaps["id-other"] = transcript.Snapshot{Found: true, FirstPrompt: "tune the bot", Prompts: 1}
	m.rebuild()
	return m, src
}

func cursorTo(t *testing.T, m Model, key string) Model {
	t.Helper()
	for i, r := range m.rows {
		if r.Key() == key {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("no row %s", key)
	return m
}

func TestMoveInteractiveSessionContinuesItInAWorkflow(t *testing.T) {
	m, src := withOthers(t)
	m = cursorTo(t, m, "o:id-other")
	m, _ = press(t, m, "m")
	if out := ansi.Strip(m.render()); m.mode != modePickWorkflow || !strings.Contains(out, "Move arbybot-8d into") {
		t.Fatalf("mode %v:\n%s", m.mode, out)
	}
	m, cmd := press(t, m, "enter") // payments-v2
	if cmd == nil || m.mode != modeList {
		t.Fatalf("enter: cmd %v, mode %v", cmd, m.mode)
	}
	msg := cmd()
	if len(src.launched) != 1 {
		t.Fatalf("launched %+v", src.launched)
	}
	if o := src.launched[0]; o.Resume != "id-other" || o.Name != "cb-payments-v2-3" || o.Dir != "/work/arbybot" || o.Worktree != "" {
		t.Errorf("launch options = %+v", o)
	}
	m, _ = send(m, msg)
	if len(src.attached) != 1 || src.attached[0] != "new00001" {
		t.Errorf("attached %v, want the copy opened", src.attached)
	}
	if !strings.Contains(m.message, "cb-payments-v2-3") || !strings.Contains(m.message, "arbybot-8d is still open in its terminal") {
		t.Errorf("message %q", m.message)
	}
}

func TestMoveBackgroundSessionAddsItAsItIs(t *testing.T) {
	m, src := withOthers(t)
	m = cursorTo(t, m, "o:id-bg")
	m, _ = press(t, m, "m", "down", "down", "down", "enter") // + new workflow…
	if m.mode != modeNameWorkflow {
		t.Fatalf("mode %v, want the name prompt", m.mode)
	}
	m.flow.input.SetValue("Bots")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, refresh := send(m, cmd())
	if len(src.launched)+len(src.resumed) != 0 {
		t.Errorf("a background session should be added as it is, not restarted: launched %v, resumed %v", src.launched, src.resumed)
	}
	if m.message != "moved bbbb0001 into Bots" {
		t.Errorf("message %q", m.message)
	}
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-bg"); s == nil || s.Workflow != "bots" || s.Repo != "api" || s.ShortID != "bbbb0001" {
		t.Fatalf("recorded %+v", s)
	}
	m, _ = send(m, refresh())
	if r, _ := m.selected(); r.Kind != RowSession || r.Session.SessionID != "id-bg" {
		t.Errorf("cursor on %+v, want the moved session", r)
	}
}

func TestMoveSessionIntoAnotherWorkflow(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.collapsed["q4-search"] = true
	m.rebuild()
	m.cursor = 2 // cb-payments-v2-2
	m, _ = press(t, m, "m")
	var labels []string
	for _, c := range m.flow.choices {
		labels = append(labels, c.label)
	}
	if got := strings.Join(labels, ", "); m.mode != modePickWorkflow || got != "q4-search, quick, + new workflow…" {
		t.Fatalf("mode %v, choices %q; its own workflow shouldn't be offered", m.mode, got)
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "Move cb-payments-v2-2 into") {
		t.Errorf("picker title:\n%s", out)
	}
	m, cmd := press(t, m, "enter") // q4-search
	if cmd == nil || m.mode != modeList {
		t.Fatalf("enter: cmd %v, mode %v", cmd, m.mode)
	}
	m, refresh := send(m, cmd())
	if m.message != "moved cb-payments-v2-2 into q4-search" {
		t.Errorf("message %q", m.message)
	}
	if len(src.launched)+len(src.resumed)+len(src.stopped)+len(src.removed) != 0 {
		t.Errorf("moving should only change claudeboard's records: %+v", src)
	}
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-2"); s == nil || s.Workflow != "q4-search" || s.Name != "cb-payments-v2-2" || s.Repo != "web" {
		t.Fatalf("recorded %+v", s)
	}
	m, _ = send(m, refresh())
	if r, _ := m.selected(); r.Kind != RowSession || r.Session.SessionID != "id-2" || r.Workflow.Slug != "q4-search" {
		t.Errorf("cursor on %+v, want the moved session under q4-search (unfolded)", r)
	}
}

func TestMoveSessionIntoANewWorkflow(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 1 // cb-payments-v2-1
	m, _ = press(t, m, "m", "down", "down", "enter")
	if m.mode != modeNameWorkflow {
		t.Fatalf("mode %v, want the name prompt", m.mode)
	}
	m.flow.input.SetValue("Hotfixes")
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	state, _, _ := m.deps.Store.Load()
	if wf := state.Workflow("hotfixes"); wf == nil || wf.Name != "Hotfixes" {
		t.Errorf("workflows %+v", state.Workflows)
	}
	if s := state.Session("id-1"); s.Workflow != "hotfixes" {
		t.Errorf("session in %q", s.Workflow)
	}
}

func TestMoveIntoItsOwnWorkflowByNameIsRefused(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 1
	m, _ = press(t, m, "m", "down", "down", "enter")
	m.flow.input.SetValue("Payments V2")
	m, cmd := press(t, m, "enter")
	m, _ = send(m, cmd())
	if !strings.Contains(m.message, "already in payments-v2") {
		t.Errorf("message %q", m.message)
	}
}

func TestMoveOnAWorkflowRowSaysWhatItDoes(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 0
	m, cmd := press(t, m, "m")
	if cmd != nil || m.mode != modeList || m.message != "m moves a session into another workflow" || len(src.launched) != 0 {
		t.Errorf("cmd %v, mode %v, message %q", cmd, m.mode, m.message)
	}
}

func TestMoveInteractiveSessionWithNoConversation(t *testing.T) {
	m, src := withOthers(t)
	delete(m.snaps, "id-other")
	m.rebuild()
	m = cursorTo(t, m, "o:id-other")
	m, cmd := press(t, m, "m")
	if cmd != nil || m.mode != modeList || !strings.Contains(m.message, "nothing to continue") || len(src.launched) != 0 {
		t.Errorf("cmd %v, mode %v, message %q", cmd, m.mode, m.message)
	}
}

func TestOtherSessionsSayHowToMoveThem(t *testing.T) {
	m, _ := withOthers(t)
	m = cursorTo(t, m, "o:id-other")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "m moves it into a workflow") {
		t.Errorf("preview doesn't mention m:\n%s", out)
	}
	m.mode = modeHelp
	if out := ansi.Strip(m.render()); !strings.Contains(out, "move to a workflow / rename session") {
		t.Errorf("help doesn't list m:\n%s", out)
	}
}
