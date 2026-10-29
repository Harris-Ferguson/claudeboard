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

Options can come before or after the prompt; everything after -- is prompt text.

examples:
  claudeboard new                               a quick session here, opened right away
  claudeboard new -w payments -r api --worktree  in the api repo, in its own worktree
  claudeboard new -w bugs "fix the flaky test"   starts in the background and returns
`

type newArgs struct {
	workflow string
	repo     string
	worktree bool
	prompt   string
}

// parseNewArgs reads -w, -r and --worktree from anywhere in args (so they can follow the
// prompt), and joins the remaining words into the prompt. Everything after "--" is
// prompt text, for prompts with words that start with "-".
func parseNewArgs(args []string) (newArgs, error) {
	var a newArgs
	var words []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		switch {
		case arg == "--":
			words = append(words, args[i+1:]...)
			i = len(args)
		case !strings.HasPrefix(arg, "-") || arg == "-":
			words = append(words, arg)
		case name == "h" || name == "help":
			return newArgs{}, flag.ErrHelp
		case name == "worktree" && !hasValue:
			a.worktree = true
		case name == "w" || name == "r":
			if !hasValue {
				if i+1 >= len(args) {
					return newArgs{}, fmt.Errorf("-%s needs a value", name)
				}
				i++
				value = args[i]
			}
			if name == "w" {
				a.workflow = value
			} else {
				a.repo = value
			}
		default:
			return newArgs{}, fmt.Errorf("unknown option %s (put the prompt after -- if it starts with -)", arg)
		}
	}
	if len(words) == 1 && words[0] == "help" {
		return newArgs{}, flag.ErrHelp
	}
	a.prompt = strings.TrimSpace(strings.Join(words, " "))
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
