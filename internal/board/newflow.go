package board

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// newWorkflowChoice marks the "+ new workflow…" entry in the workflow picker.
const newWorkflowChoice = "\x00new"

// launchedMsg reports a launch; on success the board opens the new session.
type launchedMsg struct {
	sess store.Session
	req  launch.Request
	from string // the session whose conversation this one continues, if any
	err  error
}

func (msg launchedMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		if claudecli.IsUntrusted(msg.err) && m.flow.trusting {
			m.flow.trusting = false
			m.message = tildePath(msg.req.Dir) + " still isn't trusted, so no session was started"
			return m, nil
		}
		if claudecli.IsUntrusted(msg.err) {
			m.flow.req, m.mode, m.message = msg.req, modeAskTrust, ""
			return m, nil
		}
		m.message = "couldn't start a session: " + msg.err.Error()
		return m, nil
	}
	m.flow.trusting = false
	m.message = "started " + msg.sess.Name
	if msg.from != "" {
		m.message = msg.sess.Name + " continues " + msg.from + "'s conversation; " + msg.from + " is still open in its terminal"
	}
	m.selectName = msg.sess.Name
	return m, m.attach(msg.sess.ShortID)
}

// trustDoneMsg arrives when the user leaves the claude they opened to accept the trust
// prompt; the launch is then retried.
type trustDoneMsg struct{ req launch.Request }

func (msg trustDoneMsg) apply(m Model) (Model, tea.Cmd) {
	cmd := m.launchCmd(msg.req)
	return m, cmd
}

// startNew opens the workflow picker, starting on the selected row's workflow.
func (m Model) startNew() (tea.Model, tea.Cmd) {
	sel := ""
	if r, ok := m.selected(); ok && (r.Kind == RowWorkflow || r.Kind == RowSession) {
		sel = r.Workflow.Slug
	}
	m.pickWorkflow(sel)
	return m, nil
}

// pickWorkflow opens the workflow picker on the workflow with slug sel, or the first.
func (m *Model) pickWorkflow(sel string) {
	var choices []choice
	cursor := 0
	for _, wf := range m.state.Workflows {
		if wf.Archived {
			continue
		}
		if wf.Slug == sel {
			cursor = len(choices)
		}
		choices = append(choices, choice{label: wf.Name, value: wf.Name})
	}
	choices = append(choices, choice{label: "+ new workflow…", value: newWorkflowChoice})
	m.flow = newFlow{choices: choices, cursor: cursor}
	m.mode = modePickWorkflow
}

// quick starts a session in the scratch folder, in the quick workflow, with no questions.
func (m Model) quick() (tea.Model, tea.Cmd) {
	dir := m.deps.Cfg.ScratchDir
	m.flow.req = launch.Request{Workflow: store.QuickSlug, Dir: dir, Repo: m.deps.Cfg.RepoName(dir)}
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) pickerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.mode, m.message = modeList, "cancelled"
	case "up", "k":
		m.flow.cursor = max(0, m.flow.cursor-1)
	case "down", "j":
		m.flow.cursor = min(len(m.flow.choices)-1, m.flow.cursor+1)
	case "enter":
		c := m.flow.choices[m.flow.cursor]
		switch {
		case m.mode == modePickRepo:
			return m.chooseRepo(c.value)
		case c.value != newWorkflowChoice:
			return m.workflowChosen(c.value)
		}
		m.flow.input = textinput.New()
		m.flow.input.Placeholder = "e.g. payments-v2"
		m.flow.input.SetWidth(30)
		m.mode = modeNameWorkflow
		cmd := m.flow.input.Focus()
		return m, cmd
	}
	return m, nil
}

func (m Model) nameKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode, m.message = modeList, "cancelled"
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.flow.input.Value())
		if store.Slug(name) == "" {
			m.message = "a workflow name needs at least one letter or digit"
			return m, nil
		}
		m.message = ""
		return m.workflowChosen(name)
	}
	var cmd tea.Cmd
	m.flow.input, cmd = m.flow.input.Update(k)
	return m, cmd
}

// workflowChosen goes on once the workflow is known: to the folder picker for a new
// session, or straight to moving the Other session.
func (m Model) workflowChosen(workflow string) (tea.Model, tea.Cmd) {
	if m.flow.move != nil {
		return m.moveInto(workflow)
	}
	m.pickRepoFor(workflow)
	return m, nil
}

// pickRepoFor opens the folder picker for workflow: configured repos, the current
// folder and the scratch folder, starting on the workflow's last folder.
func (m *Model) pickRepoFor(workflow string) {
	m.flow.req = launch.Request{Workflow: workflow}
	cfg := m.deps.Cfg
	last := ""
	if wf := m.state.Workflow(store.Slug(workflow)); wf != nil {
		last = wf.LastRepo
	}
	var choices []choice
	for _, r := range cfg.RepoList() {
		choices = append(choices, choice{label: r.Name + "  " + r.Dir, value: r.Dir})
	}
	choices = append(choices,
		choice{label: "current folder  " + m.deps.Cwd, value: m.deps.Cwd},
		choice{label: "scratch  " + cfg.ScratchDir, value: cfg.ScratchDir})
	cursor := len(choices) - 2 // current folder
	for i, c := range choices {
		if last != "" && cfg.RepoName(c.value) == last {
			cursor = i
			break
		}
	}
	m.flow.choices, m.flow.cursor = choices, cursor
	m.mode = modePickRepo
}

// chooseRepo launches in dir, first asking about a worktree if a session already runs
// there. The scratch folder isn't a git repo, so it never asks there.
func (m Model) chooseRepo(dir string) (tea.Model, tea.Cmd) {
	m.flow.req.Dir = dir
	m.flow.req.Repo = m.deps.Cfg.RepoName(dir)
	if dir != m.deps.Cfg.ScratchDir && launch.DirBusy(m.state, m.live, dir) {
		m.mode = modeAskWorktree
		return m, nil
	}
	m.mode = modeList
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) answerWorktree(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "esc" {
		m.message = "cancelled"
		return m, nil
	}
	m.flow.req.Worktree = s == "y" || s == "Y"
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) answerTrust(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := k.String(); s != "y" && s != "Y" {
		m.message = "cancelled"
		return m, nil
	}
	req := m.flow.req
	m.flow.trusting = true
	return m, tea.Exec(inTerminal(m.deps.Src.TrustCmd(req.Dir)), func(error) tea.Msg { return trustDoneMsg{req: req} })
}

// launchCmd starts the session in the background; launchedMsg reports back.
func (m *Model) launchCmd(req launch.Request) tea.Cmd {
	m.message = "starting a session…"
	return launchFor(m.deps, req, "")
}

// launchFor runs launch.Run off the UI goroutine. from names the session whose
// conversation req continues, if any.
func launchFor(d Deps, req launch.Request, from string) tea.Cmd {
	return func() tea.Msg {
		sess, err := launch.Run(context.Background(), d.Src, d.Store, d.Cfg, req, d.Now())
		return launchedMsg{sess: sess, req: req, from: from, err: err}
	}
}
