package board

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

const msgInteractive = "that session runs in another terminal; switch to it there"

// actionDoneMsg reports the result of a stop, delete, resume or archive.
type actionDoneMsg struct {
	text string
	err  error
}

func (msg actionDoneMsg) apply(m Model) (Model, tea.Cmd) {
	m.message = msg.text
	if msg.err != nil {
		m.message = msg.err.Error()
	}
	cmd := m.startRefresh()
	return m, cmd
}

// resumedMsg says a session was resumed so it can be opened.
type resumedMsg struct{ shortID string }

func (msg resumedMsg) apply(m Model) (Model, tea.Cmd) { return m, m.attach(msg.shortID) }

// attachDoneMsg arrives when the user comes back from a session.
type attachDoneMsg struct{ err error }

func (msg attachDoneMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		m.message = "claude attach: " + msg.err.Error()
	}
	cmd := m.startRefresh()
	return m, cmd
}

// attach hands the terminal to `claude attach` until the user presses ctrl+z.
func (m Model) attach(shortID string) tea.Cmd {
	if shortID == "" {
		return nil
	}
	return tea.ExecProcess(m.deps.Src.AttachCmd(shortID), func(err error) tea.Msg { return attachDoneMsg{err: err} })
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch {
	case r.Kind == RowWorkflow:
		m.collapsed[r.Workflow.Slug] = !m.collapsed[r.Workflow.Slug]
		m.rebuild()
		return m, nil
	case r.Interactive():
		m.message = msgInteractive
		return m, nil
	case r.Kind == RowSession && r.Live == nil:
		// Claude no longer lists it (deleted elsewhere, or its background service
		// restarted): resume from the saved conversation, then open it.
		if r.Session.SessionID == "" {
			m.message = "can't reopen " + r.Name() + ": Claude doesn't list it and its session ID is unknown"
			return m, nil
		}
		m.message = "resuming " + r.Name() + "…"
		return m, m.resumeCmd(r, true)
	}
	// Stopped background sessions are listed too; `claude attach` wakes them.
	return m, m.attach(r.ShortID())
}

func (m Model) resumeCmd(r Row, thenAttach bool) tea.Cmd {
	src, s := m.deps.Src, r.Session
	return func() tea.Msg {
		id, err := src.Resume(context.Background(), s.SessionID, s.WorkDir())
		switch {
		case err != nil:
			return actionDoneMsg{err: err}
		case thenAttach:
			return resumedMsg{shortID: id}
		}
		return actionDoneMsg{text: "resumed " + s.Name}
	}
}

// actionable reports whether x and D can act on the row, explaining in the footer
// when they can't.
func (m *Model) actionable(r Row) bool {
	switch {
	case r.Kind != RowSession && r.Kind != RowOther:
		return false
	case r.Interactive():
		m.message = msgInteractive
		return false
	}
	return true
}

func (m Model) stop() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || !m.actionable(r) {
		return m, nil
	}
	name := r.Name()
	if r.Status.Kind == status.Stopped {
		m.message = name + " is already stopped"
		return m, nil
	}
	src, id := m.deps.Src, r.ShortID()
	m.message = "stopping " + name + "…"
	return m, func() tea.Msg {
		_, err := src.Stop(context.Background(), id)
		return actionDoneMsg{text: "stopped " + name, err: err}
	}
}

func (m Model) resume() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || r.Kind != RowSession {
		return m, nil
	}
	switch {
	case r.Status.Kind != status.Stopped:
		m.message = r.Name() + " is already running"
		return m, nil
	case r.Session.SessionID == "":
		m.message = "can't resume " + r.Name() + ": its session ID is unknown"
		return m, nil
	}
	m.message = "resuming " + r.Name() + "…"
	return m, m.resumeCmd(r, false)
}

func (m Model) askDelete() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || !m.actionable(r) {
		return m, nil
	}
	m.pending, m.mode = r, modeConfirmDelete
	return m, nil
}

func (m Model) confirmDelete(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := k.String(); s != "y" && s != "Y" {
		m.message = "cancelled"
		return m, nil
	}
	m.message = "deleting " + m.pending.Name() + "…"
	return m, m.deleteCmd(m.pending)
}

// deleteCmd runs `claude rm` (unless Claude no longer lists the session) and, for
// claudeboard's own sessions, removes the record from state.json.
func (m Model) deleteCmd(r Row) tea.Cmd {
	src, st, name := m.deps.Src, m.deps.Store, r.Name()
	return func() tea.Msg {
		var out string
		if r.Live != nil {
			o, err := src.Remove(context.Background(), r.ShortID())
			if err != nil {
				return actionDoneMsg{err: err}
			}
			out = o
		}
		if r.Kind == RowSession {
			err := st.Update(func(s *store.State) error {
				s.RemoveSession(r.Session.SessionID, r.Session.Name)
				return nil
			})
			if err != nil {
				return actionDoneMsg{err: err}
			}
		}
		text := "deleted " + name
		if d := rmDetails(out); d != "" {
			text += " · " + d
		}
		return actionDoneMsg{text: text}
	}
}

// rmDetails keeps what `claude rm` printed beyond its "removed <id>" line, such as a
// note about a worktree it kept.
func rmDetails(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "removed ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, " ")
}

func (m Model) toggleArchive() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || (r.Kind != RowWorkflow && r.Kind != RowSession) {
		return m, nil
	}
	slug, st := r.Workflow.Slug, m.deps.Store
	return m, func() tea.Msg {
		archived := false
		err := st.Update(func(s *store.State) error {
			wf := s.Workflow(slug)
			if wf == nil {
				return fmt.Errorf("workflow %s not found", slug)
			}
			wf.Archived = !wf.Archived
			archived = wf.Archived
			return nil
		})
		switch {
		case err != nil:
			return actionDoneMsg{err: err}
		case archived:
			return actionDoneMsg{text: "archived " + slug + " (A shows archived workflows)"}
		}
		return actionDoneMsg{text: "unarchived " + slug}
	}
}
