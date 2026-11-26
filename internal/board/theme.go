package board

import (
	"image/color"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
)

// palette is the board's colours for one kind of terminal background. Text with no
// colour of its own keeps the terminal's foreground. Bubble Tea turns the colours
// into the nearest ones a terminal without 24-bit colour has.
type palette struct {
	accent  color.Color // claudeboard's orange: the name, the cursor, Claude's replies
	onBadge color.Color // text on the accent colour
	muted   color.Color // secondary text: folders, token counts, hints
	subtle  color.Color // borders, tree lines, separators
	selBg   color.Color // the selected row's bar
	selFg   color.Color // text on the bar that has no colour of its own
	working color.Color
	needs   color.Color
	done    color.Color
	unread  color.Color
	you     color.Color
	tool    color.Color
	code    color.Color
}

var (
	darkPalette = palette{
		accent:  lipgloss.Color("#D97757"),
		onBadge: lipgloss.Color("#1B1D23"),
		muted:   lipgloss.Color("#8A909A"),
		subtle:  lipgloss.Color("#454B56"),
		selBg:   lipgloss.Color("#2C313B"),
		selFg:   lipgloss.Color("#ECEEF1"),
		working: lipgloss.Color("#7FCF8E"),
		needs:   lipgloss.Color("#F0B94D"),
		done:    lipgloss.Color("#6E9EEB"),
		unread:  lipgloss.Color("#5CC8E0"),
		you:     lipgloss.Color("#6E9EEB"),
		tool:    lipgloss.Color("#B69CF0"),
		code:    lipgloss.Color("#7FD1C4"),
	}
	lightPalette = palette{
		accent:  lipgloss.Color("#C15F3C"),
		onBadge: lipgloss.Color("#FFFFFF"),
		muted:   lipgloss.Color("#6C737E"),
		subtle:  lipgloss.Color("#C5CAD3"),
		selBg:   lipgloss.Color("#E6E9EF"),
		selFg:   lipgloss.Color("#1F2328"),
		working: lipgloss.Color("#2F9A4E"),
		needs:   lipgloss.Color("#B57A0B"),
		done:    lipgloss.Color("#3567C9"),
		unread:  lipgloss.Color("#0B8DAA"),
		you:     lipgloss.Color("#3567C9"),
		tool:    lipgloss.Color("#7A55D3"),
		code:    lipgloss.Color("#12877A"),
	}
)

// The styles everything is drawn with. useTheme sets them; it runs before the board
// starts and from Update, never while a screen is being drawn.
var (
	pal          palette
	dark         bool
	styleWorking lipgloss.Style
	styleNeeds   lipgloss.Style
	styleDone    lipgloss.Style
	styleUnread  lipgloss.Style
	styleMuted   lipgloss.Style
	styleSubtle  lipgloss.Style
	styleBold    lipgloss.Style
	styleAccent  lipgloss.Style
	styleBadge   lipgloss.Style
	styleKey     lipgloss.Style
	styleYou     lipgloss.Style
	styleClaude  lipgloss.Style
	styleTool    lipgloss.Style
	styleCode    lipgloss.Style
	// selOn starts the selected row's bar; it's repeated after every reset inside it.
	selOn string
)

func init() { useTheme(true) }

// useTheme switches every style to the palette for a dark or a light background.
func useTheme(isDark bool) {
	pal, dark = lightPalette, isDark
	if dark {
		pal = darkPalette
	}
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	styleWorking = fg(pal.working)
	styleNeeds = fg(pal.needs).Bold(true)
	styleDone = fg(pal.done)
	styleUnread = fg(pal.unread).Bold(true)
	styleMuted = fg(pal.muted)
	styleSubtle = fg(pal.subtle)
	styleBold = lipgloss.NewStyle().Bold(true)
	styleAccent = fg(pal.accent).Bold(true)
	styleBadge = fg(pal.onBadge).Background(pal.accent).Bold(true).Padding(0, 1)
	styleKey = lipgloss.NewStyle().Bold(true)
	styleYou = fg(pal.you).Bold(true)
	styleClaude = fg(pal.accent).Bold(true)
	styleTool = fg(pal.tool)
	styleCode = fg(pal.code)
	selOn = ansi.NewStyle().BackgroundColor(pal.selBg).ForegroundColor(pal.selFg).String()
}

// highlight draws line, which is exactly one row wide, as the selected row: a bar in
// the selection colour with a mark at its left edge, replacing line's first cell. The
// mark is in the accent colour when active, and keeps the row findable where colours
// are turned off (NO_COLOR).
func highlight(line string, active bool) string {
	rest := ansi.TruncateLeft(line, 1, "")
	rest = strings.ReplaceAll(rest, ansi.ResetStyle, ansi.ResetStyle+selOn)
	c := pal.muted
	if active {
		c = pal.accent
	}
	mark := ansi.NewStyle().BackgroundColor(pal.selBg).ForegroundColor(c).Bold().String()
	return mark + "▌" + ansi.ResetStyle + selOn + rest + ansi.ResetStyle
}

// newInput is a text box in the board's colours.
func newInput() textinput.Model {
	in := textinput.New()
	in.Prompt = "› "
	in.SetStyles(inputStyles())
	return in
}

func inputStyles() textinput.Styles {
	s := textinput.DefaultStyles(dark)
	s.Focused.Prompt = styleAccent
	s.Blurred.Prompt = styleMuted
	s.Cursor.Color = pal.accent
	return s
}

// spinner is the icon of a working session or subagent, one frame per spinEvery.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinEvery = 100 * time.Millisecond

// spinMsg moves the spinner on a frame.
type spinMsg struct{}

// startSpinner keeps the spinner going while a row on the board is working. It ticks
// only then, so an idle board doesn't redraw ten times a second.
func (m *Model) startSpinner() tea.Cmd {
	if m.spinning || !slices.ContainsFunc(m.rows, func(r Row) bool { return r.Status.Kind == status.Working }) {
		return nil
	}
	m.spinning = true
	return tea.Tick(spinEvery, func(time.Time) tea.Msg { return spinMsg{} })
}

// statusIcon is a status's icon in its colour; working ones show spinner frame frame.
func statusIcon(s status.Status, frame int) string {
	switch s.Kind {
	case status.Working:
		return styleWorking.Render(spinner[frame%len(spinner)])
	case status.NeedsYou:
		return styleNeeds.Render(s.Icon())
	case status.Done:
		return styleDone.Render(s.Icon())
	}
	return styleMuted.Render(s.Icon())
}
