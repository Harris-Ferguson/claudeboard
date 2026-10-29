package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on the selected row or start sessions.
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
	case "m":
		return m.startMove()
	case "n":
		return m.startNew()
	case "s":
		return m.quick()
	case "N":
		return m.quickNew()
	}
	return m, nil
}

// flowKey handles keys while a picker or a y/N question is open.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modePickWorkflow, modePickRepo:
		return m.pickerKey(k)
	case modeNameWorkflow:
		return m.nameKey(k)
	}
	mode := m.mode
	m.mode = modeList
	switch mode {
	case modeConfirmDelete:
		return m.confirmDelete(k)
	case modeAskWorktree:
		return m.answerWorktree(k)
	case modeAskTrust:
		return m.answerTrust(k)
	}
	return m, nil
}
