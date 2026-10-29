package board

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

var (
	styleYou    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	styleClaude = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
	styleTool   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// indent starts every preview line after an item's first, so the "you:" and "claude:"
// labels stand out.
const indent = "   "

var (
	heading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	bullet  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
)

// bodyLines draws the conversation: a blank line before each prompt, and a run of the
// same tool call shown once with a count.
func bodyLines(items []transcript.Item, width int) []string {
	var out []string
	for i := 0; i < len(items); i++ {
		it := items[i]
		if it.Kind == transcript.ItemTool {
			n := 1
			for i+n < len(items) && items[i+n] == it {
				n++
			}
			out = append(out, toolLine(it, n, width))
			i += n - 1
			continue
		}
		if it.Kind == transcript.ItemPrompt && len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, itemLines(it, width)...)
	}
	return out
}

// toolLine is one tool call on one line, its argument cut to fit: "› Bash  npm test ×2".
// The "›" keeps tool calls apart from bullet points in Claude's replies.
func toolLine(it transcript.Item, repeat, width int) string {
	lead := indent + "› "
	suffix := ""
	if repeat > 1 {
		suffix = fmt.Sprintf(" ×%d", repeat)
	}
	room := width - ansi.StringWidth(lead) - ansi.StringWidth(suffix)
	if room < 0 {
		return truncate(lead+it.Tool, width)
	}
	name := truncate(it.Tool, room)
	line := indent + styleFaint.Render("›") + " " + styleTool.Render(name)
	if left := room - ansi.StringWidth(name) - 2; left > 0 {
		if detail := firstLine(clean(it.Text)); detail != "" {
			line += "  " + styleFaint.Render(truncate(detail, left))
		}
	}
	return line + styleFaint.Render(suffix)
}

// itemLines draws a prompt or a reply: its label, then the text wrapped to width under
// a hanging indent, at most maxItemLines lines. Replies get light markdown: **bold**,
// `code`, # headings, - bullets and ``` code blocks.
func itemLines(it transcript.Item, width int) []string {
	label, markdown := " "+styleYou.Render("you:")+" ", false
	if it.Kind == transcript.ItemText {
		label, markdown = " "+styleClaude.Render("claude:")+" ", true
	}
	var out []string
	lead, inCode, gap := label, false, false
	for _, line := range strings.Split(clean(it.Text), "\n") {
		trimmed := strings.TrimSpace(line)
		if markdown && strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if trimmed == "" {
			gap = len(out) > 0
			continue
		}
		if gap {
			out, gap = append(out, ""), false
		}
		first, rest := lead, indent
		var spans []span
		switch m := bullet.FindStringSubmatch(line); {
		case !markdown:
			spans = []span{{text: line}}
		case inCode:
			pad := strings.Repeat(" ", len(line)-len(strings.TrimLeft(line, " ")))
			first, rest = first+pad, rest+pad
			spans = []span{{text: line, style: &styleFaint}}
		case heading.MatchString(trimmed):
			spans = inlineSpans(heading.FindStringSubmatch(trimmed)[1], &styleBold)
		case m != nil:
			first, rest = first+m[1]+"• ", rest+m[1]+"  "
			spans = inlineSpans(m[2], nil)
		default:
			spans = inlineSpans(line, nil)
		}
		out = append(out, wrapSpans(spans, width, first, rest)...)
		lead = indent
	}
	if len(out) == 0 {
		out = []string{label}
	}
	if len(out) > maxItemLines {
		out = append(out[:maxItemLines-1], indent+styleFaint.Render("…"))
	}
	return out
}

// span is a run of text in one style; a nil style is plain.
type span struct {
	text  string
	style *lipgloss.Style
}

// inlineSpans splits a line of markdown into **bold**, `code` and plain spans. Plain
// text gets base. Markers without a closing partner are kept as text.
func inlineSpans(line string, base *lipgloss.Style) []span {
	var out []span
	for line != "" {
		i := strings.IndexAny(line, "*`")
		if i < 0 {
			break
		}
		if line[i] == '`' {
			if j := strings.IndexByte(line[i+1:], '`'); j > 0 {
				out = append(out, span{line[:i], base}, span{line[i+1 : i+1+j], &styleCode})
				line = line[i+2+j:]
				continue
			}
		} else if strings.HasPrefix(line[i:], "**") {
			if j := strings.Index(line[i+2:], "**"); j > 0 {
				out = append(out, span{line[:i], base}, span{line[i+2 : i+2+j], &styleBold})
				line = line[i+4+j:]
				continue
			}
		}
		out = append(out, span{line[:i+1], base})
		line = line[i+1:]
	}
	return append(out, span{line, base})
}

// token is a word of a span. Tokens with no space before them are glued to the
// previous one: a line only breaks between words.
type token struct {
	text  string
	style *lipgloss.Style
	space bool
}

func tokenize(spans []span) []token {
	var out []token
	space := false
	for _, sp := range spans {
		var b strings.Builder
		flush := func() {
			if b.Len() > 0 {
				out = append(out, token{b.String(), sp.style, space})
				space = false
				b.Reset()
			}
		}
		for _, r := range sp.text {
			if unicode.IsSpace(r) {
				flush()
				space = true
				continue
			}
			b.WriteRune(r)
		}
		flush()
	}
	return out
}

func (t token) render(text string) string {
	if t.style == nil {
		return text
	}
	return t.style.Render(text)
}

// wrapSpans lays spans out in lines of at most width cells, the first starting with
// first and the rest with rest. Each word is styled on its own, so no style runs on
// past the end of a line; a word too long for any line is broken.
func wrapSpans(spans []span, width int, first, rest string) []string {
	var lines []string
	cur, used, empty := first, ansi.StringWidth(first), true
	newline := func() {
		lines = append(lines, cur)
		cur, used, empty = rest, ansi.StringWidth(rest), true
	}
	toks := tokenize(spans)
	for i := 0; i < len(toks); {
		j, w := i+1, ansi.StringWidth(toks[i].text)
		for ; j < len(toks) && !toks[j].space; j++ {
			w += ansi.StringWidth(toks[j].text)
		}
		gap := 1
		if empty {
			gap = 0
		}
		if !empty && used+gap+w > width {
			newline()
			gap = 0
		}
		if empty && used+w > width {
			for _, t := range toks[i:j] {
				for text := t.text; text != ""; {
					cut := ansi.Truncate(text, width-used, "")
					if cut == "" {
						if !empty {
							newline()
							continue
						}
						cut = string([]rune(text)[:1])
					}
					cur, used, empty = cur+t.render(cut), used+ansi.StringWidth(cut), false
					if text = text[len(cut):]; text != "" {
						newline()
					}
				}
			}
		} else {
			cur, used, empty = cur+strings.Repeat(" ", gap), used+gap, false
			for _, t := range toks[i:j] {
				cur, used = cur+t.render(t.text), used+ansi.StringWidth(t.text)
			}
		}
		i = j
	}
	return append(lines, cur)
}
