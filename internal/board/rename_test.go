package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// rename presses R on row i, types label over what's there, and presses enter, returning
// the board after the save and a refresh that reads the saved state (keeping the
// fixture's transcripts, which a real refresh would look for on disk).
func rename(t *testing.T, m Model, i int, label string) Model {
	t.Helper()
	m.cursor = i
	m, _ = press(t, m, "R")
	if m.mode != modeRename {
		t.Fatalf("R: mode %v, message %q", m.mode, m.message)
	}
	m.flow.input.SetValue(label)
	m, cmd := press(t, m, "enter")
	if cmd == nil || m.mode != modeList {
		t.Fatalf("enter: cmd %v, mode %v", cmd, m.mode)
	}
	m, refresh := send(m, cmd())
	if refresh == nil {
		t.Fatalf("no refresh after the save; message %q", m.message)
	}
	msg := refreshWith(m)
	msg.state, _, _ = m.deps.Store.Load()
	msg.readers = map[string]*transcript.Reader{}
	m, _ = send(m, msg)
	return m
}

func TestRenameShowsTheLabelInPlaceOfTheFirstPrompt(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2 // cb-payments-v2-2
	m, _ = press(t, m, "R")
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Rename cb-payments-v2-2") || m.flow.input.Value() != "hook PaymentForm into checkout" {
		t.Fatalf("input %q:\n%s", m.flow.input.Value(), out)
	}
	m, _ = press(t, m, "esc")
	m = rename(t, m, 2, "  checkout UI ")
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-2"); s.Label != "checkout UI" || s.Name != "cb-payments-v2-2" {
		t.Errorf("recorded %+v", s)
	}
	if m.message != "renamed cb-payments-v2-2 to checkout UI" {
		t.Errorf("message %q", m.message)
	}
	out = ansi.Strip(m.render())
	if !strings.Contains(out, "! web  checkout UI") {
		t.Errorf("row should show the label:\n%s", out)
	}
	if !strings.Contains(out, "checkout UI · cb-payments-v2-2 · web") {
		t.Errorf("preview title should show the label, then Claude's name for it:\n%s", out)
	}
	if r, _ := m.selected(); r.Name() != "checkout UI" {
		t.Errorf("Name() = %q; messages should use the label", r.Name())
	}
}

func TestRenameToNothingGoesBackToTheFirstPrompt(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m = rename(t, m, 2, "checkout UI")
	m.cursor = 2
	m, _ = press(t, m, "R")
	if m.flow.input.Value() != "checkout UI" {
		t.Errorf("input starts with %q, want the current label", m.flow.input.Value())
	}
	m, _ = press(t, m, "esc")
	m = rename(t, m, 2, "")
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-2"); s.Label != "" {
		t.Errorf("label %q", s.Label)
	}
	if !strings.Contains(m.message, "first prompt") {
		t.Errorf("message %q", m.message)
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "! web  hook PaymentForm") {
		t.Errorf("row:\n%s", out)
	}
}

func TestRenameKeepingTheFirstPromptStoresNoLabel(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m = rename(t, m, 2, "hook PaymentForm into checkout")
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-2"); s.Label != "" {
		t.Errorf("label %q; enter on the unchanged first prompt shouldn't pin it", s.Label)
	}
}

func TestRenameEscCancels(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2
	m, _ = press(t, m, "R")
	m.flow.input.SetValue("nope")
	m, cmd := press(t, m, "esc")
	if cmd != nil || m.mode != modeList || m.message != "cancelled" {
		t.Errorf("cmd %v, mode %v, message %q", cmd, m.mode, m.message)
	}
	state, _, _ := m.deps.Store.Load()
	if s := state.Session("id-2"); s.Label != "" {
		t.Errorf("label %q", s.Label)
	}
}

func TestRenameHintFitsANarrowPreview(t *testing.T) {
	m, _ := testModel(t, 90, 16) // a 51-column preview
	m.cursor = 2
	m, _ = press(t, m, "R")
	if out := ansi.Strip(m.render()); !strings.Contains(out, "esc cancel") {
		t.Errorf("the hint is cut off:\n%s", out)
	}
}

func TestRenameOnlyAppliesToWorkflowSessions(t *testing.T) {
	m, _ := withOthers(t)
	for key, want := range map[string]string{
		"w:payments-v2": "R renames a session",
		"o:id-other":    "R renames sessions in a workflow; m moves this one into one",
	} {
		m2 := cursorTo(t, m, key)
		m2, cmd := press(t, m2, "R")
		if cmd != nil || m2.mode != modeList || m2.message != want {
			t.Errorf("%s: cmd %v, mode %v, message %q", key, cmd, m2.mode, m2.message)
		}
	}
}

func TestRenameOnASubagentRenamesItsSession(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 30)
	m.cursor = 2 // a subagent of cb-payments-v2-1
	if r, _ := m.selected(); r.Kind != RowAgent {
		t.Fatalf("row 2 is %+v", r)
	}
	m, _ = press(t, m, "R")
	if m.mode != modeRename || m.pending.Session.Name != "cb-payments-v2-1" {
		t.Errorf("mode %v, renaming %q", m.mode, m.pending.Session.Name)
	}
}

func TestLongOrMultilineLabelsStayOnOneRow(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.state.Sessions[1].Label = "first line\nsecond line"
	m.rebuild()
	if out := ansi.Strip(m.render()); !strings.Contains(out, "! web  first line") || strings.Contains(out, "second line") {
		t.Errorf("only the label's first line should show:\n%s", out)
	}
	m = rename(t, m, 2, strings.Repeat("y", 80))
	for _, line := range strings.Split(ansi.Strip(m.render()), "\n") {
		if strings.Contains(line, "! web") && !strings.Contains(line, "y…") {
			t.Errorf("a long label should be cut short on the row: %q", line)
		}
	}
}
