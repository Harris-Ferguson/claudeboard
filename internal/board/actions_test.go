package board

import (
	"strings"
	"testing"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
)

func TestEnterFoldsWorkflow(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "enter") // cursor starts on payments-v2
	if len(m.rows) != 7 || !m.rows[0].Collapsed {
		t.Fatalf("after enter: %d rows", len(m.rows))
	}
	m, _ = press(t, m, "enter")
	if len(m.rows) != 9 {
		t.Errorf("after second enter: %d rows", len(m.rows))
	}
}

func TestEnterOpensSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 2
	_, cmd := press(t, m, "enter")
	if cmd == nil || len(src.attached) != 1 || src.attached[0] != "aaaa0002" {
		t.Errorf("cmd %v, attached %v", cmd, src.attached)
	}
}

func TestEnterOnInteractiveSessionExplains(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 8
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.message != msgInteractive || len(src.attached) != 0 {
		t.Errorf("cmd %v, message %q", cmd, m.message)
	}
}

func TestEnterResumesUnlistedSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live = append(m.live[:2:2], m.live[3:]...) // Claude no longer lists cb-q4-search-1
	m.rebuild()
	m.cursor = 4
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if len(src.resumed) != 1 || src.resumed[0] != "id-3 /work/api" {
		t.Fatalf("resumed %v", src.resumed)
	}
	if _, cmd = send(m, msg); cmd == nil || len(src.attached) != 1 || src.attached[0] != "res00001" {
		t.Errorf("after resuming: attached %v", src.attached)
	}
}

func TestStop(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 1
	m, cmd := press(t, m, "x")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	if len(src.stopped) != 1 || src.stopped[0] != "aaaa0001" || m.message != "stopped cb-payments-v2-1" {
		t.Errorf("stopped %v, message %q", src.stopped, m.message)
	}
	m.live[2].State = "stopped"
	m.rebuild()
	m.cursor = 4
	if m, cmd = press(t, m, "x"); cmd != nil || m.message != "cb-q4-search-1 is already stopped" {
		t.Errorf("stopping a stopped session: cmd %v, message %q", cmd, m.message)
	}
}

func TestResumeOnlyWhenStopped(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 1
	if m, _ = press(t, m, "r"); !strings.Contains(m.message, "already running") {
		t.Errorf("message %q", m.message)
	}
	m.live[2].State = "stopped"
	m.rebuild()
	m.cursor = 4
	m, cmd := press(t, m, "r")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	if len(src.resumed) != 1 || m.message != "resumed cb-q4-search-1" || len(src.attached) != 0 {
		t.Errorf("resumed %v, message %q, attached %v", src.resumed, m.message, src.attached)
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.rmOutput = "removed aaaa0003\n  worktree: “/work/api/.claude/worktrees/q4-search-1”"
	m.cursor = 4
	m, _ = press(t, m, "D")
	if m.mode != modeConfirmDelete || !strings.Contains(m.footer(100), "Delete cb-q4-search-1") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer(100))
	}
	m, cmd := press(t, m, "n")
	if cmd != nil || m.message != "cancelled" || len(src.removed) != 0 {
		t.Fatalf("n should cancel: %q", m.message)
	}
	m, cmd = press(t, m, "D", "y")
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	m, _ = send(m, cmd())
	if len(src.removed) != 1 || !strings.HasPrefix(m.message, "deleted cb-q4-search-1 · worktree:") {
		t.Errorf("removed %v, message %q", src.removed, m.message)
	}
	if st, _, _ := m.deps.Store.Load(); st.Session("id-3") != nil {
		t.Error("session still in state.json")
	}
}

func TestDeleteOtherBackgroundSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live = append(m.live, claudecli.Session{PID: 7, ShortID: "ffff0007", SessionID: "id-7", Name: "manual", Kind: claudecli.KindBackground, State: "done"})
	m.rebuild()
	m.cursor = len(m.rows) - 1
	m, cmd := press(t, m, "D", "y")
	if cmd == nil {
		t.Fatal("no command")
	}
	send(m, cmd())
	if len(src.removed) != 1 || src.removed[0] != "ffff0007" {
		t.Errorf("removed %v", src.removed)
	}
}

func TestArchiveToggles(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := press(t, m, "a")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	st, _, _ := m.deps.Store.Load()
	if !st.Workflow("payments-v2").Archived || !strings.HasPrefix(m.message, "archived payments-v2") {
		t.Errorf("archived %v, message %q", st.Workflow("payments-v2").Archived, m.message)
	}
}

func TestEnterResumesStoppedSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live[2].State, m.live[2].PID, m.live[2].Status = "stopped", 0, ""
	m.rebuild()
	m.cursor = 4
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if len(src.resumed) != 1 || src.resumed[0] != "id-3 /work/api" {
		t.Fatalf("resumed %v; a stopped session should be resumed, then opened", src.resumed)
	}
	if _, cmd = send(m, msg); cmd == nil || len(src.attached) != 1 {
		t.Errorf("not opened after resuming: attached %v", src.attached)
	}
}

func TestNoResumeBeforeClaudeHasListedSessions(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.lastOK, m.live = time.Time{}, nil // no listing yet, so every row looks stopped
	m.rebuild()
	m.cursor = 1
	for _, k := range []string{"enter", "r"} {
		m2, cmd := press(t, m, k)
		if cmd != nil || len(src.resumed) != 0 || !strings.Contains(m2.message, "waiting for") {
			t.Errorf("%s: cmd %v, resumed %v, message %q", k, cmd, src.resumed, m2.message)
		}
	}
}

func TestEnterOnAForgottenNeverPromptedSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live = m.live[:3:3] // Claude no longer lists cb-quick-1, which never got a prompt
	m.rebuild()
	m.cursor = 6
	m, cmd := press(t, m, "enter")
	if cmd != nil || len(src.resumed) != 0 || !strings.Contains(m.message, "D removes it") {
		t.Errorf("cmd %v, resumed %v, message %q", cmd, src.resumed, m.message)
	}
}

func TestResumingMessageClearsOnceOpened(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.message = "resuming cb-q4-search-1…"
	m, _ = send(m, resumedMsg{shortID: "aaaa0003"})
	if m.message != "" {
		t.Errorf("message %q", m.message)
	}
}
