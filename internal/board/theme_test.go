package board

import (
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

func TestARefreshStartsTheSpinnerOnce(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.spinning = false
	if cmd := m.startSpinner(); cmd == nil || !m.spinning {
		t.Fatal("a working session should start the spinner")
	}
	if cmd := m.startSpinner(); cmd != nil {
		t.Error("a second spinner started")
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
	if dark || !strings.Contains(m.render(), styleBadge.Render("claudeboard")) {
		t.Error("a light background should switch to the light palette")
	}
	m, _ = send(m, tea.BackgroundColorMsg{Color: color.RGBA{0x1b, 0x1d, 0x23, 0xff}})
	if !dark {
		t.Error("a dark background should switch back")
	}
}
