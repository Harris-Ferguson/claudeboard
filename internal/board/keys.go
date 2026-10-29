package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on the selected row. Task 15 adds n and s.
func (m Model) commandKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		return m.enter()
	case "x":
		return m.stop()
	case "r":
		return m.resume()
	case "D":
		return m.askDelete()
	case "a":
		return m.toggleArchive()
	}
	return m, nil
}

// flowKey handles keys while a question is open. Task 15 adds the new-session flow.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mode := m.mode
	m.mode = modeList
	if mode == modeConfirmDelete {
		return m.confirmDelete(k)
	}
	return m, nil
}
