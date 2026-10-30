package transcript

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// maxItems is how many preview items a snapshot keeps.
const maxItems = 200

type record struct {
	Type        string          `json:"type"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
	Operation   string          `json:"operation"` // queue-operation records
	Content     json.RawMessage `json:"content"`   // queue-operation records: what was queued
}

type message struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// blocks decodes message content, which is either a plain string or a list of blocks.
func blocks(content json.RawMessage) []block {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	_ = json.Unmarshal(content, &bs)
	return bs
}

// accumulator folds one conversation file's lines into a Snapshot.
type accumulator struct {
	// side is set for a subagent's own file, whose records are all marked sidechain. In
	// a session's file, sidechain records belong to subagents and are left out.
	side         bool
	items        []Item
	usage        map[string]Usage  // by API message ID, so repeated records count once
	pending      map[string]string // tool_use ID → summary, until its result arrives
	pendingOrder []string
	firstPrompt  string
	prompts      int
	activity     int
	first, last  time.Time // the oldest and newest record

	agentCalls map[string]bool      // IDs of the calls here that started subagents
	results    map[string]time.Time // when those calls got their result
	notified   map[string]time.Time // the latest "task finished" notice, by agent ID
}

func newAccumulator(side bool) *accumulator {
	return &accumulator{side: side, usage: map[string]Usage{}, pending: map[string]string{},
		agentCalls: map[string]bool{}, results: map[string]time.Time{}, notified: map[string]time.Time{}}
}

// add applies one line. Lines that aren't JSON, or are record types claudeboard doesn't
// use, are ignored.
func (a *accumulator) add(line []byte) {
	var r record
	if json.Unmarshal(line, &r) != nil {
		return
	}
	at, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
	if !at.IsZero() {
		if a.first.IsZero() || at.Before(a.first) {
			a.first = at
		}
		if at.After(a.last) {
			a.last = at
		}
	}
	if r.Type == "queue-operation" && r.Operation == "enqueue" {
		a.notice(r.Content, at)
		return
	}
	if len(r.Message) == 0 {
		return
	}
	var m message
	if json.Unmarshal(r.Message, &m) != nil {
		return
	}
	switch r.Type {
	case "assistant":
		if m.Usage != nil && m.ID != "" {
			a.usage[m.ID] = Usage{
				Input:      m.Usage.InputTokens,
				Output:     m.Usage.OutputTokens,
				CacheRead:  m.Usage.CacheReadInputTokens,
				CacheWrite: m.Usage.CacheCreationInputTokens,
			}
		}
		if r.IsSidechain && !a.side {
			return
		}
		a.activity++
		for _, b := range blocks(m.Content) {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" {
					a.push(Item{Kind: ItemText, Text: t})
				}
			case "tool_use":
				if (b.Name == "Agent" || b.Name == "Task") && b.ID != "" {
					a.agentCalls[b.ID] = true
				}
				if hiddenTools[b.Name] {
					continue
				}
				label, detail := describe(b.Name, b.Input, true)
				a.push(Item{Kind: ItemTool, Tool: label, Text: detail})
				if b.ID != "" {
					a.pending[b.ID] = Summarize(b.Name, b.Input)
					a.pendingOrder = append(a.pendingOrder, b.ID)
				}
			}
		}
	case "user":
		if r.IsMeta || (r.IsSidechain && !a.side) {
			return
		}
		var texts []string
		for _, b := range blocks(m.Content) {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" && !isSystemText(t) {
					texts = append(texts, t)
				}
			case "tool_result":
				delete(a.pending, b.ToolUseID)
				if a.agentCalls[b.ToolUseID] {
					a.results[b.ToolUseID] = at
				}
			}
		}
		if len(texts) == 0 {
			return
		}
		text := strings.Join(texts, "\n")
		a.prompts++
		if a.firstPrompt == "" {
			a.firstPrompt = text
		}
		a.push(Item{Kind: ItemPrompt, Text: text})
	}
}

// systemPrefixes start the user-role text that Claude Code writes itself: slash-command
// and shell wrappers, reminders, notifications and interruption notices. Anything else,
// including pasted code that starts with "<", is a real prompt.
var systemPrefixes = []string{
	"<command-", "<local-command-", "<bash-", "<system-", "<task-notification",
	"<user-prompt-submit-hook", "[Request interrupted",
}

func isSystemText(t string) bool {
	for _, p := range systemPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// taskID finds the agent IDs in a notification Claude Code queues for the session.
var taskID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)

// notice records a queued "task finished" notification: Claude Code queues one each time
// a subagent stops. It names the agent by ID; the tool call it names may be a later
// message to the agent rather than the call that started it.
func (a *accumulator) notice(content json.RawMessage, at time.Time) {
	var s string
	if json.Unmarshal(content, &s) != nil || !strings.HasPrefix(s, "<task-notification>") {
		return
	}
	for _, m := range taskID.FindAllStringSubmatch(s, -1) {
		if at.After(a.notified[m[1]]) {
			a.notified[m[1]] = at
		}
	}
}

func (a *accumulator) push(it Item) {
	a.items = append(a.items, it)
	if len(a.items) > maxItems {
		a.items = a.items[len(a.items)-maxItems:]
	}
}

// pendingTool is the most recent tool call that has no result yet.
func (a *accumulator) pendingTool() string {
	for i := len(a.pendingOrder) - 1; i >= 0; i-- {
		if s, ok := a.pending[a.pendingOrder[i]]; ok {
			return s
		}
	}
	a.pendingOrder = a.pendingOrder[:0]
	return ""
}

// total is the tokens of this file's messages.
func (a *accumulator) total() Usage {
	var u Usage
	for _, v := range a.usage {
		u = u.plus(v)
	}
	return u
}

func (a *accumulator) snapshot() Snapshot {
	return Snapshot{
		Found:       true,
		Items:       append([]Item(nil), a.items...),
		Pending:     a.pendingTool(),
		Usage:       a.total(),
		FirstPrompt: a.firstPrompt,
		Prompts:     a.prompts,
		Activity:    a.activity,
	}
}

// hiddenTools are calls Claude makes to manage itself rather than to do the work, such
// as loading its own tool definitions. The preview leaves them out.
var hiddenTools = map[string]bool{"ToolSearch": true}

// maxDetail is the most of a tool call's argument kept; the board cuts it to fit.
const maxDetail = 120

// leadingCd matches "cd <folder> &&" (or ";") at the start of a command. Sessions often
// cd into their own folder first, which says nothing in a one-line summary.
var leadingCd = regexp.MustCompile(`^cd\s+("[^"]*"|'[^']*'|\S+)\s*(&&|;)\s*`)

// fallbackArgs are tried in order for tools claudeboard doesn't know, such as MCP tools.
var fallbackArgs = []string{"title", "subject", "description", "file_path", "path", "url", "query", "skill", "command", "pattern", "prompt"}

// Summarize describes a tool call in a few words: the tool name plus its most telling
// argument (a file's base name, a command, a search pattern, a URL). For Bash that is
// the command, since it's what a permission prompt asks about.
func Summarize(name string, input json.RawMessage) string {
	label, detail := describe(name, input, false)
	if detail == "" {
		return label
	}
	return label + " " + detail
}

// describe splits a tool call into a label (the tool's name) and its most telling
// argument. With narrate, a Bash command is replaced by Claude's own description of it,
// which reads better in the preview than a long command line.
func describe(name string, input json.RawMessage, narrate bool) (label, detail string) {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	arg := func(k string) string {
		s, _ := in[k].(string)
		return strings.Join(strings.Fields(s), " ")
	}
	label = name
	switch name {
	case "Read", "Edit", "Write", "MultiEdit":
		if p := arg("file_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "NotebookEdit":
		if p := arg("notebook_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "Bash", "Monitor":
		detail = leadingCd.ReplaceAllString(arg("command"), "")
		if d := arg("description"); narrate && d != "" {
			detail = d
		}
	case "Grep", "Glob":
		detail = arg("pattern")
	case "Task", "Agent":
		detail = arg("description")
	case "AskUserQuestion":
		if qs, _ := in["questions"].([]any); len(qs) > 0 {
			if q, _ := qs[0].(map[string]any); q != nil {
				s, _ := q["question"].(string)
				detail = strings.Join(strings.Fields(s), " ")
			}
		}
	default:
		if rest, ok := strings.CutPrefix(name, "mcp__"); ok {
			label = strings.ReplaceAll(rest, "__", " ")
		}
		for _, k := range fallbackArgs {
			if detail = arg(k); detail != "" {
				break
			}
		}
	}
	if r := []rune(detail); len(r) > maxDetail {
		detail = string(r[:maxDetail]) + "…"
	}
	return label, detail
}
