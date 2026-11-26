package board

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// footerKeys is the key help along the bottom; shortKeys is what fits on a narrow
// screen. Each is a key and what it does.
var (
	footerKeys = [][2]string{{"↑↓", "move"}, {"enter", "open (ctrl+z back)"}, {"tab", "next waiting"},
		{"n", "new"}, {"s", "quick"}, {"?", "help"}, {"q", "quit"}}
	shortKeys = [][2]string{{"↑↓", ""}, {"enter", "open (ctrl+z back)"}, {"n", "new"}, {"?", "help"}, {"q", "quit"}}
)

const (
	maxItemLines    = 8 // a long prompt or reply takes at most this many preview lines
	minPreviewWidth = 20
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render draws the screen: exactly m.height lines of exactly m.width cells. The
// header, then the list and the preview side by side in boxes, then the footer.
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
		rw = w - lw - 4
	}
	panel := m.panelMode()
	var boxes [][]string
	if !wide && panel {
		boxes = append(boxes, box(m.panelTitle(), m.panelLines(lw, bh), lw, bh, true))
	} else {
		boxes = append(boxes, box("Sessions", m.leftLines(lw, bh), lw, bh, false))
	}
	if wide {
		boxes = append(boxes, box(m.panelTitle(), m.panelLines(rw, bh), rw, bh, panel))
	}
	lines := make([]string, 0, h)
	lines = append(lines, m.header(w))
	for i := 0; i < bh+2; i++ {
		line := ""
		for _, b := range boxes {
			line += b[i]
		}
		lines = append(lines, line)
	}
	lines = append(lines, m.footer(w))
	return strings.Join(lines, "\n")
}

// previewFits reports whether the screen is wide enough for the preview pane.
func (m Model) previewFits() bool { return m.width-clamp(m.width*2/5, 28, 50)-4 >= minPreviewWidth }

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// box frames lines in a rounded border, w cells wide and h lines high inside, with
// title set into the top edge, cut to leave at least one ─ before the corner. A box
// asking for something (a picker, a name, the help) is in the accent colour.
func box(title string, lines []string, w, h int, active bool) []string {
	edge, head := styleSubtle, styleMuted.Bold(true)
	if active {
		edge, head = styleAccent.UnsetBold(), styleAccent
	}
	top := edge.Render("╭" + strings.Repeat("─", w) + "╮")
	if title = truncate(clean(title), w-4); title != "" {
		top = edge.Render("╭─ ") + head.Render(title) + " " + edge.Render(strings.Repeat("─", w-3-ansi.StringWidth(title))+"╮")
	}
	out := make([]string, 0, h+2)
	out = append(out, top)
	side := edge.Render("│")
	for i := 0; i < h; i++ {
		out = append(out, side+pad(at(lines, i), w)+side)
	}
	return append(out, edge.Render("╰"+strings.Repeat("─", w)+"╯"))
}

// header is the top line: claudeboard's name, what needs attention, and the tokens.
// A warning that the status is stale or unavailable comes first, so a narrow screen
// cuts the counts rather than the warning.
func (m Model) header(w int) string {
	left := styleBadge.Render("claudeboard")
	var parts []string
	switch {
	case m.lastOK.IsZero() && m.listErr != nil:
		parts = append(parts, styleNeeds.Render("status unavailable"))
	case !m.lastOK.IsZero():
		if age := m.deps.Now().Sub(m.lastOK); age > staleAfter {
			parts = append(parts, styleNeeds.Render(fmt.Sprintf("status stale (%ds)", int(age.Seconds()))))
		}
	}
	if m.counts.Working > 0 {
		parts = append(parts, styleWorking.Render(fmt.Sprintf("%d working", m.counts.Working)))
	}
	if m.counts.NeedsYou > 0 {
		parts = append(parts, styleNeeds.Render(fmt.Sprintf("%d needs you", m.counts.NeedsYou)))
	}
	if m.counts.Unread > 0 {
		parts = append(parts, styleUnread.Render(fmt.Sprintf("%d unread", m.counts.Unread)))
	}
	if len(parts) > 0 {
		left += "  " + strings.Join(parts, styleMuted.Render(" · "))
	}
	return fit(left, styleMuted.Render(formatTokens(m.counts.Tokens)+" tokens"), w)
}

// footer is the bottom line: an open question, the latest message, or key help that
// fits in width.
func (m Model) footer(width int) string {
	switch m.mode {
	case modeConfirmDelete:
		return pad(" "+styleNeeds.Render("Delete "+m.pending.Name()+" (and its worktree, if any)? y/N"), width)
	case modeAskWorktree:
		return pad(" "+styleNeeds.Render(repoLabel(m.flow.req.Repo)+" already has a session running. Use a git worktree? y/N"), width)
	case modeAskTrust:
		return pad(" "+styleNeeds.Render("Folder not trusted by Claude yet. Open claude to accept, then /exit? y/N  "+tildePath(m.flow.req.Dir)), width)
	}
	if m.message != "" {
		// Messages can carry Claude's multi-line output; the footer is one line.
		return pad(" "+strings.Join(strings.Fields(m.message), " "), width)
	}
	help := keyHelp(footerKeys)
	if ansi.StringWidth(help) > width-2 {
		help = keyHelp(shortKeys)
	}
	return pad(" "+help, width)
}

// keyHelp lists keys, each in bold followed by what it does.
func keyHelp(keys [][2]string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = styleKey.Render(k[0])
		if k[1] != "" {
			parts[i] += " " + styleMuted.Render(k[1])
		}
	}
	return strings.Join(parts, styleSubtle.Render(" · "))
}

// panelMode reports whether the right-hand panel shows something other than the
// preview (and so takes over the whole body on a narrow screen).
func (m Model) panelMode() bool {
	switch m.mode {
	case modeHelp, modePickWorkflow, modeNameWorkflow, modePickRepo, modeRename:
		return true
	}
	return false
}

func (m Model) leftLines(width, height int) []string {
	if len(m.rows) == 0 {
		return []string{"", " No sessions yet.", "",
			" " + styleAccent.Render("n") + "  start a session",
			" " + styleAccent.Render("s") + "  quick one-off in the scratch folder"}
	}
	lines := make([]string, 0, height)
	for i := m.listTop; i < len(m.rows) && len(lines) < height; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.cursor && !m.panelMode(), i == m.cursor, width))
	}
	return lines
}

// rowLine is one row of the list, exactly width cells: a workflow, with ▾ when its
// sessions show below it and ▸ when they're folded away, or a session indented under
// it. The selected row is a highlighted bar, with its mark in the accent colour while
// the list has the keys (active).
func (m Model) rowLine(r Row, active, selected bool, width int) string {
	tokens := ""
	if t := tokensOrBlank(r.Tokens); t != "" {
		tokens = styleMuted.Render(t)
	}
	var line string
	switch r.Kind {
	case RowWorkflow:
		fold := "▾"
		if r.Collapsed {
			fold = "▸"
		}
		name := styleMuted.Render(fold) + " " + styleBold.Render(r.Workflow.Name)
		if r.Stats.NeedsYou > 0 {
			name += "  " + styleNeeds.Render("!")
		}
		if r.Stats.Unread > 0 {
			name += "  " + styleUnread.Render("•")
		}
		if r.Workflow.Archived {
			name += "  " + styleMuted.Render("archived")
		}
		line = fit(" "+name, tokens, width)
	case RowOtherHeader:
		return pad(" "+styleMuted.Render("Other sessions"), width)
	case RowSession:
		line = fit("   "+statusIcon(r.Status, m.frame)+" "+styleMuted.Render(repoLabel(r.Session.Repo))+labelPart(r), tokens, width)
	case RowAgent:
		line = fit("   "+styleSubtle.Render(r.Tree)+statusIcon(r.Status, m.frame)+" "+r.Name(), tokens, width)
	case RowAgentsFolded:
		line = fit("   "+styleSubtle.Render(r.Tree)+agentsFoldedLabel(r), tokens, width)
	default:
		line = fit("   "+statusIcon(r.Status, m.frame)+" "+r.Name()+labelPart(r), tokens, width)
	}
	if selected {
		return highlight(line, active)
	}
	return line
}

// labelPart is a session row's label, after a "•" and in bold when it's unread.
func labelPart(r Row) string {
	l := shortLabel(r.label())
	if r.Unread {
		return " " + styleUnread.Render("•") + " " + styleBold.Render(l)
	}
	return "  " + l
}

// panelTitle is the title of the right-hand panel: what it shows.
func (m Model) panelTitle() string {
	switch m.mode {
	case modeHelp:
		return "Keys"
	case modePickWorkflow:
		if m.flow.move != nil {
			return "Move " + m.flow.move.Name() + " into: pick a workflow"
		}
		return "New session: pick a workflow"
	case modePickRepo:
		return "New " + m.flow.req.Workflow + " session: pick a folder"
	case modeNameWorkflow:
		return "New workflow name"
	case modeRename:
		return "Rename " + m.pending.Session.Name
	}
	if r, ok := m.selected(); ok {
		return previewTitle(r)
	}
	return ""
}

// previewTitle is the title of r's preview: a session's label, else the row's name.
func previewTitle(r Row) string {
	if r.Kind == RowSession || r.Kind == RowOther {
		if l := r.label(); l != "" {
			return l
		}
	}
	return r.Name()
}

// panelLines is the right-hand panel: a picker, help, or the selected row's preview.
func (m Model) panelLines(width, height int) []string {
	switch m.mode {
	case modeHelp:
		return helpLines()
	case modePickWorkflow, modePickRepo:
		return m.pickerLines(width)
	case modeNameWorkflow:
		return []string{" " + m.flow.input.View(), "", styleMuted.Render(" enter continue · esc cancel")}
	case modeRename:
		return []string{" " + m.flow.input.View(), "", styleMuted.Render(" enter save · empty = first prompt · esc cancel")}
	}
	r, ok := m.selected()
	if !ok {
		return []string{"", " Press " + styleAccent.Render("n") + " to start a session, or " + styleAccent.Render("s") + " for a quick one-off."}
	}
	return m.previewLines(r, width, height)
}

func (m Model) pickerLines(width int) []string {
	var lines []string
	for i, c := range m.flow.choices {
		line := pad("   "+c.label, width)
		if i == m.flow.cursor {
			line = highlight(line, true)
		}
		lines = append(lines, line)
	}
	if m.mode == modePickRepo && len(m.deps.Cfg.Repos) == 0 {
		lines = append(lines, "", styleMuted.Render(" add your repos to "+tildePath(m.deps.Cfg.Path)+" to list them here"))
	}
	return append(lines, "", styleMuted.Render(" ↑↓ choose · enter select · esc cancel"))
}

func helpLines() []string {
	keys := [][2]string{
		{"↑↓ / j k", "move"},
		{"enter", "open session (ctrl+z back) / fold"},
		{"tab / u", "next session that needs you / unread"},
		{"n / N", "new session / at once, random name"},
		{"s", "quick one-off in the scratch folder"},
		{"x / r", "stop / resume (keeps the conversation)"},
		{"D", "delete session and its worktree"},
		{"m / R", "move to a workflow / rename session"},
		{"a / A", "archive workflow / show archived"},
		{"p", "preview on/off · pgup pgdn scroll"},
		{"ctrl+l", "redraw the screen"},
		{"q", "quit (sessions keep running)"},
	}
	var lines []string
	for _, k := range keys {
		lines = append(lines, " "+styleAccent.Render(fmt.Sprintf("%-10s", k[0]))+" "+k[1])
	}
	return append(lines, "", styleMuted.Render(" press any key to close"))
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
	// The first line starts with the session's name, unless the title already is it
	// (a session with no label or prompt yet).
	var where []string
	name := r.Name()
	if r.Kind == RowSession {
		name = r.Session.Name
	}
	if name != previewTitle(r) {
		where = append(where, name)
	}
	switch {
	case r.Kind == RowSession:
		folder := repoLabel(r.Session.Repo)
		if r.Session.Worktree != "" {
			folder += " (worktree " + r.Session.Worktree + ")"
		}
		where = append(where, folder)
	case r.Interactive():
		where = append(where, "running in another terminal")
	}
	where = append(where, formatTokens(r.Tokens)+" tokens")
	usage := usageText(r.Snap.Usage)
	if n := len(r.Snap.Agents); n > 0 {
		usage += " · " + plural(n, "subagent")
	}
	state := " " + statusIcon(r.Status, m.frame) + " " + r.Status.Text()
	if p := r.Snap.Pending; p != "" && (r.Status.Kind == status.NeedsYou || r.Status.Kind == status.Working) {
		state += ": " + p
	}
	if r.Kind == RowOther {
		state += styleMuted.Render(" · m moves it into a workflow")
	}
	head := []string{styleMuted.Render(" " + strings.Join(where, " · ")), styleMuted.Render(" " + usage), state, ""}

	var body []string
	if !r.Snap.Found {
		body = []string{styleMuted.Render(" no messages yet")}
	}
	body = append(body, bodyLines(r.Snap.Items, width-1, "you:")...)
	return m.scrolled(head, body, height)
}

// agentLines is a subagent's preview: what it's doing and its own conversation, which
// starts with the task its session gave it.
func (m Model) agentLines(r Row, width, height int) []string {
	a := r.Agent
	where := styleMuted.Render(" subagent of " + r.Owner.Name() + " · " + formatTokens(r.Tokens) + " tokens")
	usage := " " + usageText(a.Usage)
	if a.Type != "" {
		usage = " " + a.Type + " ·" + usage
	}
	state := " " + statusIcon(r.Status, m.frame) + " finished"
	if r.Status.Kind == status.Working {
		state = " " + statusIcon(r.Status, m.frame) + " working"
		if a.Pending != "" {
			state += ": " + a.Pending
		}
	}
	state += styleMuted.Render(" · enter opens " + r.Owner.Name())
	head := []string{where, styleMuted.Render(usage), state, ""}
	return m.scrolled(head, bodyLines(a.Items, width-1, "task:"), height)
}

// finishedAgentLines is the preview of a session's finished-subagents line: a list.
func finishedAgentLines(r Row) []string {
	lines := []string{styleMuted.Render(" " + plural(r.Finished, "finished subagent") + " · " + formatTokens(r.Tokens) + " tokens"), ""}
	for _, a := range r.Owner.Snap.Agents {
		if !agentRunning(*r.Owner, a) {
			lines = append(lines, " "+statusIcon(r.Status, 0)+" "+agentLabel(a)+styleMuted.Render(" · "+formatTokens(a.Usage.Fresh())+" tokens"))
		}
	}
	hint := " enter shows them, to read what each did"
	if !r.Collapsed {
		hint = " enter hides them"
	}
	return append(lines, "", styleMuted.Render(hint))
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
		styleMuted.Render(" " + plural(r.Sessions, "session") + " · " + formatTokens(r.Tokens) + " tokens"),
		" " + styleWorking.Render(fmt.Sprintf("%d working", r.Stats.Working)) + styleMuted.Render(" · ") +
			styleNeeds.Render(fmt.Sprintf("%d need you", r.Stats.NeedsYou)),
	}
	if r.Workflow.LastRepo != "" {
		lines = append(lines, " last folder: "+repoLabel(r.Workflow.LastRepo))
	}
	return append(lines, "", styleMuted.Render(" enter fold/unfold · n new session here · a archive"))
}
