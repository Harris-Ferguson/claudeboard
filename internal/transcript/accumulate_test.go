package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestAccumulatorBuildsPreviewAndUsage(t *testing.T) {
	a := newAccumulator(false)
	for _, l := range fixtureLines(t) {
		a.add(l)
	}
	s := a.snapshot()
	want := []Item{
		{Kind: ItemPrompt, Text: "wire PaymentForm into checkout\nand run the tests"},
		{Kind: ItemText, Text: "Looking at the checkout page."},
		{Kind: ItemTool, Tool: "Edit", Text: "CheckoutPage.vue"},
		{Kind: ItemText, Text: "Running the tests."},
		{Kind: ItemTool, Tool: "Bash", Text: "run tests"},
	}
	if !reflect.DeepEqual(s.Items, want) {
		t.Errorf("Items = %+v", s.Items)
	}
	if s.Pending != "Bash npm test" {
		t.Errorf("Pending = %q (the Edit was answered, the Bash wasn't)", s.Pending)
	}
	if s.Prompts != 1 || s.FirstPrompt != "wire PaymentForm into checkout\nand run the tests" {
		t.Errorf("Prompts = %d, FirstPrompt = %q", s.Prompts, s.FirstPrompt)
	}
	// msg_1 is written twice and must be counted once.
	if want := (Usage{Input: 15, Output: 27, CacheRead: 700, CacheWrite: 40}); s.Usage != want {
		t.Errorf("Usage = %+v, want %+v", s.Usage, want)
	}
	if s.Usage.Total() != 782 || !s.Found {
		t.Errorf("Total = %d, Found = %v", s.Usage.Total(), s.Found)
	}
}

func TestSidechainLinesOnlyCountTokensInASessionFile(t *testing.T) {
	lines := []string{
		`{"type":"user","isSidechain":true,"message":{"content":"sub prompt"}}`,
		`{"type":"assistant","isSidechain":true,"message":{"id":"m","content":[{"type":"text","text":"sub"}],"usage":{"output_tokens":100}}}`,
	}
	main, side := newAccumulator(false), newAccumulator(true)
	for _, l := range lines {
		main.add([]byte(l))
		side.add([]byte(l))
	}
	if s := main.snapshot(); len(s.Items) != 0 || s.Prompts != 0 || s.Usage.Output != 100 {
		t.Errorf("session file: snapshot = %+v", s)
	}
	if s := side.snapshot(); len(s.Items) != 2 || s.Prompts != 1 || s.Usage.Output != 100 {
		t.Errorf("subagent file: snapshot = %+v", s)
	}
}

func TestItemsAreCapped(t *testing.T) {
	a := newAccumulator(false)
	for i := 0; i < 250; i++ {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":"prompt %d"}}`, i)))
	}
	s := a.snapshot()
	if len(s.Items) != maxItems || s.Items[maxItems-1].Text != "prompt 249" || s.Prompts != 250 {
		t.Errorf("len %d, last %q, prompts %d", len(s.Items), s.Items[len(s.Items)-1].Text, s.Prompts)
	}
	if s.FirstPrompt != "prompt 0" {
		t.Errorf("FirstPrompt = %q", s.FirstPrompt)
	}
}

func TestSummarize(t *testing.T) {
	long := strings.Repeat("x", 150)
	cases := []struct{ name, input, want string }{
		{"Edit", `{"file_path":"/repo/src/CheckoutPage.vue"}`, "Edit CheckoutPage.vue"},
		{"Read", `{}`, "Read"},
		{"NotebookEdit", `{"notebook_path":"/n/a.ipynb"}`, "NotebookEdit a.ipynb"},
		{"Bash", `{"command":"npm test\n  --watch"}`, "Bash npm test --watch"},
		{"Bash", `{"command":"` + long + `"}`, "Bash " + strings.Repeat("x", 120) + "…"},
		{"Bash", `not json`, "Bash"},
		// The command is what a permission prompt asks about, so it wins over the
		// description; a leading cd into the session's own folder is noise.
		{"Bash", `{"command":"cd ~/worktrees/web-spec && wc -l docs/ab.md","description":"Count lines"}`, "Bash wc -l docs/ab.md"},
		{"Bash", `{"command":"cd '/a b' ; git status"}`, "Bash git status"},
		{"Bash", `{"command":"cd /repo"}`, "Bash cd /repo"},
		{"Grep", `{"pattern":"TODO"}`, "Grep TODO"},
		{"Task", `{"prompt":"go"}`, "Task"},
		{"Agent", `{"description":"Review the branch","prompt":"long brief"}`, "Agent Review the branch"},
		{"AskUserQuestion", `{"questions":[{"question":"Who is the audience?","header":"Audience"}]}`, "AskUserQuestion Who is the audience?"},
		{"WebFetch", `{"url":"https://example.com/a","prompt":"summarise"}`, "WebFetch https://example.com/a"},
		{"WebSearch", `{"query":"bubble tea v2"}`, "WebSearch bubble tea v2"},
		{"TaskCreate", `{"subject":"Write the spec","description":"long"}`, "TaskCreate Write the spec"},
		{"Skill", `{"skill":"superpowers:brainstorming"}`, "Skill superpowers:brainstorming"},
		// Tools claudeboard doesn't know get their most telling common argument.
		{"Artifact", `{"title":"AB testing deck","content":"<html>"}`, "Artifact AB testing deck"},
		{"Artifact", `{}`, "Artifact"},
		{"mcp__claude-in-chrome__navigate", `{"url":"https://example.com","tabId":3}`, "claude-in-chrome navigate https://example.com"},
	}
	for _, c := range cases {
		if got := Summarize(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("Summarize(%s, %s) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestPreviewToolItemsPreferBashDescriptions(t *testing.T) {
	a := newAccumulator(false)
	a.add([]byte(`{"type":"assistant","message":{"id":"m","content":[` +
		`{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"cd /w && M=origin/master; git show \"$M\":x","description":"Show x on master"}},` +
		`{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"cd /w && ls"}}]}}`))
	want := []Item{
		{Kind: ItemTool, Tool: "Bash", Text: "Show x on master"},
		{Kind: ItemTool, Tool: "Bash", Text: "ls"},
	}
	if s := a.snapshot(); !reflect.DeepEqual(s.Items, want) {
		t.Errorf("Items = %+v, want %+v", s.Items, want)
	}
}

func TestToolSearchIsLeftOut(t *testing.T) {
	a := newAccumulator(false)
	a.add([]byte(`{"type":"assistant","message":{"id":"m","content":[{"type":"tool_use","id":"t1","name":"ToolSearch","input":{"query":"select:Read"}}]}}`))
	if s := a.snapshot(); len(s.Items) != 0 || s.Pending != "" {
		t.Errorf("ToolSearch (Claude loading its own tools) shown: %+v", s)
	}
}

func TestPromptsThatStartWithMarkupAreKept(t *testing.T) {
	a := newAccumulator(false)
	for _, content := range []string{"<?php echo 'hi';", "<template>fix this</template>"} {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":%q}}`, content)))
	}
	for _, wrapper := range []string{"<command-name>/model</command-name>", "<local-command-stdout>ok</local-command-stdout>", "<bash-input>ls</bash-input>"} {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":%q}}`, wrapper)))
	}
	s := a.snapshot()
	if s.Prompts != 2 || s.FirstPrompt != "<?php echo 'hi';" {
		t.Errorf("Prompts = %d, FirstPrompt = %q; pasted code is a real prompt, Claude Code's wrappers aren't", s.Prompts, s.FirstPrompt)
	}
}

func TestFreshLeavesOutCacheReads(t *testing.T) {
	if got := (Usage{Input: 1, Output: 2, CacheRead: 1000, CacheWrite: 4}).Fresh(); got != 7 {
		t.Errorf("Fresh = %d, want 7", got)
	}
}

func TestActivityCountsClaudesRecords(t *testing.T) {
	a := newAccumulator(false)
	a.add([]byte(`{"type":"user","message":{"content":"go"}}`))
	a.add([]byte(`{"type":"assistant","message":{"id":"m1","content":[{"type":"text","text":"on it"}]}}`))
	a.add([]byte(`{"type":"assistant","message":{"id":"m2","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`))
	a.add([]byte(`{"type":"assistant","isSidechain":true,"message":{"id":"s2","content":[{"type":"text","text":"side"}]}}`))
	if got := a.snapshot().Activity; got != 2 {
		t.Errorf("Activity = %d, want 2 (Claude's records in the main conversation only)", got)
	}
}
