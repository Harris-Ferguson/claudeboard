// Package transcript reads Claude Code conversation files (one JSON record per line)
// and turns them into preview items and token totals.
package transcript

import "time"

// ItemKind says who an Item comes from.
type ItemKind int

const (
	ItemPrompt ItemKind = iota // something the user typed
	ItemText                   // Claude's reply text
	ItemTool                   // a tool call, summarised
)

// Item is one entry of the preview.
type Item struct {
	Kind ItemKind
	Tool string // ItemTool: the tool's name, e.g. "Bash"
	Text string // the prompt or reply; for a tool call, its most telling argument
}

// Usage is a token count split by type.
type Usage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total adds up every token type.
func (u Usage) Total() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

// Fresh is the tokens a session actually added: everything except cache reads, which
// re-count the same context on every turn and so dwarf the rest (often 95%+).
func (u Usage) Fresh() int64 { return u.Input + u.Output + u.CacheWrite }

func (u Usage) plus(v Usage) Usage {
	return Usage{u.Input + v.Input, u.Output + v.Output, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite}
}

// Snapshot is what the board needs to know about a session's conversation.
type Snapshot struct {
	Found       bool   // the conversation file exists
	Items       []Item // the most recent preview items, oldest first
	Pending     string // the tool call still waiting for a result, summarised
	Usage       Usage  // tokens, including subagents; each message counts once
	FirstPrompt string
	Prompts     int
	Agents      []Agent // the session's subagents, in the order they started
	// Activity counts Claude's records in the main conversation (Claude Code writes one
	// per content block). It starts over if the file is rewritten, so it can go down.
	Activity int
}

// Agent is a subagent: a conversation the session (or another subagent) started with
// the Agent tool, kept in its own file next to the session's.
type Agent struct {
	ID          string // from its file name, agent-<ID>.jsonl
	Parent      string // the ID of the subagent that started it; "" for the session
	Type        string // e.g. "Explore"; "" if Claude didn't record it
	Description string // the few words Claude gave the task
	Items       []Item
	Pending     string
	// Usage is its own messages' tokens. A fork's file starts with a copy of the message
	// that started it, which counts for whoever started it.
	Usage Usage
	Last  time.Time // its newest record
	// Ended says Claude recorded that it finished after its last step: a notice that it
	// stopped, or for one Claude waited on, the result of the call that started it. It
	// can't say whether one is still running after its session stopped or restarted.
	Ended bool
}
