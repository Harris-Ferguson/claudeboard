package board

import (
	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// RowKind is what a line in the left pane shows.
type RowKind int

const (
	RowWorkflow     RowKind = iota // a workflow heading
	RowSession                     // a claudeboard session
	RowOtherHeader                 // the "Other sessions" heading; never selected
	RowOther                       // a session claudeboard didn't start
	RowAgent                       // a subagent, under its session
	RowAgentsFolded                // the line for a session's finished subagents
)

// Row is one line of the left pane plus everything the preview needs about it.
type Row struct {
	Kind      RowKind
	Workflow  store.Workflow     // RowWorkflow, and the parent of a RowSession
	Session   store.Session      // RowSession
	Live      *claudecli.Session // what Claude lists; nil when it doesn't list the session
	Snap      transcript.Snapshot
	Status    status.Status
	Tokens    int64  // fresh tokens (cache reads left out): the session's, or the workflow's
	Stats     Counts // RowWorkflow: its sessions
	Sessions  int    // RowWorkflow: how many sessions it has
	Collapsed bool   // RowWorkflow; RowAgentsFolded: the finished subagents are hidden
	Unread    bool   // a background session that finished a turn since the user last saw it

	Owner    *Row             // RowAgent, RowAgentsFolded: the session the subagents belong to
	Agent    transcript.Agent // RowAgent
	Tree     string           // RowAgent, RowAgentsFolded: the tree lines drawn before it
	Finished int              // RowAgentsFolded: how many subagents finished
}

// Counts summarises sessions for the header and workflow rows.
type Counts struct {
	Working  int
	NeedsYou int
	Unread   int
	Tokens   int64
}

// Key identifies a row across refreshes so the cursor stays on it.
func (r Row) Key() string {
	switch r.Kind {
	case RowWorkflow:
		return "w:" + r.Workflow.Slug
	case RowSession:
		if r.Session.SessionID != "" {
			return "s:" + r.Session.SessionID
		}
		return "s:" + r.Session.Name
	case RowOtherHeader:
		return "other"
	case RowAgent:
		return "a:" + r.Owner.Key() + "/" + r.Agent.ID
	case RowAgentsFolded:
		return "f:" + r.Owner.Key()
	}
	return "o:" + r.Live.SessionID
}

// session is the row's session: the row itself, or for a subagent row, its session's.
func (r Row) session() Row {
	if r.Owner != nil {
		return *r.Owner
	}
	return r
}

// ShortID is the ID `claude attach`, `stop` and `rm` take.
func (r Row) ShortID() string {
	if r.Live != nil && r.Live.ShortID != "" {
		return r.Live.ShortID
	}
	return r.Session.ShortID
}

// Name is how the row's session is referred to in messages.
func (r Row) Name() string {
	switch {
	case r.Kind == RowSession:
		return r.Session.Name
	case r.Kind == RowWorkflow:
		return r.Workflow.Name
	case r.Kind == RowAgent:
		return agentLabel(r.Agent)
	case r.Kind == RowAgentsFolded:
		return r.Owner.Name() + "'s finished subagents"
	case r.Live == nil:
		return ""
	case r.Live.Name != "":
		return r.Live.Name
	}
	return r.Live.ShortID
}

// SessionID is the full ID of a session row's session, or "".
func (r Row) SessionID() string {
	if r.Live != nil && r.Live.SessionID != "" {
		return r.Live.SessionID
	}
	return r.Session.SessionID
}

// unread reports whether a background session finished a turn (it's done, or waiting
// for the user) since the user last saw it. A session the state has no record of isn't:
// the board records it as seen when it first sees it.
func unread(st store.State, id string, l *claudecli.Session, snap transcript.Snapshot, s status.Status) bool {
	seen, ok := st.Seen[id]
	if !ok || l == nil || l.Kind != claudecli.KindBackground {
		return false
	}
	return snap.Activity > seen && (s.Kind == status.Done || s.Kind == status.NeedsYou)
}

// Interactive reports whether the row is a session running in someone's terminal,
// which claudeboard can't open, stop or delete.
func (r Row) Interactive() bool {
	return r.Kind == RowOther && r.Live.Kind == claudecli.KindInteractive
}

// Running reports whether the row's background session is in Claude's background
// service: listed, and not stopped, failed, crashed or exited. Other states, such as one
// it passes through while starting, count as running, because resuming a session that is
// running starts a copy of it.
func (r Row) Running() bool {
	if r.Live == nil {
		return false
	}
	switch r.Live.State {
	case "stopped", "failed", "crashed", "exited":
		return false
	}
	return true
}

// BuildRows lays out the left pane: each workflow (in creation order) followed by its
// sessions unless collapsed, then sessions Claude lists that claudeboard didn't start.
// Each session is followed by its subagents; openAgents (by the session row's Key)
// says whose finished ones are shown. Archived workflows are left out unless
// showArchived. Counts covers the visible claudeboard sessions only.
func BuildRows(st store.State, live []claudecli.Session, snaps map[string]transcript.Snapshot, collapsed, openAgents map[string]bool, showArchived bool) ([]Row, Counts) {
	byID := map[string]*claudecli.Session{}
	byShort := map[string]*claudecli.Session{}
	for i := range live {
		l := &live[i]
		if l.SessionID != "" {
			byID[l.SessionID] = l
		}
		if l.ShortID != "" {
			byShort[l.ShortID] = l
		}
	}
	known := map[string]bool{}
	var rows []Row
	var total Counts
	for _, wf := range st.Workflows {
		head := Row{Kind: RowWorkflow, Workflow: wf, Collapsed: collapsed[wf.Slug]}
		var children []Row
		for _, s := range st.Sessions {
			if s.Workflow != wf.Slug {
				continue
			}
			l := byID[s.SessionID]
			if l == nil && s.SessionID == "" {
				l = byShort[s.ShortID]
			}
			snapID := s.SessionID
			if l != nil {
				known[l.SessionID] = true
				snapID = l.SessionID
			}
			known[s.SessionID] = true
			snap := snaps[snapID]
			r := Row{Kind: RowSession, Workflow: wf, Session: s, Live: l, Snap: snap,
				Status: status.From(l, snap.Prompts > 0), Tokens: snap.Usage.Fresh()}
			r.Unread = unread(st, snapID, l, snap, r.Status)
			if r.Unread {
				head.Stats.Unread++
			}
			head.Sessions++
			head.Tokens += r.Tokens
			head.Stats.Tokens += r.Tokens
			switch r.Status.Kind {
			case status.Working:
				head.Stats.Working++
			case status.NeedsYou:
				head.Stats.NeedsYou++
			}
			children = append(children, r)
			children = append(children, agentRows(r, openAgents[r.Key()])...)
		}
		if wf.Archived && !showArchived {
			continue
		}
		total.Working += head.Stats.Working
		total.NeedsYou += head.Stats.NeedsYou
		total.Unread += head.Stats.Unread
		total.Tokens += head.Stats.Tokens
		rows = append(rows, head)
		if !head.Collapsed {
			rows = append(rows, children...)
		}
	}
	var others []Row
	for i := range live {
		l := &live[i]
		if l.SessionID == "" || known[l.SessionID] {
			continue
		}
		snap := snaps[l.SessionID]
		r := Row{Kind: RowOther, Live: l, Snap: snap, Status: status.From(l, snap.Prompts > 0), Tokens: snap.Usage.Fresh()}
		r.Unread = unread(st, l.SessionID, l, snap, r.Status)
		others = append(others, r)
		others = append(others, agentRows(r, openAgents[r.Key()])...)
	}
	if len(others) > 0 {
		rows = append(rows, Row{Kind: RowOtherHeader})
		rows = append(rows, others...)
	}
	return rows, total
}
