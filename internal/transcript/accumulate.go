package transcript

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// maxItems is how many preview items a snapshot keeps.
const maxItems = 200

type record struct {
	Type        string          `json:"type"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
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

// accumulator folds transcript lines into a Snapshot.
type accumulator struct {
	items        []Item
	usage        map[string]Usage  // by API message ID, so repeated records count once
	pending      map[string]string // tool_use ID → summary, until its result arrives
	pendingOrder []string
	firstPrompt  string
	prompts      int
}

func newAccumulator() *accumulator {
	return &accumulator{usage: map[string]Usage{}, pending: map[string]string{}}
}

// add applies one line. Lines that aren't JSON, or are record types claudeboard doesn't
// use, are ignored. Subagent files (main == false) only contribute tokens.
func (a *accumulator) add(line []byte, main bool) {
	var r record
	if json.Unmarshal(line, &r) != nil || len(r.Message) == 0 {
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
		if !main || r.IsSidechain {
			return
		}
		for _, b := range blocks(m.Content) {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" {
					a.push(Item{Kind: ItemText, Text: t})
				}
			case "tool_use":
				s := Summarize(b.Name, b.Input)
				a.push(Item{Kind: ItemTool, Text: s})
				if b.ID != "" {
					a.pending[b.ID] = s
					a.pendingOrder = append(a.pendingOrder, b.ID)
				}
			}
		}
	case "user":
		if !main || r.IsMeta || r.IsSidechain {
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

func (a *accumulator) snapshot() Snapshot {
	var u Usage
	for _, v := range a.usage {
		u = u.plus(v)
	}
	return Snapshot{
		Found:       true,
		Items:       append([]Item(nil), a.items...),
		Pending:     a.pendingTool(),
		Usage:       u,
		FirstPrompt: a.firstPrompt,
		Prompts:     a.prompts,
	}
}

// Summarize describes a tool call in a few words: the tool name plus its most telling
// argument (a file's base name, a shortened command, or a search pattern).
func Summarize(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	arg := func(k string) string {
		s, _ := in[k].(string)
		return s
	}
	var detail string
	switch name {
	case "Read", "Edit", "Write", "MultiEdit":
		if p := arg("file_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "NotebookEdit":
		if p := arg("notebook_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "Bash":
		detail = strings.Join(strings.Fields(arg("command")), " ")
		if r := []rune(detail); len(r) > 60 {
			detail = string(r[:60]) + "…"
		}
	case "Grep", "Glob":
		detail = arg("pattern")
	}
	if detail == "" {
		return name
	}
	return name + " " + detail
}
