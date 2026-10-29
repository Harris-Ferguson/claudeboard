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

func TestMoveOnlyAppliesToOtherSessions(t *testing.T) {
	m, src := withOthers(t)
	m.cursor = 2 // cb-payments-v2-2
	m, cmd := press(t, m, "m")
	if cmd != nil || m.mode != modeList || !strings.Contains(m.message, "Other sessions") || len(src.launched) != 0 {
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
	if out := ansi.Strip(m.render()); !strings.Contains(out, "move an Other session into a") {
		t.Errorf("help doesn't list m:\n%s", out)
	}
}
