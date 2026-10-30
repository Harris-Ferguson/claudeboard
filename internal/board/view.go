package board

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

var (
	styleWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleNeeds   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleFaint   = lipgloss.NewStyle().Faint(true)
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleUnread  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

const (
	defaultHelp     = "↑↓ move  enter open (ctrl+z back)  tab next waiting  n new  s quick  ? help  q quit"
	shortHelp       = "↑↓  enter open (ctrl+z back)  n new  ? help  q quit"
	maxItemLines    = 8 // a long prompt or reply takes at most this many preview lines
	minPreviewWidth = 20
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render draws the screen: exactly m.height lines of exactly m.width cells.
func (m Model) render() string {
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		return "loading…"
	}
	if w < 30 || h < 6 {
		return "claudeboard: terminal too small"
	}
	bh := m.bodyHeight()
	lw, rw := w-2, 0
	wide := m.previewVisible() && m.previewFits()
	if wide {
		lw = clamp(w*2/5, 28, 50)
		rw = w - lw - 3
	}
	var left, right []string
	if !wide && m.panelMode() {
		left = m.panelLines(lw, bh)
	} else {
		left = m.leftLines(lw, bh)
	}
	if wide {
		right = m.panelLines(rw, bh)
	}
	lines := make([]string, 0, h)
	lines = append(lines, topBorder(m.header(), w))
	for i := 0; i < bh; i++ {
		line := "│" + pad(at(left, i), lw) + "│"
		if wide {
			line += pad(at(right, i), rw) + "│"
		}
		lines = append(lines, line)
	}
	if wide {
		lines = append(lines, "├"+strings.Repeat("─", lw)+"┴"+strings.Repeat("─", rw)+"┤")
	} else {
		lines = append(lines, "├"+strings.Repeat("─", lw)+"┤")
	}
	lines = append(lines, "│"+pad(" "+m.footer(w-3), w-2)+"│", "└"+strings.Repeat("─", w-2)+"┘")
	return strings.Join(lines, "\n")
}

// previewFits reports whether the screen is wide enough for the preview pane.
func (m Model) previewFits() bool { return m.width-clamp(m.width*2/5, 28, 50)-3 >= minPreviewWidth }

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func topBorder(title string, w int) string {
	title = truncate(title, w-4)
	return "┌ " + title + " " + strings.Repeat("─", max(0, w-4-ansi.StringWidth(title))) + "┐"
}

func (m Model) header() string {
	parts := []string{"claudeboard"}
	if m.counts.Working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", m.counts.Working))
	}
	if m.counts.NeedsYou > 0 {
		parts = append(parts, styleNeeds.Render(fmt.Sprintf("%d needs you", m.counts.NeedsYou)))
	}
	parts = append(parts, formatTokens(m.counts.Tokens)+" tokens")
	switch {
	case m.lastOK.IsZero() && m.listErr != nil:
		parts = append(parts, styleNeeds.Render("status unavailable"))
	case !m.lastOK.IsZero():
		if age := m.deps.Now().Sub(m.lastOK); age > staleAfter {
			parts = append(parts, styleNeeds.Render(fmt.Sprintf("status stale (%ds)", int(age.Seconds()))))
		}
	}
	return strings.Join(parts, " · ")
}

// footer is the bottom line: an open question, the latest message, or key help that
// fits in width.
func (m Model) footer(width int) string {
	switch m.mode {
	case modeConfirmDelete:
		return styleNeeds.Render("Delete " + m.pending.Name() + " (and its worktree, if any)? y/N")
	case modeAskWorktree:
		return styleNeeds.Render(repoLabel(m.flow.req.Repo) + " already has a session running. Use a git worktree? y/N")
	case modeAskTrust:
		return styleNeeds.Render("Folder not trusted by Claude yet. Open claude to accept, then /exit? y/N  " + tildePath(m.flow.req.Dir))
	}
	if m.message != "" {
		// Messages can carry Claude's multi-line output; the footer is one line.
		return strings.Join(strings.Fields(m.message), " ")
	}
	if ansi.StringWidth(defaultHelp) > width {
		return styleFaint.Render(shortHelp)
	}
	return styleFaint.Render(defaultHelp)
}

// panelMode reports whether the right-hand panel shows something other than the
// preview (and so takes over the whole body on a narrow screen).
func (m Model) panelMode() bool {
	switch m.mode {
	case modeHelp, modePickWorkflow, modeNameWorkflow, modePickRepo:
		return true
	}
	return false
}

func (m Model) leftLines(width, height int) []string {
	if len(m.rows) == 0 {
		return []string{"", " No sessions yet.", "", " n  start a session", " s  quick one-off in the scratch folder"}
	}
	lines := make([]string, 0, height)
	for i := m.listTop; i < len(m.rows) && len(lines) < height; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.cursor, width))
	}
	return lines
}

func (m Model) rowLine(r Row, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = styleBold.Render("▸") + " "
	}
	switch r.Kind {
	case RowWorkflow:
		name := styleBold.Render(r.Workflow.Name)
		if r.Stats.NeedsYou > 0 {
			name += "  " + styleNeeds.Render("!")
		}
		if r.Stats.Unread > 0 {
			name += "  " + styleUnread.Render("•")
		}
		if r.Collapsed {
			name += "  [+]"
		}
		if r.Workflow.Archived {
			name += "  " + styleFaint.Render("archived")
		}
		return fit(marker+name, tokensOrBlank(r.Tokens), width)
	case RowOtherHeader:
		return styleFaint.Render("  Other sessions")
	case RowSession:
		return fit("  "+marker+statusIcon(r.Status)+" "+repoLabel(r.Session.Repo)+labelPart(r), tokensOrBlank(r.Tokens), width)
	case RowAgent:
		return fit("  "+marker+styleFaint.Render(r.Tree)+statusIcon(r.Status)+" "+r.Name(), tokensOrBlank(r.Tokens), width)
	case RowAgentsFolded:
		return fit("  "+marker+styleFaint.Render(r.Tree)+agentsFoldedLabel(r), tokensOrBlank(r.Tokens), width)
	}
	return fit("  "+marker+statusIcon(r.Status)+" "+r.Name()+labelPart(r), tokensOrBlank(r.Tokens), width)
}

// labelPart is a session row's label, after a "•" and in bold when it's unread.
func labelPart(r Row) string {
	if r.Unread {
		return " " + styleUnread.Render("•") + " " + styleBold.Render(sessionLabel(r.Snap))
	}
	return "  " + sessionLabel(r.Snap)
}

func statusIcon(s status.Status) string {
	switch s.Kind {
	case status.Working:
		return styleWorking.Render(s.Icon())
	case status.NeedsYou:
		return styleNeeds.Render(s.Icon())
	case status.Done:
		return styleDone.Render(s.Icon())
	}
	return styleFaint.Render(s.Icon())
}

// panelLines is the right-hand panel: a picker, help, or the selected row's preview.
func (m Model) panelLines(width, height int) []string {
	switch m.mode {
	case modeHelp:
		return helpLines()
	case modePickWorkflow:
		if m.flow.move != nil {
			return m.pickerLines("Move " + m.flow.move.Name() + " into: pick a workflow")
		}
		return m.pickerLines("New session: pick a workflow")
	case modePickRepo:
		return m.pickerLines("New " + m.flow.req.Workflow + " session: pick a folder")
	case modeNameWorkflow:
		return []string{" " + styleBold.Render("New workflow name"), "", " " + m.flow.input.View(), "",
			styleFaint.Render(" enter continue · esc cancel")}
	}
	r, ok := m.selected()
	if !ok {
		return []string{"", " Press n to start a session, or s for a quick one-off."}
	}
	return m.previewLines(r, width, height)
}

func (m Model) pickerLines(title string) []string {
	lines := []string{" " + styleBold.Render(title), ""}
	for i, c := range m.flow.choices {
		marker := "  "
		if i == m.flow.cursor {
			marker = styleBold.Render("▸") + " "
		}
		lines = append(lines, " "+marker+c.label)
	}
	if m.mode == modePickRepo && len(m.deps.Cfg.Repos) == 0 {
		lines = append(lines, "", styleFaint.Render(" add your repos to "+tildePath(m.deps.Cfg.Path)+" to list them here"))
	}
	return append(lines, "", styleFaint.Render(" ↑↓ choose · enter select · esc cancel"))
}

func helpLines() []string {
	keys := [][2]string{
		{"↑↓ / j k", "move"},
		{"enter", "open session (ctrl+z back) / fold"},
		{"tab", "next session that needs you"},
		{"n / N", "new session / at once, random name"},
		{"s", "quick one-off in the scratch folder"},
		{"x / r", "stop / resume (keeps the conversation)"},
		{"D", "delete session and its worktree"},
		{"m", "move an Other session into a workflow"},
		{"a / A", "archive workflow / show archived"},
		{"p", "preview on/off · pgup pgdn scroll"},
		{"ctrl+l", "redraw the screen"},
		{"q", "quit (sessions keep running)"},
	}
	lines := []string{" " + styleBold.Render("Keys"), ""}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf(" %-10s %s", k[0], k[1]))
	}
	return append(lines, "", styleFaint.Render(" press any key to close"))
}

func (m Model) previewLines(r Row, width, height int) []string {
	switch r.Kind {
	case RowWorkflow:
		return m.workflowLines(r)
	case RowOtherHeader:
		return nil
	case RowAgent:
		return m.agentLines(r, width, height)
	case RowAgentsFolded:
		return finishedAgentLines(r)
	}
	title := " " + styleBold.Render(r.Name())
	switch {
	case r.Kind == RowSession:
		where := repoLabel(r.Session.Repo)
		if r.Session.Worktree != "" {
			where += " (worktree " + r.Session.Worktree + ")"
		}
		title += " · " + where
	case r.Interactive():
		title += " · running in another terminal"
	}
	title += " · " + formatTokens(r.Tokens) + " tokens"
	usage := " " + usageText(r.Snap.Usage)
	if n := len(r.Snap.Agents); n > 0 {
		usage += " · " + plural(n, "subagent")
	}
	state := " " + statusIcon(r.Status) + " " + r.Status.Text()
	if p := r.Snap.Pending; p != "" && (r.Status.Kind == status.NeedsYou || r.Status.Kind == status.Working) {
		state += ": " + p
	}
	if r.Kind == RowOther {
		state += styleFaint.Render(" · m moves it into a workflow")
	}
	head := []string{title, styleFaint.Render(usage), state, ""}

	var body []string
	if !r.Snap.Found {
		body = []string{styleFaint.Render(" no messages yet")}
	}
	body = append(body, bodyLines(r.Snap.Items, width-1, "you:")...)
	return m.scrolled(head, body, height)
}

// agentLines is a subagent's preview: what it's doing and its own conversation, which
// starts with the task its session gave it.
func (m Model) agentLines(r Row, width, height int) []string {
	a := r.Agent
	title := " " + styleBold.Render(r.Name()) + " · subagent of " + r.Owner.Name() + " · " + formatTokens(r.Tokens) + " tokens"
	usage := " " + usageText(a.Usage)
	if a.Type != "" {
		usage = " " + a.Type + " ·" + usage
	}
	state := " " + statusIcon(r.Status) + " finished"
	if r.Status.Kind == status.Working {
		state = " " + statusIcon(r.Status) + " working"
		if a.Pending != "" {
			state += ": " + a.Pending
		}
	}
	state += styleFaint.Render(" · enter opens " + r.Owner.Name())
	head := []string{title, styleFaint.Render(usage), state, ""}
	return m.scrolled(head, bodyLines(a.Items, width-1, "task:"), height)
}

// finishedAgentLines is the preview of a session's finished-subagents line: a list.
func finishedAgentLines(r Row) []string {
	lines := []string{
		" " + styleBold.Render(r.Owner.Name()) + " · " + plural(r.Finished, "finished subagent") + " · " + formatTokens(r.Tokens) + " tokens",
		"",
	}
	for _, a := range r.Owner.Snap.Agents {
		if !agentRunning(*r.Owner, a) {
			lines = append(lines, " "+statusIcon(r.Status)+" "+agentLabel(a)+styleFaint.Render(" · "+formatTokens(a.Usage.Fresh())+" tokens"))
		}
	}
	hint := " enter shows them, to read what each did"
	if !r.Collapsed {
		hint = " enter hides them"
	}
	return append(lines, "", styleFaint.Render(hint))
}

func usageText(u transcript.Usage) string {
	return fmt.Sprintf("in %s · out %s · cache r %s · w %s",
		formatTokens(u.Input), formatTokens(u.Output), formatTokens(u.CacheRead), formatTokens(u.CacheWrite))
}

// scrolled fits head and the end of body into height lines, scrolled back by the
// user's PgUp.
func (m Model) scrolled(head, body []string, height int) []string {
	avail := height - len(head)
	if avail <= 0 {
		return head[:max(0, height)]
	}
	scroll := clamp(m.previewScroll, 0, max(0, len(body)-avail))
	start := max(0, len(body)-avail-scroll)
	return append(head, body[start:min(len(body), start+avail)]...)
}

func (m Model) workflowLines(r Row) []string {
	lines := []string{
		" " + styleBold.Render(r.Workflow.Name) + " · " + plural(r.Sessions, "session") + " · " + formatTokens(r.Tokens) + " tokens",
		fmt.Sprintf(" %d working · %d need you", r.Stats.Working, r.Stats.NeedsYou),
	}
	if r.Workflow.LastRepo != "" {
		lines = append(lines, " last folder: "+repoLabel(r.Workflow.LastRepo))
	}
	return append(lines, "", styleFaint.Render(" enter fold/unfold · n new session here · a archive"))
}
