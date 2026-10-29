package board

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestAttachCommandResetsTheTerminalAfterwards(t *testing.T) {
	var out bytes.Buffer
	c := inTerminal(exec.Command("sh", "-c", "printf drawn; exit 3"))
	c.SetStdin(strings.NewReader(""))
	c.SetStdout(&out)
	c.SetStderr(io.Discard)
	if err := c.Run(); err == nil {
		t.Error("the command's exit status should come back")
	}
	if got := out.String(); got != "drawn"+resetModes {
		t.Errorf("output = %q, want the command's output then the reset", got)
	}
	// What `claude attach` turns on that the board doesn't set again itself.
	for _, mode := range []string{"\x1b[?2026l", "\x1b[?1000l", "\x1b[?1002l", "\x1b[?1003l", "\x1b[?1006l", "\x1b[?1004l", "\x1b[?2031l", "\x1b[r", "\x1b[0m", "\x1b(B"} {
		if !strings.Contains(resetModes, mode) {
			t.Errorf("reset is missing %q", mode)
		}
	}
}

func TestCtrlLRedrawsTheScreen(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	_, cmd := press(t, m, "ctrl+l")
	if cmd == nil {
		t.Fatal("ctrl+l did nothing")
	}
	if got, want := fmt.Sprintf("%T", cmd()), fmt.Sprintf("%T", tea.ClearScreen()); got != want {
		t.Errorf("ctrl+l sent %s, want %s (a full redraw)", got, want)
	}
	m.mode, m.height = modeHelp, 24
	if !strings.Contains(ansi.Strip(m.render()), "redraw the screen") {
		t.Error("help doesn't list ctrl+l")
	}
}
