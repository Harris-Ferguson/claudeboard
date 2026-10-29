package board

import (
	"io"
	"os/exec"
)

// resetModes turns off what `claude attach` turns on and the board doesn't set again
// itself: synchronized output, mouse, focus and colour-scheme reporting, a scroll
// region, text attributes and the character set. claude attach resets these when it
// detaches cleanly, but if it exits any other way, or draws again on its way out, the
// board would be drawn into a terminal in the wrong state. Bubble Tea sets bracketed
// paste, keyboard modes, the cursor and the alt screen again when it takes the
// terminal back.
const resetModes = "\x1b[?2026l" +
	"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
	"\x1b[?1004l\x1b[?2031l" +
	"\x1b[r\x1b[0m\x1b(B\x0f"

// terminalCommand runs a claude command in the board's terminal (`claude attach`, or
// claude for its trust prompt), then writes resetModes before the board takes the
// terminal back.
type terminalCommand struct{ *exec.Cmd }

func inTerminal(c *exec.Cmd) *terminalCommand { return &terminalCommand{c} }

func (c *terminalCommand) SetStdin(r io.Reader) {
	if c.Stdin == nil {
		c.Stdin = r
	}
}

func (c *terminalCommand) SetStdout(w io.Writer) {
	if c.Stdout == nil {
		c.Stdout = w
	}
}

func (c *terminalCommand) SetStderr(w io.Writer) {
	if c.Stderr == nil {
		c.Stderr = w
	}
}

// Run implements tea.ExecCommand.
func (c *terminalCommand) Run() error {
	err := c.Cmd.Run()
	if c.Stdout != nil {
		_, _ = io.WriteString(c.Stdout, resetModes)
	}
	return err
}
