package board

import (
	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// movedMsg reports a session recorded in a workflow it wasn't in.
type movedMsg struct {
	sess     store.Session // as recorded now
	name     string        // how the board referred to it
	workflow string
	err      error
}

func (msg movedMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		m.message = "couldn't move it: " + msg.err.Error()
		return m, nil
	}
	m.message = "moved " + msg.name + " into " + msg.workflow
	m.selectName = msg.sess.Name
	delete(m.collapsed, msg.sess.Workflow) // so the cursor can follow it there
	cmd := m.startRefresh()
	return m, cmd
}

// startMove opens the workflow picker for the selected session (or the one a selected
// subagent belongs to).
func (m Model) startMove() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	r = r.session()
	switch {
	case !ok || (r.Kind != RowOther && r.Kind != RowSession):
		m.message = "m moves a session into another workflow"
		return m, nil
	case r.Interactive() && !r.Snap.Found:
		m.message = r.Name() + " has no conversation yet, so there is nothing to continue"
		return m, nil
	}
	m.pickWorkflow("", r.Workflow.Slug)
	m.flow.move = &r
	return m, nil
}

// moveInto puts the session into workflow. One of claudeboard's own sessions only
// changes workflow in its record: it keeps running as it is. An interactive Other
// session is still open in its terminal, so Claude continues its conversation in a
// background copy, which the board opens. A background Other session is recorded as
// it is, without a restart.
func (m Model) moveInto(workflow string) (tea.Model, tea.Cmd) {
	r := *m.flow.move
	m.mode = modeList
	if r.Kind == RowSession {
		d, sess, name := m.deps, r.Session, r.Name()
		m.message = "moving " + name + "…"
		return m, func() tea.Msg {
			err := d.Store.Update(func(st *store.State) error {
				wf, err := st.MoveSession(sess, workflow, d.Now())
				if err == nil {
					sess.Workflow = wf.Slug
				}
				return err
			})
			return movedMsg{sess: sess, name: name, workflow: workflow, err: err}
		}
	}
	if r.Interactive() {
		dir := r.Live.Cwd
		m.flow.req = launch.Request{Workflow: workflow, Dir: dir, Repo: m.deps.Cfg.RepoName(dir), Resume: r.Live.SessionID}
		m.message = "continuing " + r.Name() + " in the background…"
		return m, launchFor(m.deps, m.flow.req, r.Name())
	}
	d, live, name := m.deps, *r.Live, r.Name()
	m.message = "moving " + name + "…"
	return m, func() tea.Msg {
		sess, err := launch.Adopt(d.Store, d.Cfg, workflow, live, d.Now())
		return movedMsg{sess: sess, name: name, workflow: workflow, err: err}
	}
}
