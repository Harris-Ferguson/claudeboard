// Package transcript reads Claude Code conversation files (one JSON record per line)
// and turns them into preview items and token totals.
package transcript

// ItemKind says who an Item comes from.
type ItemKind int

const (
	ItemPrompt ItemKind = iota // something the user typed
	ItemText                   // Claude's reply text
	ItemTool                   // a tool call, summarised
)

// Item is one line of the preview.
type Item struct {
	Kind ItemKind
	Text string
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

func (u Usage) plus(v Usage) Usage {
	return Usage{u.Input + v.Input, u.Output + v.Output, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite}
}

// Snapshot is what the board needs to know about a session's conversation.
type Snapshot struct {
	Found       bool   // the conversation file exists
	Items       []Item // the most recent preview items, oldest first
	Pending     string // the tool call still waiting for a result, summarised
	Usage       Usage  // tokens, including subagents
	FirstPrompt string
	Prompts     int
	Subagents   int
}
