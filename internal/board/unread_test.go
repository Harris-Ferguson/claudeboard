package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// unreadModel is the fixture with Claude's activity set per session (cb-payments-v2-1
// is working, cb-payments-v2-2 needs you, cb-q4-search-1 is done) and seen as given.
func unreadModel(t *testing.T, w int, seen map[string]int) (Model, *stubSource) {
	t.Helper()
	m, src := testModel(t, w, 16)
	for id, n := range map[string]int{"id-1": 5, "id-2": 5, "id-3": 3} {
		s := m.snaps[id]
		s.Activity = n
		m.snaps[id] = s
	}
	m.state.Seen = seen
	m.rebuild()
	return m, src
}

func refreshWith(m Model) refreshedMsg {
	return refreshedMsg{state: m.state, live: m.live, snaps: m.snaps, readers: m.readers, at: testNow}
}

func TestFinishedSessionWithNewActivityIsUnread(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 1, "id-2": 1, "id-3": 1})
	for i, want := range map[int]bool{1: false, 2: true, 4: true} { // working, needs you, done
		if m.rows[i].Unread != want {
			t.Errorf("row %d (%s) unread = %v, want %v", i, m.rows[i].Name(), m.rows[i].Unread, want)
		}
	}
	if m.rows[0].Stats.Unread != 1 || m.rows[3].Stats.Unread != 1 {
		t.Errorf("workflow unread counts = %d, %d", m.rows[0].Stats.Unread, m.rows[3].Stats.Unread)
	}
	out := m.render()
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "api • plan the search rewrite") || !strings.Contains(plain, "q4-search  •") {
		t.Errorf("unread marker missing:\n%s", plain)
	}
	if !strings.Contains(out, styleBold.Render("plan the search rewrite")) {
		t.Error("an unread session's label should be bold")
	}
}

func TestCaughtUpSessionIsNotUnread(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 5, "id-3": 3})
	for _, r := range m.rows {
		if r.Unread || r.Stats.Unread != 0 {
			t.Errorf("%s shows unread", r.Name())
		}
	}
}

func TestSelectingASessionWithThePreviewMarksItSeen(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	m.cursor = 3
	m, cmd := press(t, m, "down") // onto cb-q4-search-1
	if m.rows[4].Unread || m.state.Seen["id-3"] != 3 {
		t.Fatalf("unread %v, seen %d; selecting it with the preview showing should count", m.rows[4].Unread, m.state.Seen["id-3"])
	}
	if cmd == nil {
		t.Fatal("no command to save it")
	}
	cmd()
	if st, _, _ := m.deps.Store.Load(); st.Seen["id-3"] != 3 {
		t.Errorf("saved seen = %v", st.Seen)
	}
	// A refresh that read state.json before the save landed doesn't make it unread again.
	stale := refreshWith(m)
	stale.state.Seen = map[string]int{"id-1": 5, "id-2": 5, "id-3": 1}
	m.cursor = 1
	m, _ = send(m, stale)
	if m.rows[4].Unread {
		t.Error("it went back to unread after a refresh")
	}
}

func TestNarrowScreenSelectionDoesNotMarkSeen(t *testing.T) {
	m, _ := unreadModel(t, 70, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	m.cursor = 3
	m, _ = press(t, m, "down")
	if !m.rows[4].Unread {
		t.Error("with the preview hidden, selecting a session isn't seeing it")
	}
}

func TestComingBackFromASessionMarksItSeen(t *testing.T) {
	m, _ := unreadModel(t, 70, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	m, _ = send(m, attachDoneMsg{shortID: "aaaa0003"})
	r := refreshWith(m)
	s := r.snaps["id-3"]
	s.Activity = 4 // what happened while it was open
	r.snaps = map[string]transcript.Snapshot{"id-1": r.snaps["id-1"], "id-2": r.snaps["id-2"], "id-3": s, "id-4": r.snaps["id-4"]}
	m, cmd := send(m, r)
	if m.rows[4].Unread || m.state.Seen["id-3"] != 4 || cmd == nil {
		t.Errorf("unread %v, seen %d, cmd %v", m.rows[4].Unread, m.state.Seen["id-3"], cmd)
	}
}

func TestSessionsFirstSeenStartOutRead(t *testing.T) {
	m, _ := unreadModel(t, 100, nil)
	m, cmd := send(m, refreshWith(m))
	if m.rows[4].Unread || m.state.Seen["id-3"] != 3 || m.state.Seen["id-2"] != 5 {
		t.Errorf("unread %v, seen %v", m.rows[4].Unread, m.state.Seen)
	}
	if cmd == nil {
		t.Fatal("no command to save the starting point")
	}
	cmd()
	if st, _, _ := m.deps.Store.Load(); st.Seen["id-3"] != 3 {
		t.Errorf("saved seen = %v", st.Seen)
	}
}
