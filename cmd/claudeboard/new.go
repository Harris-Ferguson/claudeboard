package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

const newUsage = `usage: claudeboard new [-w workflow] [-r repo|path] [--worktree] [prompt…]

  -w workflow   workflow to add the session to (default: quick)
  -r repo|path  a repo name from config.toml, "scratch", or a folder (default: here)
  --worktree    run the session in its own git worktree
  prompt        start with this prompt in the background and return;
                without one, the session opens here (ctrl+z leaves it running)
`

type newArgs struct {
	workflow string
	repo     string
	worktree bool
	prompt   string
}

func parseNewArgs(args []string) (newArgs, error) {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var a newArgs
	fs.StringVar(&a.workflow, "w", "", "")
	fs.StringVar(&a.repo, "r", "", "")
	fs.BoolVar(&a.worktree, "worktree", false, "")
	if err := fs.Parse(args); err != nil {
		return newArgs{}, err
	}
	a.prompt = strings.TrimSpace(strings.Join(fs.Args(), " "))
	return a, nil
}

func runNew(args []string, stdout, stderr io.Writer) int {
	a, err := parseNewArgs(args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, newUsage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(stderr, "claudeboard new: %v\n\n%s", err, newUsage)
		return 2
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
	dir, err := cfg.ResolveRepo(a.repo, cwd)
	if err != nil {
		return fail(stderr, err)
	}
	cli := claudecli.New()
	sess, err := launch.Run(context.Background(), cli, store.Open(home), cfg, launch.Request{
		Workflow: a.workflow, Dir: dir, Repo: cfg.RepoName(dir), Worktree: a.worktree, Prompt: a.prompt,
	}, time.Now())
	if claudecli.IsUntrusted(err) {
		fmt.Fprintf(stderr, "claudeboard: %s isn't trusted by Claude Code yet. Run `claude` there once, accept the trust prompt, then try again.\n", dir)
		return 1
	}
	if err != nil {
		return fail(stderr, err)
	}
	if a.prompt != "" {
		fmt.Fprintf(stdout, "started %s (%s)\n", sess.Name, sess.ShortID)
		return 0
	}
	attach := cli.AttachCmd(sess.ShortID)
	attach.Stdin, attach.Stdout, attach.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := attach.Run(); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "claudeboard: %v\n", err)
	return 1
}
