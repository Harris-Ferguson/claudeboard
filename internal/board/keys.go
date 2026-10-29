package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on sessions. Tasks 14 and 15 fill it in.
func (m Model) commandKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m, nil }

// flowKey handles keys while a question is open. Tasks 14 and 15 fill it in.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeList
	return m, nil
}
