package board

import (
	"fmt"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// agentRunning reports whether a subagent counts as running: Claude hasn't recorded
// that it finished, its session is running, and it did something since the session's
// process last started, as a subagent's work doesn't survive a restart.
func agentRunning(owner Row, a transcript.Agent) bool {
	if a.Ended || !owner.Running() {
		return false
	}
	if s := owner.Live.StartedAt; s > 0 && a.Last.Before(time.UnixMilli(s)) {
		return false
	}
	return true
}

// agentRows lays out a session's subagents under it: the running ones as a tree, then
// one line for the finished ones, followed by them when open.
func agentRows(owner Row, open bool) []Row {
	var running, finished []transcript.Agent
	for _, a := range owner.Snap.Agents {
		if agentRunning(owner, a) {
			running = append(running, a)
		} else {
			finished = append(finished, a)
		}
	}
	o := &owner
	var rows []Row
	add := func(a transcript.Agent, tree string, kind status.Kind) {
		rows = append(rows, Row{Kind: RowAgent, Owner: o, Agent: a, Tree: tree,
			Status: status.Status{Kind: kind}, Tokens: a.Usage.Fresh()})
	}
	walkTree(running, "", len(finished) == 0, func(a transcript.Agent, tree string) { add(a, tree, status.Working) })
	if len(finished) == 0 {
		return rows
	}
	fold := Row{Kind: RowAgentsFolded, Owner: o, Tree: "└ ", Finished: len(finished), Collapsed: !open,
		Status: status.Status{Kind: status.Done}}
	for _, a := range finished {
		fold.Tokens += a.Usage.Fresh()
	}
	rows = append(rows, fold)
	if open {
		walkTree(finished, "  ", true, func(a transcript.Agent, tree string) { add(a, tree, status.Done) })
	}
	return rows
}

// walkTree calls emit for each agent, parents before the agents they started, with the
// tree lines to draw before it ("├ ", "│ └ "…). An agent whose parent isn't in agents
// goes at the top. ends says nothing follows them at the top level.
func walkTree(agents []transcript.Agent, indent string, ends bool, emit func(a transcript.Agent, tree string)) {
	in := map[string]bool{}
	for _, a := range agents {
		in[a.ID] = true
	}
	children := map[string][]transcript.Agent{}
	var top []transcript.Agent
	for _, a := range agents {
		if a.Parent != "" && a.Parent != a.ID && in[a.Parent] {
			children[a.Parent] = append(children[a.Parent], a)
		} else {
			top = append(top, a)
		}
	}
	var walk func(list []transcript.Agent, indent string, ends bool)
	walk = func(list []transcript.Agent, indent string, ends bool) {
		for i, a := range list {
			glyph, below := "├ ", "│ "
			if ends && i == len(list)-1 {
				glyph, below = "└ ", "  "
			}
			emit(a, indent+glyph)
			walk(children[a.ID], indent+below, true)
		}
	}
	walk(top, indent, ends)
}

// agentLabel is what a subagent row shows: Claude's few words for the task.
func agentLabel(a transcript.Agent) string {
	switch {
	case a.Description != "":
		return a.Description
	case a.Type != "":
		return a.Type
	}
	return a.ID
}

// agentsFoldedLabel is the finished line's text: "✓ 3 finished", with [+] while folded.
func agentsFoldedLabel(r Row) string {
	s := statusIcon(r.Status) + fmt.Sprintf(" %d finished", r.Finished)
	if r.Collapsed {
		s += "  [+]"
	}
	return s
}
