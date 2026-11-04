package board

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// renamedMsg reports a session's new label being saved.
type renamedMsg struct {
	name  string // the session's Claude name
	label string // "" when it went back to its first prompt
	err   error
}

func (msg renamedMsg) apply(m Model) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.message = "couldn't rename it: " + msg.err.Error()
		return m, nil
	case msg.label == "":
		m.message = msg.name + " shows its first prompt again"
	default:
		m.message = "renamed " + msg.name + " to " + msg.label
	}
	cmd := m.startRefresh()
	return m, cmd
}

// startRename asks for a new label for the selected session (or the one a selected
// subagent belongs to), starting with the one it shows now.
func (m Model) startRename() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	r = r.session()
	switch {
	case ok && r.Kind == RowOther:
		m.message = "R renames sessions in a workflow; m moves this one into one"
		return m, nil
	case !ok || r.Kind != RowSession:
		m.message = "R renames a session"
		return m, nil
	}
	m.pending, m.mode = r, modeRename
	m.flow.input = textinput.New()
	m.flow.input.SetValue(r.label())
	m.flow.input.SetWidth(40)
	cmd := m.flow.input.Focus()
	return m, cmd
}

func (m Model) renameKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode, m.message = modeList, "cancelled"
		return m, nil
	case "enter":
		m.mode = modeList
		r := m.pending
		label := strings.TrimSpace(m.flow.input.Value())
		if label == promptLabel(r.Snap) {
			label = "" // the first prompt as it is: keep showing that, not a copy of it
		}
		d, sess := m.deps, r.Session
		return m, func() tea.Msg {
			err := d.Store.Update(func(st *store.State) error { return st.SetLabel(sess, label) })
			return renamedMsg{name: sess.Name, label: label, err: err}
		}
	}
	var cmd tea.Cmd
	m.flow.input, cmd = m.flow.input.Update(k)
	return m, cmd
}
