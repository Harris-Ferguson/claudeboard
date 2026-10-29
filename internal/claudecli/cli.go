package claudecli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	shortTimeout  = 5 * time.Second
	launchTimeout = 15 * time.Second
)

// CLI runs the claude command. Bin is the executable, normally "claude".
type CLI struct {
	Bin string
}

// New returns a CLI that runs "claude" from PATH.
func New() CLI { return CLI{Bin: "claude"} }

// LaunchOpts describe a background session to start.
type LaunchOpts struct {
	Dir            string // folder the session runs in
	Name           string
	Worktree       string // git worktree name; empty for none
	Model          string
	PermissionMode string
	IDE            bool
	Prompt         string // empty: start idle and wait for the user
}

// LaunchArgs builds the claude arguments for o.
func LaunchArgs(o LaunchOpts) []string {
	args := []string{"--bg", "--name", o.Name}
	if o.Worktree != "" {
		args = append(args, "--worktree", o.Worktree)
	}
	if o.Model != "" {
		args = append(args, "--model", o.Model)
	}
	if o.PermissionMode != "" {
		args = append(args, "--permission-mode", o.PermissionMode)
	}
	if o.IDE {
		args = append(args, "--ide")
	}
	if p := o.Prompt; p != "" {
		// A leading "-" would be read as a flag; a leading space keeps it a prompt.
		if strings.HasPrefix(p, "-") {
			p = " " + p
		}
		args = append(args, p)
	}
	return args
}

func (c CLI) command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Bin, args...)
	cmd.Dir = dir
	cmd.Env = CleanEnv(os.Environ())
	return cmd
}

// run runs claude to completion and returns its trimmed combined output.
func (c CLI) run(ctx context.Context, timeout time.Duration, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.command(ctx, dir, args...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("claude %s: %s", args[0], text)
	}
	return text, nil
}

// Version returns the installed Claude Code version, e.g. "2.1.312".
func (c CLI) Version(ctx context.Context) (string, error) {
	out, err := c.run(ctx, shortTimeout, "", "--version")
	if err != nil {
		return "", err
	}
	return ParseVersion(out), nil
}

// List returns every session Claude knows about, including stopped background ones.
func (c CLI) List(ctx context.Context) ([]Session, error) {
	ctx, cancel := context.WithTimeout(ctx, shortTimeout)
	defer cancel()
	cmd := c.command(ctx, "", "agents", "--json", "--all")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("claude agents --json: %s", msg)
	}
	return ParseAgents(out)
}

// Launch starts a background session and returns its short ID.
func (c CLI) Launch(ctx context.Context, o LaunchOpts) (string, error) {
	out, err := c.run(ctx, launchTimeout, o.Dir, LaunchArgs(o)...)
	if err != nil {
		return "", err
	}
	id, _, err := ParseLaunch(out)
	return id, err
}

// Stop stops a background session, keeping its conversation and worktree.
func (c CLI) Stop(ctx context.Context, shortID string) (string, error) {
	return c.run(ctx, shortTimeout, "", "stop", shortID)
}

// Remove deletes a background session and its worktree.
func (c CLI) Remove(ctx context.Context, shortID string) (string, error) {
	return c.run(ctx, shortTimeout, "", "rm", shortID)
}

// Resume restarts a stopped session in the background and returns its short ID. Only
// call it for stopped sessions: on a running one Claude starts a copy instead.
func (c CLI) Resume(ctx context.Context, sessionID, dir string) (string, error) {
	out, err := c.run(ctx, launchTimeout, dir, "--bg", "--resume", sessionID)
	if err != nil {
		return "", err
	}
	id, _, err := ParseLaunch(out)
	return id, err
}

// AttachCmd is `claude attach <shortID>`, for the caller to run in its terminal.
func (c CLI) AttachCmd(shortID string) *exec.Cmd {
	return c.command(context.Background(), "", "attach", shortID)
}

// TrustCmd is an interactive claude in dir, so the user can accept its trust prompt.
func (c CLI) TrustCmd(dir string) *exec.Cmd {
	return c.command(context.Background(), dir)
}
