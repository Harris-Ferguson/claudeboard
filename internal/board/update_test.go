package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = send(m, key(k))
	}
	return m, cmd
}

func TestDoRefreshAdoptsSessionsAndReadsTranscripts(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-work-api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"fix the bug"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := &stubSource{live: []claudecli.Session{
		{PID: 1, ShortID: "aaaa0001", SessionID: "sess-1", Name: "cb-bugs-1", Kind: claudecli.KindBackground, Cwd: "/work/api", State: "working"},
		{PID: 2, SessionID: "sess-other", Name: "cb-looks-like-ours-1", Kind: claudecli.KindInteractive, Status: "idle"},
	}}
	d := Deps{Src: src, Store: store.Open(t.TempDir()), ProjectsDir: projects, Now: func() time.Time { return testNow }}
	msg := doRefresh(d, nil)
	if msg.listErr != nil || msg.storeErr != nil {
		t.Fatalf("errors: %v, %v", msg.listErr, msg.storeErr)
	}
	if s := msg.state.Session("sess-1"); s == nil || s.Workflow != "bugs" || s.Dir != "/work/api" || msg.state.Workflow("bugs") == nil {
		t.Errorf("cb-bugs-1 not adopted: %+v", msg.state)
	}
	if msg.state.Session("sess-other") != nil {
		t.Error("interactive sessions must not be adopted")
	}
	if s := msg.snaps["sess-1"]; !s.Found || s.FirstPrompt != "fix the bug" {
		t.Errorf("snapshot = %+v", s)
	}
	if msg.readers["sess-1"] == nil {
		t.Error("reader not kept for the next refresh")
	}
}

func TestRefreshKeepsLastStatusWhenListFails(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.listErr = errors.New("claude agents --json: boom")
	m, _ = send(m, doRefresh(m.deps, nil))
	if m.listErr == nil || len(m.live) != 5 || !m.lastOK.Equal(testNow) {
		t.Errorf("listErr %v, %d live, lastOK %v", m.listErr, len(m.live), m.lastOK)
	}
}

func TestTickStartsOneRefreshAtATime(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := send(m, tickMsg(testNow))
	if !m.refreshing || m.readers != nil || cmd == nil {
		t.Fatalf("after first tick: refreshing %v, readers %v", m.refreshing, m.readers)
	}
	if m.startRefresh() != nil {
		t.Error("a second refresh started while one was running")
	}
}

func TestRebuildKeepsSelection(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 4 // cb-q4-search-1
	m.collapsed["payments-v2"] = true
	m.rebuild()
	if r, _ := m.selected(); r.Session.Name != "cb-q4-search-1" {
		t.Errorf("selection moved to %+v", r)
	}
}

func TestSelectionClampsWhenSessionDisappears(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 8 // arbybot-8d, the only Other session
	m.live = m.live[:4]
	m.rebuild()
	if m.cursor != len(m.rows)-1 || m.rows[m.cursor].Kind == RowOtherHeader {
		t.Errorf("cursor %d of %d rows", m.cursor, len(m.rows))
	}
}

func TestMoveSkipsOtherHeader(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 6
	m, _ = press(t, m, "down")
	if m.cursor != 8 {
		t.Errorf("down from 6 went to %d, want 8", m.cursor)
	}
	m, _ = press(t, m, "k")
	if m.cursor != 6 {
		t.Errorf("up from 8 went to %d, want 6", m.cursor)
	}
}

func TestTabJumpsToWaitingSession(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "tab")
	if m.cursor != 2 {
		t.Errorf("tab went to %d, want 2", m.cursor)
	}
	m.live[1].State, m.live[1].WaitingFor = "working", ""
	m.rebuild()
	m, _ = press(t, m, "tab")
	if m.cursor != 2 || m.message != "nothing needs you right now" {
		t.Errorf("cursor %d, message %q", m.cursor, m.message)
	}
}

func TestQuitHelpAndPreviewKeys(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	if _, cmd := press(t, m, "q"); cmd == nil {
		t.Fatal("q returned no command")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
	m, _ = press(t, m, "?")
	if m.mode != modeHelp {
		t.Fatal("? should open help")
	}
	m, _ = press(t, m, "x")
	if m.mode != modeList {
		t.Error("any key should close help")
	}
	m, _ = press(t, m, "p")
	if m.previewVisible() {
		t.Error("p should hide the preview on a wide screen")
	}
	m, _ = press(t, m, "pgup")
	if m.previewScroll == 0 {
		t.Error("pgup should scroll the preview back")
	}
	m, _ = press(t, m, "A")
	if !m.showArchived {
		t.Error("A should show archived workflows")
	}
}

func TestWindowSize(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 30})
	if m.width != 80 || m.height != 30 {
		t.Errorf("size = %dx%d", m.width, m.height)
	}
}
