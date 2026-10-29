package board

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

func stripAll(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func TestReplyMarkdownIsStyled(t *testing.T) {
	it := transcript.Item{Kind: transcript.ItemText,
		Text: "**Done.** Ran `go test` again.\n\n\n## Next steps\n- fix the *bug*\n  - nested\n```go\n  return nil\n```"}
	lines := itemLines(it, 60)
	want := []string{
		" claude: Done. Ran go test again.",
		"",
		"   Next steps",
		"   • fix the *bug*",
		"     • nested",
		"     return nil",
	}
	if got := stripAll(lines); !reflect.DeepEqual(got, want) {
		t.Errorf("lines:\n%q\nwant:\n%q", got, want)
	}
	raw := strings.Join(lines, "\n")
	for _, s := range []string{styleClaude.Render("claude:"), styleBold.Render("Done."), styleCode.Render("go"),
		styleCode.Render("test"), styleBold.Render("Next"), styleFaint.Render("return")} {
		if !strings.Contains(raw, s) {
			t.Errorf("reply is missing the styled %q", ansi.Strip(s))
		}
	}
}

func TestPromptsAreNotReadAsMarkdown(t *testing.T) {
	lines := itemLines(transcript.Item{Kind: transcript.ItemPrompt, Text: "rename **kwargs to `opts`"}, 60)
	if got := stripAll(lines); !reflect.DeepEqual(got, []string{" you: rename **kwargs to `opts`"}) {
		t.Errorf("lines = %q", got)
	}
	if !strings.Contains(lines[0], styleYou.Render("you:")) {
		t.Error("you: label isn't styled")
	}
}

func TestWrappedTextKeepsItsIndentAndWidth(t *testing.T) {
	text := strings.Repeat("x", 80) + " 修复结账页面的错误修复结账页面的错误 " + strings.Repeat("word ", 100)
	for _, kind := range []transcript.ItemKind{transcript.ItemPrompt, transcript.ItemText} {
		for _, w := range []int{20, 37, 60} {
			lines := itemLines(transcript.Item{Kind: kind, Text: text}, w)
			if len(lines) > maxItemLines {
				t.Errorf("width %d: %d lines, want at most %d", w, len(lines), maxItemLines)
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > w {
					t.Errorf("width %d: line %d is %d cells: %q", w, i, ansi.StringWidth(l), ansi.Strip(l))
				}
				if i > 0 && !strings.HasPrefix(ansi.Strip(l), "   ") {
					t.Errorf("width %d: line %d isn't indented under its label: %q", w, i, ansi.Strip(l))
				}
			}
			if last := ansi.Strip(lines[len(lines)-1]); last != "   …" {
				t.Errorf("width %d: a cut item should end in an ellipsis line, got %q", w, last)
			}
		}
	}
}

func TestSpacingInsideALineIsKept(t *testing.T) {
	for _, tc := range []struct {
		name string
		it   transcript.Item
		want []string
	}{
		{"code block", transcript.Item{Kind: transcript.ItemText,
			Text: "Set:\n```go\nx := 1      // one\nlonger := 2 // two\n  if a    &&  b {\n```"}, []string{
			" claude: Set:",
			"   x := 1      // one",
			"   longer := 2 // two",
			"     if a    &&  b {",
		}},
		{"diagram", transcript.Item{Kind: transcript.ItemText,
			Text: "Flow:\n┌──────────┐      ┌──────────┐\n│  board   │ ───▶ │  claude  │\n└──────────┘      └──────────┘"}, []string{
			" claude: Flow:",
			"   ┌──────────┐      ┌──────────┐",
			"   │  board   │ ───▶ │  claude  │",
			"   └──────────┘      └──────────┘",
		}},
		{"table", transcript.Item{Kind: transcript.ItemText,
			Text: "| Option | Pros       |\n|--------|------------|\n| A      | fast       |"}, []string{
			" claude: | Option | Pros       |",
			"   |--------|------------|",
			"   | A      | fast       |",
		}},
		{"styled", transcript.Item{Kind: transcript.ItemText, Text: "**Done.**   Ran `go test`   twice"}, []string{
			" claude: Done.   Ran go test   twice",
		}},
		{"prompt", transcript.Item{Kind: transcript.ItemPrompt, Text: "why does  a    b  differ"}, []string{
			" you: why does  a    b  differ",
		}},
	} {
		if got := stripAll(itemLines(tc.it, 60)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n%q\nwant:\n%q", tc.name, got, tc.want)
		}
	}
}

func TestLongSpaceRunBreaksTheLine(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{"a" + strings.Repeat(" ", 9) + "b", []string{" claude: a         b"}},
		{"a" + strings.Repeat(" ", 10) + "b", []string{" claude: a", "   b"}},
		{"a" + strings.Repeat(" ", 40) + "b", []string{" claude: a", "   b"}},
		{"```\nab" + strings.Repeat(" ", 40) + "cd\n```", []string{" claude: ab", "   cd"}},
	} {
		lines := itemLines(transcript.Item{Kind: transcript.ItemText, Text: tc.text}, 20)
		if got := stripAll(lines); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q:\n%q\nwant:\n%q", tc.text, got, tc.want)
		}
		for i, l := range lines {
			if ansi.StringWidth(l) > 20 {
				t.Errorf("%q: line %d is %d cells: %q", tc.text, i, ansi.StringWidth(l), ansi.Strip(l))
			}
		}
	}
}

func TestBodyCollapsesRepeatedToolCalls(t *testing.T) {
	items := []transcript.Item{
		{Kind: transcript.ItemPrompt, Text: "make a deck"},
		{Kind: transcript.ItemTool, Tool: "Artifact"},
		{Kind: transcript.ItemTool, Tool: "Artifact"},
		{Kind: transcript.ItemTool, Tool: "Artifact"},
		{Kind: transcript.ItemTool, Tool: "Read", Text: "a.txt"},
		{Kind: transcript.ItemText, Text: "Done."},
		{Kind: transcript.ItemPrompt, Text: "thanks"},
	}
	want := []string{
		" you: make a deck",
		"   › Artifact ×3",
		"   › Read  a.txt",
		" claude: Done.",
		"",
		" you: thanks",
	}
	if got := stripAll(bodyLines(items, 60)); !reflect.DeepEqual(got, want) {
		t.Errorf("body:\n%q\nwant:\n%q", got, want)
	}
}

func TestToolLineFitsItsWidth(t *testing.T) {
	it := transcript.Item{Kind: transcript.ItemTool, Tool: "Bash", Text: strings.Repeat("y", 200)}
	for _, w := range []int{6, 8, 20, 40} {
		l := toolLine(it, 2, w)
		if ansi.StringWidth(l) > w {
			t.Errorf("width %d: %d cells: %q", w, ansi.StringWidth(l), ansi.Strip(l))
		}
	}
	l := toolLine(it, 2, 40)
	if p := ansi.Strip(l); !strings.HasPrefix(p, "   › Bash  yyy") || !strings.HasSuffix(p, "… ×2") {
		t.Errorf("tool line = %q", p)
	}
	if !strings.Contains(l, styleTool.Render("Bash")) {
		t.Error("tool name isn't styled")
	}
}
