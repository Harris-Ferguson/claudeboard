package board

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// movedMsg reports a background session added to a workflow.
type movedMsg struct {
	sess     store.Session
	workflow string
	err      error
}

func (msg movedMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		m.message = "couldn't move it: " + msg.err.Error()
		return m, nil
	}
	m.message = "moved " + msg.sess.Name + " into " + msg.workflow
	m.selectName = msg.sess.Name
	cmd := m.startRefresh()
	return m, cmd
}

// startMove opens the workflow picker for the selected Other session.
func (m Model) startMove() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	switch {
	case !ok || r.Kind != RowOther:
		m.message = "m moves a session from Other sessions into a workflow"
		return m, nil
	case r.Interactive() && !r.Snap.Found:
		m.message = r.Name() + " has no conversation yet, so there is nothing to continue"
		return m, nil
	}
	m.pickWorkflow("")
	m.flow.move = &r
	return m, nil
}

// moveInto puts the Other session into workflow. An interactive session is still open
// in its terminal, so Claude continues its conversation in a background copy, which the
// board opens. A background session is recorded as it is, without a restart.
func (m Model) moveInto(workflow string) (tea.Model, tea.Cmd) {
	r := *m.flow.move
	m.mode = modeList
	if r.Interactive() {
		dir := r.Live.Cwd
		m.flow.req = launch.Request{Workflow: workflow, Dir: dir, Repo: m.deps.Cfg.RepoName(dir), Resume: r.Live.SessionID}
		m.message = "continuing " + r.Name() + " in the background…"
		return m, launchFor(m.deps, m.flow.req, r.Name())
	}
	d, live := m.deps, *r.Live
	m.message = "moving " + r.Name() + "…"
	return m, func() tea.Msg {
		sess, err := launch.Adopt(d.Store, d.Cfg, workflow, live, d.Now())
		return movedMsg{sess: sess, workflow: workflow, err: err}
	}
}
