package board

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// lineWith is the screen line (with its colours) whose text contains s.
func lineWith(t *testing.T, screen, s string) string {
	t.Helper()
	for _, l := range strings.Split(screen, "\n") {
		if strings.Contains(ansi.Strip(l), s) {
			return l
		}
	}
	t.Fatalf("no line contains %q:\n%s", s, ansi.Strip(screen))
	return ""
}

// background is the escape code parameters that put colour c behind text.
func background(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)
}

func TestSelectedRowIsABar(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2 // cb-payments-v2-2
	out := m.render()
	sel := lineWith(t, out, "web  hook PaymentForm")
	if !strings.Contains(sel, selOn) || !strings.Contains(ansi.Strip(sel), "▌") {
		t.Errorf("the selected row should be a bar with a mark: %q", sel)
	}
	if other := lineWith(t, out, "wire the new AfterPay"); strings.Contains(other, selOn) || strings.Contains(ansi.Strip(other), "▌") {
		t.Errorf("an unselected row is highlighted: %q", other)
	}
	// The bar runs the whole row: each coloured part inside it ends by starting the
	// bar's colours again, up to the reset that ends the row.
	line := strings.TrimSuffix(m.rowLine(m.rows[2], true, true, 38), ansi.ResetStyle)
	if strings.Count(line, ansi.ResetStyle) != strings.Count(line, ansi.ResetStyle+selOn) {
		t.Errorf("the bar stops part way along the row: %q", line)
	}
	if !strings.Contains(selOn, background(pal.selBg)) {
		t.Errorf("the bar %q isn't in the selection colour %s", selOn, background(pal.selBg))
	}
}

func TestWorkflowRowsShowWhetherTheyAreFolded(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.collapsed["q4-search"] = true
	m.rebuild()
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "▾ payments-v2") || !strings.Contains(out, "▸ q4-search") {
		t.Errorf("want ▾ on an open workflow and ▸ on a folded one:\n%s", out)
	}
}

func TestPanesAreRoundedBoxesWithTitles(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2
	out := ansi.Strip(m.render())
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[1], "╭─ Sessions ") || !strings.Contains(lines[1], "╮╭─ hook PaymentForm into checkout ") {
		t.Errorf("top of the panes: %q", lines[1])
	}
	if last := lines[len(lines)-2]; !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Errorf("bottom of the panes: %q", last)
	}
	m.mode = modeHelp
	if top := strings.Split(ansi.Strip(m.render()), "\n")[1]; !strings.Contains(top, "╭─ Keys ") {
		t.Errorf("help pane title: %q", top)
	}
}

func TestHeaderHasTheNameAsABadge(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	head := strings.Split(m.render(), "\n")[0]
	if !strings.Contains(head, styleBadge.Render("claudeboard")) {
		t.Errorf("header: %q", head)
	}
	plain := ansi.Strip(head)
	if !strings.Contains(plain, "1 working · 1 needs you") || !strings.HasSuffix(strings.TrimRight(plain, " "), "65k tokens") {
		t.Errorf("header: %q", plain)
	}
}

func TestFooterKeysStandOut(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	lines := strings.Split(m.render(), "\n")
	if foot := lines[len(lines)-1]; !strings.Contains(foot, styleKey.Render("enter")) || !strings.Contains(foot, styleMuted.Render("open (ctrl+z back)")) {
		t.Errorf("footer: %q", foot)
	}
}

func TestWorkingSessionsSpin(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	before := ansi.Strip(lineWith(t, m.render(), "wire the new AfterPay"))
	m, cmd := send(m, spinMsg{})
	if cmd == nil {
		t.Fatal("the spinner stopped while a session is working")
	}
	after := ansi.Strip(lineWith(t, m.render(), "wire the new AfterPay"))
	if before == after || !strings.Contains(before, spinner[0]+" api") || !strings.Contains(after, spinner[1]+" api") {
		t.Errorf("before %q, after %q", before, after)
	}
	for i := range m.live {
		m.live[i].State = "done"
	}
	m.rebuild()
	if _, cmd := send(m, spinMsg{}); cmd != nil {
		t.Error("the spinner keeps ticking with nothing working")
	}
}

// spins runs cmd, and every command in it if it's a batch, and counts the spinMsgs
// they give, as the program would get them. Each tick waits out its spinEvery.
func spins(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	switch msg := cmd().(type) {
	case spinMsg:
		return 1
	case tea.BatchMsg:
		n := 0
		for _, c := range msg {
			n += spins(c)
		}
		return n
	}
	return 0
}

func TestTheSpinnerTicksWhileASessionIsWorking(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := send(m, refreshWith(m))
	if n := spins(cmd); n != 1 {
		t.Fatalf("the first refresh started %d spinners, want 1", n)
	}
	m, cmd = send(m, refreshWith(m))
	if n := spins(cmd); n != 0 {
		t.Fatalf("a refresh started %d more spinners while one was going", n)
	}
	for frame := 1; frame <= 2; frame++ {
		m, cmd = send(m, spinMsg{})
		if n := spins(cmd); n != 1 || m.frame != frame {
			t.Fatalf("tick %d gave %d more ticks and frame %d; it should keep going", frame, n, m.frame)
		}
	}
	for i := range m.live {
		m.live[i].State = "done"
	}
	m, cmd = send(m, refreshWith(m))
	if n := spins(cmd); n != 0 {
		t.Errorf("a refresh started %d spinners with nothing working", n)
	}
	m, cmd = send(m, spinMsg{})
	if n := spins(cmd); n != 0 || m.spinning {
		t.Errorf("%d more ticks, spinning %v; the spinner should stop with nothing working", n, m.spinning)
	}
}

func TestBoardMatchesALightTerminal(t *testing.T) {
	t.Cleanup(func() { useTheme(true) })
	m, _ := testModel(t, 100, 16)
	cmd := m.Init()
	var asked bool
	batch, _ := cmd().(tea.BatchMsg)
	for _, c := range batch {
		if c != nil && reflect.TypeOf(c()) == reflect.TypeOf(tea.RequestBackgroundColor()) {
			asked = true
		}
	}
	if !asked {
		t.Error("the board doesn't ask for the terminal's background colour")
	}
	m, _ = send(m, tea.BackgroundColorMsg{Color: color.RGBA{0xfa, 0xfa, 0xfa, 0xff}})
	if head := strings.Split(m.render(), "\n")[0]; dark || !strings.Contains(head, background(lightPalette.accent)) {
		t.Errorf("a light background should switch to the light palette: dark %v, header %q", dark, head)
	}
	m, _ = send(m, tea.BackgroundColorMsg{Color: color.RGBA{0x1b, 0x1d, 0x23, 0xff}})
	if head := strings.Split(m.render(), "\n")[0]; !dark || !strings.Contains(head, background(darkPalette.accent)) {
		t.Errorf("a dark background should switch back: dark %v, header %q", dark, head)
	}
}
