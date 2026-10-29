package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/board"
	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// runBoard checks Claude Code is usable, then runs the board until the user quits.
func runBoard(stderr io.Writer) int {
	ctx := context.Background()
	cli := claudecli.New()
	v, err := cli.Version(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "claudeboard: couldn't run `claude --version` (%v).\nIs Claude Code installed and on your PATH?\n", err)
		return 1
	}
	if _, err := cli.List(ctx); err != nil {
		fmt.Fprintf(stderr, "claudeboard: %v\nclaudeboard needs Claude Code %s or newer with background sessions (`claude agents --json`).\n", err, claudecli.MinVersion)
		return 1
	}
	home, err := config.Home()
	if err != nil {
		return fail(stderr, err)
	}
	cfg, err := config.Load(home)
	if err != nil {
		return fail(stderr, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(stderr, err)
	}
	warning := ""
	if claudecli.CompareVersions(v, claudecli.MinVersion) < 0 {
		warning = fmt.Sprintf("Claude Code %s is older than %s, the version claudeboard was tested with", v, claudecli.MinVersion)
	}
	m := board.New(board.Deps{
		Src:         cli,
		Store:       store.Open(home),
		Cfg:         cfg,
		ProjectsDir: transcript.ProjectsDir(),
		Cwd:         cwd,
		Now:         time.Now,
	}, warning)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return fail(stderr, err)
	}
	return 0
}
