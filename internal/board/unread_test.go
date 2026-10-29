package board

import (
	"strings"
	"testing"
	"time"

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

// clock starts the board's time at testNow and returns a function that moves it on.
func clock(m *Model) func(time.Duration) {
	now := testNow
	m.deps.Now = func() time.Time { return now }
	return func(d time.Duration) { now = now.Add(d) }
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

func TestRestingOnASessionWithThePreviewMarksItSeen(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	wait := clock(&m)
	m.cursor = 3
	m, cmd := press(t, m, "down") // onto cb-q4-search-1
	if !m.rows[4].Unread || cmd != nil {
		t.Fatalf("unread %v, cmd %v; selecting it isn't looking at it yet", m.rows[4].Unread, cmd)
	}
	wait(900 * time.Millisecond)
	m, _ = send(m, refreshWith(m))
	if !m.rows[4].Unread {
		t.Fatal("marked seen before the cursor had rested on it for a second")
	}
	wait(time.Second)
	m, cmd = send(m, refreshWith(m))
	if m.rows[4].Unread || m.state.Seen["id-3"] != 3 {
		t.Fatalf("unread %v, seen %d; resting on it with the preview showing should count", m.rows[4].Unread, m.state.Seen["id-3"])
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

func TestMovingPastAnUnreadSessionLeavesItUnread(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 1, "id-3": 1})
	wait := clock(&m)
	m.cursor = 1
	for range 3 { // past cb-payments-v2-2 and q4-search, onto cb-q4-search-1
		m, _ = press(t, m, "down")
		wait(500 * time.Millisecond)
		m, _ = send(m, refreshWith(m))
	}
	if !m.rows[2].Unread || m.state.Seen["id-2"] != 1 {
		t.Errorf("cb-payments-v2-2: unread %v, seen %d; the cursor only went past it", m.rows[2].Unread, m.state.Seen["id-2"])
	}
	if !m.rows[4].Unread {
		t.Error("cb-q4-search-1 was marked seen half a second after the cursor got there")
	}
}

func TestCursorMovedByARefreshStartsTheWaitAgain(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 1, "id-3": 3})
	wait := clock(&m)
	m, _ = press(t, m, "down") // cb-payments-v2-1
	wait(2 * time.Second)
	// cb-payments-v2-1 was deleted elsewhere, so the cursor lands on cb-payments-v2-2.
	gone := refreshWith(m)
	gone.state.Sessions = gone.state.Sessions[1:]
	gone.live = gone.live[1:]
	m, _ = send(m, gone)
	if r, _ := m.selected(); r.Session.Name != "cb-payments-v2-2" || !r.Unread {
		t.Fatalf("selected %s, unread %v; the cursor has only just got there", r.Name(), r.Unread)
	}
	wait(time.Second)
	m, _ = send(m, gone)
	if r, _ := m.selected(); r.Unread {
		t.Errorf("%s is still unread after a second on it", r.Name())
	}
}

func TestNarrowScreenSelectionDoesNotMarkSeen(t *testing.T) {
	m, _ := unreadModel(t, 70, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	wait := clock(&m)
	m.cursor = 3
	m, _ = press(t, m, "down")
	wait(2 * time.Second)
	m, _ = send(m, refreshWith(m))
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

func TestSessionsInFoldedAndArchivedWorkflowsAreTracked(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 5})
	m.collapsed["q4-search"] = true
	m.state.Workflows[2].Archived = true // quick
	m.rebuild()
	m, _ = send(m, refreshWith(m))
	if seen, ok := m.state.Seen["id-3"]; !ok || seen != 3 {
		t.Fatalf("seen %v; cb-q4-search-1, in the folded q4-search, should get a record", m.state.Seen)
	}
	if _, ok := m.state.Seen["id-4"]; !ok {
		t.Errorf("seen %v; cb-quick-1, in the archived quick, should get a record", m.state.Seen)
	}
	// It finishes another turn while q4-search is still folded.
	r := refreshWith(m)
	s := r.snaps["id-3"]
	s.Activity = 6
	r.snaps = map[string]transcript.Snapshot{"id-1": r.snaps["id-1"], "id-2": r.snaps["id-2"], "id-3": s, "id-4": r.snaps["id-4"]}
	m, _ = send(m, r)
	if m.rows[3].Kind != RowWorkflow || m.rows[3].Stats.Unread != 1 {
		t.Errorf("row 3 is %s with %d unread, want q4-search with 1", m.rows[3].Name(), m.rows[3].Stats.Unread)
	}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "q4-search  •  [+]") {
		t.Errorf("no • on the folded workflow:\n%s", out)
	}
}

func TestComingBackToASessionInAFoldedWorkflowMarksItSeen(t *testing.T) {
	m, _ := unreadModel(t, 100, map[string]int{"id-1": 5, "id-2": 5, "id-3": 1})
	m.collapsed["q4-search"] = true
	m.rebuild()
	m, _ = send(m, attachDoneMsg{shortID: "aaaa0003"})
	m, _ = send(m, refreshWith(m))
	if m.state.Seen["id-3"] != 3 || m.rows[3].Stats.Unread != 0 {
		t.Errorf("seen %d, q4-search unread %d; you just came back from it", m.state.Seen["id-3"], m.rows[3].Stats.Unread)
	}
}
