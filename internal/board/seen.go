package board

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// noteSeen marks sessions as seen: the selected one when the preview shows it, the one
// with short ID back (the session the user just came back from), and any the state
// has no record of yet, so sessions don't all start out unread. It updates the rows
// and returns a command that saves the change, or nil when nothing changed.
func (m *Model) noteSeen(back string) tea.Cmd {
	showing := !m.panelMode() && m.previewVisible() && m.previewFits()
	changes := map[string]int{}
	for i, r := range m.rows {
		id := r.SessionID()
		if id == "" || (r.Kind != RowSession && r.Kind != RowOther) {
			continue
		}
		seen, known := m.state.Seen[id]
		looked := (i == m.cursor && showing) || (back != "" && r.ShortID() == back)
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
