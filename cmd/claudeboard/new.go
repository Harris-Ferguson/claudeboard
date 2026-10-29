package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
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
	cwd, err := os.Getwd()
	if err != nil {
		return fail(stderr, err)
	}
	return newSession(a, newEnv{
		cli:      claudecli.New(),
		home:     home,
		projects: transcript.ProjectsDir(),
		cwd:      cwd,
		stdin:    os.Stdin,
		terminal: term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stdout.Fd()),
		now:      time.Now,
	}, stdout, stderr)
}

// newCLI is the part of claudecli.CLI that `claudeboard new` uses.
type newCLI interface {
	launch.Launcher
	Available() error
	AttachCmd(shortID string) *exec.Cmd
	TrustCmd(dir string) *exec.Cmd
}

// newEnv is the outside world `claudeboard new` works in; tests replace it.
type newEnv struct {
	cli      newCLI
	home     string
	projects string // where Claude Code keeps transcripts
	cwd      string
	stdin    io.Reader // where answers to questions come from
	terminal bool      // stdin and stdout are a terminal, so a session can open here
	now      func() time.Time
}

// newSession starts a session as described by a, then either opens it here or (with a
// prompt) leaves it running in the background.
func newSession(a newArgs, env newEnv, stdout, stderr io.Writer) int {
	if err := env.cli.Available(); err != nil {
		return fail(stderr, err)
	}
	if a.prompt == "" && !env.terminal {
		fmt.Fprintln(stderr, "claudeboard: this isn't a terminal, so the session has nowhere to open.\n"+
			"To start one in the background instead, pass a prompt: claudeboard new [options] \"your prompt\"")
		return 1
	}
	cfg, err := config.Load(env.home)
	if err != nil {
		return fail(stderr, err)
	}
	dir, err := cfg.ResolveRepo(a.repo, env.cwd)
	if err != nil {
		return fail(stderr, err)
	}
	req := launch.Request{Workflow: a.workflow, Dir: dir, Repo: cfg.RepoName(dir), Worktree: a.worktree, Prompt: a.prompt}
	start := func() (store.Session, error) {
		return launch.Run(context.Background(), env.cli, store.Open(env.home), cfg, req, env.now())
	}
	sess, err := start()
	if claudecli.IsUntrusted(err) {
		question := fmt.Sprintf("%s isn't trusted by Claude Code yet. Open claude there to accept the trust prompt (then type /exit)? [Y/n] ", dir)
		if !env.terminal || !ask(env.stdin, stdout, question) {
			fmt.Fprintf(stderr, "claudeboard: %s isn't trusted by Claude Code yet. Accept its trust prompt once, then try again:\n  cd %s && claude\n", dir, shellQuote(dir))
			return 1
		}
		if err := runAttached(env.cli.TrustCmd(dir), env, stdout, stderr); err != nil {
			return fail(stderr, err)
		}
		sess, err = start()
	}
	if err != nil {
		return fail(stderr, err)
	}
	reopen := "claude attach " + sess.ShortID
	if a.prompt != "" {
		fmt.Fprintf(stdout, "started %s (%s) in %s\nopen it with %s, or see it in claudeboard\n", sess.Name, sess.ShortID, dir, reopen)
		return 0
	}
	fmt.Fprintf(stdout, "opening %s (%s) in %s. Ctrl+Z inside it comes back here.\n", sess.Name, sess.ShortID, dir)
	if err := runAttached(env.cli.AttachCmd(sess.ShortID), env, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "claudeboard: %s: %v\n%s is still running; try %s\n", reopen, err, sess.Name, reopen)
		return 1
	}
	if err := recordSeen(env, sess.SessionID); err != nil {
		fmt.Fprintf(stderr, "claudeboard: saving what you've seen: %v\n", err)
	}
	fmt.Fprintf(stdout, "%s is still running. Back to it: %s (or open claudeboard)\n", sess.Name, reopen)
	return 0
}

// recordSeen notes how much of the session the user has just seen in it, so the board
// doesn't show it as unread.
func recordSeen(env newEnv, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	snap, err := transcript.NewReader(env.projects, sessionID).Update()
	if err != nil {
		return err
	}
	return store.Open(env.home).Update(func(s *store.State) error {
		s.MarkSeen(sessionID, snap.Activity)
		return nil
	})
}

// runAttached runs an interactive claude command in this terminal.
func runAttached(cmd *exec.Cmd, env newEnv, stdout, stderr io.Writer) error {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = env.stdin, stdout, stderr
	return cmd.Run()
}

// ask prints question and reads a line; an empty answer or one starting with y is yes.
func ask(in io.Reader, out io.Writer, question string) bool {
	fmt.Fprint(out, question)
	line, _ := bufio.NewReader(in).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || strings.HasPrefix(answer, "y")
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "claudeboard: %v\n", err)
	return 1
}
