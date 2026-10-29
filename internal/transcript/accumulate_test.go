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
	a := newAccumulator()
	for _, l := range fixtureLines(t) {
		a.add(l, true)
	}
	s := a.snapshot()
	want := []Item{
		{Kind: ItemPrompt, Text: "wire PaymentForm into checkout\nand run the tests"},
		{Kind: ItemText, Text: "Looking at the checkout page."},
		{Kind: ItemTool, Text: "Edit CheckoutPage.vue"},
		{Kind: ItemText, Text: "Running the tests."},
		{Kind: ItemTool, Text: "Bash npm test"},
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

func TestSubagentLinesOnlyCountTokens(t *testing.T) {
	a := newAccumulator()
	a.add([]byte(`{"type":"user","message":{"content":"sub prompt"}}`), false)
	a.add([]byte(`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"sub"}],"usage":{"output_tokens":100}}}`), false)
	s := a.snapshot()
	if len(s.Items) != 0 || s.Prompts != 0 || s.Usage.Output != 100 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestItemsAreCapped(t *testing.T) {
	a := newAccumulator()
	for i := 0; i < 250; i++ {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":"prompt %d"}}`, i)), true)
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
	long := strings.Repeat("x", 100)
	cases := []struct{ name, input, want string }{
		{"Edit", `{"file_path":"/repo/src/CheckoutPage.vue"}`, "Edit CheckoutPage.vue"},
		{"Read", `{}`, "Read"},
		{"NotebookEdit", `{"notebook_path":"/n/a.ipynb"}`, "NotebookEdit a.ipynb"},
		{"Bash", `{"command":"npm test\n  --watch"}`, "Bash npm test --watch"},
		{"Bash", `{"command":"` + long + `"}`, "Bash " + strings.Repeat("x", 60) + "…"},
		{"Bash", `not json`, "Bash"},
		{"Grep", `{"pattern":"TODO"}`, "Grep TODO"},
		{"Task", `{"prompt":"go"}`, "Task"},
	}
	for _, c := range cases {
		if got := Summarize(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("Summarize(%s, %s) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestPromptsThatStartWithMarkupAreKept(t *testing.T) {
	a := newAccumulator()
	for _, content := range []string{"<?php echo 'hi';", "<template>fix this</template>"} {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":%q}}`, content)), true)
	}
	for _, wrapper := range []string{"<command-name>/model</command-name>", "<local-command-stdout>ok</local-command-stdout>", "<bash-input>ls</bash-input>"} {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":%q}}`, wrapper)), true)
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
