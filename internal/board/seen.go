package board

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// noteCursor restarts the wait before the selected session counts as seen when the
// cursor is on a different row than before, however it got there.
func (m *Model) noteCursor() {
	key := ""
	if r, ok := m.selected(); ok {
		key = r.Key()
	}
	if key != m.selKey {
		m.selKey, m.selSince = key, m.deps.Now()
	}
}

// noteSeen marks sessions as seen: the selected one once the cursor has rested on it
// for lookTime with the preview showing (so moving past a session doesn't count), the
// one with short ID back (the session the user just came back from), and any the
// state has no record of yet, so sessions don't all start out unread. It runs after
// each refresh, updates the rows and returns a command that saves the change, or nil
// when nothing changed.
func (m *Model) noteSeen(back string) tea.Cmd {
	showing := !m.panelMode() && m.previewVisible() && m.previewFits()
	rested := showing && m.deps.Now().Sub(m.selSince) >= lookTime
	changes := map[string]int{}
	for i, r := range m.rows {
		id := r.SessionID()
		if id == "" || (r.Kind != RowSession && r.Kind != RowOther) {
			continue
		}
		seen, known := m.state.Seen[id]
		looked := (i == m.cursor && rested) || (back != "" && r.ShortID() == back)
		if !known || (looked && seen < r.Snap.Activity) {
			changes[id] = r.Snap.Activity
		}
	}
	if len(changes) == 0 {
		return nil
	}
	if m.seen == nil {
		m.seen = map[string]int{}
	}
	for id, n := range changes {
		m.seen[id] = n
	}
	m.rebuild()
	st := m.deps.Store
	return func() tea.Msg {
		err := st.Update(func(s *store.State) error {
			for id, n := range changes {
				if cur, ok := s.Seen[id]; !ok || cur < n {
					s.MarkSeen(id, n)
				}
			}
			return nil
		})
		if err != nil {
			return actionDoneMsg{err: fmt.Errorf("saving what you've seen: %w", err)}
		}
		return nil
	}
}
