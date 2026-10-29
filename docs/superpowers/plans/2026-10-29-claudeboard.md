# claudeboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `claudeboard`, a terminal control panel that starts, tracks and switches between Claude Code background sessions grouped into workflows across repos.

**Architecture:** One Go binary. `internal/claudecli` wraps the `claude` CLI (`--bg`, `agents --json`, `attach`, `stop`, `rm`). `internal/transcript` reads conversation files for previews and token totals. `internal/store` keeps claudeboard's own workflow and session records in `~/.claudeboard/state.json`. `internal/launch` starts and records sessions for both the board and `claudeboard new`. `internal/board` is the Bubble Tea UI.

**Tech Stack:** Go 1.26+, Bubble Tea v2, Bubbles v2 (textinput), Lip Gloss v2, charmbracelet/x/ansi, BurntSushi/toml.

**Spec:** `docs/superpowers/specs/2026-10-28-claudeboard-design.md`

## Global Constraints

- Module path `github.com/Harris-Ferguson/claudeboard`; `go.mod` says `go 1.26.0` (Bubble Tea v2.1.1 needs Go 1.26).
- Dependencies, exactly: `charm.land/bubbletea/v2 v2.1.1`, `charm.land/bubbles/v2 v2.2.1`, `charm.land/lipgloss/v2 v2.0.6`, `github.com/charmbracelet/x/ansi v0.11.9`, `github.com/BurntSushi/toml v1.6.0`. Nothing else outside the standard library.
- Minimum Claude Code version: `2.1.312` (`claudecli.MinVersion`).
- Names: workflow slug is lowercase `[a-z0-9-]`, runs of other characters become `-`, trimmed to 24 characters; session name `cb-<slug>-<n>`; worktree name `<slug>-<n>`; the quick workflow's slug is `quick`.
- Data folder: `$CLAUDEBOARD_HOME`, else `~/.claudeboard`. Files: `config.toml`, `state.json`, `state.lock`, `scratch/`.
- Transcripts: `$CLAUDE_CONFIG_DIR/projects`, else `~/.claude/projects`. Main file found by glob `*/<sessionId>.jsonl`; subagents in `<main path without .jsonl>/subagents/*.jsonl`.
- Environment variables removed from every `claude` command claudeboard runs, exactly: `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_EXECPATH`, `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN`, `CLAUDE_CODE_SESSION_ATTENDED`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_EFFORT`, `CLAUDE_PID`. All other variables pass through.
- Timings and sizes: refresh every 1.5 s; list/stop/rm timeout 5 s; launch/resume timeout 15 s; status shown stale after 5 s; preview hidden by default below 90 columns; preview keeps the last 200 items; Bash summaries cut at 60 characters; row labels cut at 40 characters.
- No default test runs the real `claude`. The only exception is the opt-in `go test -tags realclaude ./internal/claudecli`.
- Bubble Tea models are values. Call methods that mutate `m` (`startRefresh`, `launchCmd`, `rebuild`, …) as their own statement before `return m, cmd`, never inside the return list: Go doesn't specify whether `m` is copied before or after a call in the same return list.
- Every task ends with `go vet ./... && go test ./...` passing, then a commit whose message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`, then `git push origin main`. The user asked for direct pushes to main.

## Review Focus

1. **Huge transcript lines.** Tool results can put whole files on one line, several MB long. They must be read with no line-length limit, and token counts must still be right. Test: `TestReaderHandlesHugeLines` (Task 8).
2. **More sessions than fit on screen.** The left list must scroll so the cursor stays visible. Test: `TestListScrollsToCursor` (Task 12).
3. **Wide characters and escape codes in prompts and replies** (CJK, emoji, `\x1b[…m`, tabs). Every rendered line must stay exactly the terminal width. Tests: `TestWideCharactersKeepBoxAligned` (Task 12) and `TestClean` (Task 11).
4. **The selected session vanishing between refreshes**, e.g. deleted elsewhere or an Other session exiting. The cursor must stay in range and never land on the "Other sessions" header. Test: `TestSelectionClampsWhenSessionDisappears` (Task 13).
5. **The board and `claudeboard new` writing `state.json` at the same time.** Neither may lose the other's session. Tests: `TestUpdateIsSerialised` (Task 4) and `launch.Run` holding the store lock for the whole launch (Task 9).

## File Structure

```
go.mod, go.sum
.github/workflows/ci.yml           go vet + go test on push to main
README.md                          install, use, PHPStorm notes, first-run checklist
cmd/claudeboard/
  main.go                          subcommand dispatch, --version
  new.go                           `claudeboard new`
  board.go                         startup checks, runs the board
internal/config/config.go          config.toml: load, example, repos
internal/store/naming.go           Slug, SessionName, WorktreeName, ParseSessionName
internal/store/state.go            State, Workflow, Session, Relink
internal/store/store.go            Load / Update (flock + atomic write)
internal/claudecli/parse.go        Session, ParseAgents, ParseLaunch, versions, IsUntrusted
internal/claudecli/env.go          MarkerVars, CleanEnv
internal/claudecli/cli.go          CLI wrappers, LaunchOpts, LaunchArgs
internal/status/status.go          live listing → board status
internal/transcript/transcript.go  Item, Usage, Snapshot
internal/transcript/accumulate.go  per-line parsing, Summarize
internal/transcript/reader.go      incremental file reading, ProjectsDir
internal/launch/launch.go          Run, DirBusy
internal/board/format.go           token formatting, truncation, labels
internal/board/rows.go             Row, BuildRows
internal/board/model.go            Source, Deps, Model, selection helpers
internal/board/view.go             rendering
internal/board/update.go           Init, Update, refresh cycle, list keys
internal/board/keys.go             commandKey, flowKey (grow in Tasks 14–15)
internal/board/actions.go          enter, stop, resume, delete, archive
internal/board/newflow.go          new-session flow, quick one-off, trust
```

---

### Task 1: Project skeleton and CI

**Files:**
- Create: `go.mod`, `cmd/claudeboard/main.go`, `cmd/claudeboard/main_test.go`, `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `run(args []string, stdout, stderr io.Writer) int` and `buildVersion() string` in package `main`.

- [ ] **Step 1: Create the module**

```bash
cd /Users/harrisferguson/organizer
go mod init github.com/Harris-Ferguson/claudeboard
go mod edit -go=1.26.0
```

- [ ] **Step 2: Write the failing test** — `cmd/claudeboard/main_test.go`

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"--version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "claudeboard ") {
		t.Fatalf("got %q, want it to start with %q", out.String(), "claudeboard ")
	}
}
```

- [ ] **Step 3: Run it to see it fail**

Run: `go test ./cmd/claudeboard`
Expected: FAIL, `undefined: run`

- [ ] **Step 4: Implement** — `cmd/claudeboard/main.go`

```go
// Command claudeboard is a terminal control panel for Claude Code background sessions.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3". Builds from a
// git checkout without it report the commit instead.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches subcommands and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "--version", "-v", "version":
			fmt.Fprintln(stdout, "claudeboard", buildVersion())
			return 0
		}
	}
	fmt.Fprintln(stderr, "claudeboard: the board isn't built yet")
	return 1
}

// buildVersion returns version, or "dev (<commit>)" when built from a git checkout.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			return "dev (" + s.Value[:7] + ")"
		}
	}
	return version
}
```

- [ ] **Step 5: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Add CI** — `.github/workflows/ci.yml`

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go vet ./...
      - run: go test ./...
```

- [ ] **Step 7: Commit and push**

```bash
git add go.mod cmd .github
git commit -F - <<'EOF'
Add project skeleton with --version and CI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 2: Config

**Files:**
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `type Config struct { ScratchDir string; Repos map[string]string; Defaults Defaults }`
  - `type Defaults struct { Model, PermissionMode string; IDE bool }`
  - `type Repo struct { Name, Dir string }`
  - `func Home() (string, error)`
  - `func ExpandHome(p string) string`
  - `func Load(home string) (Config, error)`
  - `func (c Config) RepoList() []Repo` (sorted by name)
  - `func (c Config) RepoName(dir string) string` (config name, `"scratch"`, or the cleaned path)
  - `func (c Config) ResolveRepo(arg, cwd string) (string, error)`

- [ ] **Step 1: Add the TOML dependency**

```bash
go get github.com/BurntSushi/toml@v1.6.0
```

- [ ] **Step 2: Write the failing tests** — `internal/config/config_test.go`

```go
package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCreatesExampleConfig(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[repos]") {
		t.Errorf("example config has no [repos] section:\n%s", data)
	}
	if want := filepath.Join(home, "scratch"); cfg.ScratchDir != want {
		t.Errorf("ScratchDir = %q, want %q", cfg.ScratchDir, want)
	}
	if info, err := os.Stat(cfg.ScratchDir); err != nil || !info.IsDir() {
		t.Errorf("scratch folder not created: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("Repos = %v, want none", cfg.Repos)
	}
}

func TestLoadReadsReposAndDefaults(t *testing.T) {
	t.Setenv("HOME", "/Users/tester")
	home := t.TempDir()
	scratch := filepath.Join(home, "s")
	writeConfig(t, home, `
scratch_dir = "`+scratch+`"

[repos]
api = "~/work/api"
web = "/srv/web/"

[defaults]
model = "opus"
permission_mode = "plan"
ide = true
`)
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"api": "/Users/tester/work/api", "web": "/srv/web"}
	if !reflect.DeepEqual(cfg.Repos, want) {
		t.Errorf("Repos = %v, want %v", cfg.Repos, want)
	}
	if cfg.Defaults != (Defaults{Model: "opus", PermissionMode: "plan", IDE: true}) {
		t.Errorf("Defaults = %+v", cfg.Defaults)
	}
	if cfg.ScratchDir != scratch {
		t.Errorf("ScratchDir = %q, want %q", cfg.ScratchDir, scratch)
	}
}

func TestLoadRejectsBadTOML(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "repos = [")
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("err = %v, want an error naming config.toml", err)
	}
}

func TestHome(t *testing.T) {
	t.Setenv("CLAUDEBOARD_HOME", "/tmp/cb-home")
	if h, _ := Home(); h != "/tmp/cb-home" {
		t.Errorf("Home() = %q with CLAUDEBOARD_HOME set", h)
	}
	t.Setenv("CLAUDEBOARD_HOME", "")
	t.Setenv("HOME", "/Users/tester")
	if h, _ := Home(); h != "/Users/tester/.claudeboard" {
		t.Errorf("Home() = %q, want /Users/tester/.claudeboard", h)
	}
}

func TestRepoNameAndList(t *testing.T) {
	cfg := Config{ScratchDir: "/s", Repos: map[string]string{"web": "/w", "api": "/a"}}
	if got := cfg.RepoList(); !reflect.DeepEqual(got, []Repo{{"api", "/a"}, {"web", "/w"}}) {
		t.Errorf("RepoList() = %v", got)
	}
	for dir, want := range map[string]string{"/a/": "api", "/w": "web", "/s": "scratch", "/x/y": "/x/y"} {
		if got := cfg.RepoName(dir); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestResolveRepo(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{ScratchDir: "/s", Repos: map[string]string{"api": "/a"}}
	for arg, want := range map[string]string{"": cwd, "api": "/a", "scratch": "/s", "sub": filepath.Join(cwd, "sub")} {
		got, err := cfg.ResolveRepo(arg, cwd)
		if err != nil || got != want {
			t.Errorf("ResolveRepo(%q) = %q, %v; want %q", arg, got, err, want)
		}
	}
	if _, err := cfg.ResolveRepo("nope", cwd); err == nil {
		t.Error("ResolveRepo(\"nope\") should fail")
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/config`
Expected: FAIL, `undefined: Load` (and the other functions)

- [ ] **Step 4: Implement** — `internal/config/config.go`

```go
// Package config loads claudeboard's config file, <home>/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Defaults are passed to every session claudeboard starts.
type Defaults struct {
	Model          string `toml:"model"`
	PermissionMode string `toml:"permission_mode"`
	IDE            bool   `toml:"ide"`
}

// Config is the parsed config file, with "~" expanded in every path.
type Config struct {
	ScratchDir string            `toml:"scratch_dir"`
	Repos      map[string]string `toml:"repos"`
	Defaults   Defaults          `toml:"defaults"`
}

// Repo is one entry of the [repos] table.
type Repo struct {
	Name string
	Dir  string
}

const exampleConfig = `# claudeboard config

# Where quick one-offs (the "s" key) run. Default: <claudeboard home>/scratch
# scratch_dir = "~/.claudeboard/scratch"

# Repos you start sessions in: name = path
[repos]
# api = "~/work/api"
# web = "~/work/web"

[defaults]
model = ""            # empty = Claude Code's default
permission_mode = ""  # empty = Claude Code's default
ide = false           # pass --ide so diffs open in your IDE (untested with background sessions)
`

// Home returns claudeboard's data folder: $CLAUDEBOARD_HOME, or ~/.claudeboard.
func Home() (string, error) {
	if h := os.Getenv("CLAUDEBOARD_HOME"); h != "" {
		return h, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claudeboard"), nil
}

// ExpandHome replaces a leading "~" with the user's home folder.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if u, err := os.UserHomeDir(); err == nil {
			return filepath.Join(u, p[1:])
		}
	}
	return p
}

// Load reads home/config.toml, creating it with commented examples on first run, and
// makes sure the scratch folder exists.
func Load(home string) (Config, error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Config{}, err
	}
	path := filepath.Join(home, "config.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(path, []byte(exampleConfig), 0o644); err != nil {
			return Config{}, err
		}
	}
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if c.ScratchDir == "" {
		c.ScratchDir = filepath.Join(home, "scratch")
	}
	c.ScratchDir = filepath.Clean(ExpandHome(c.ScratchDir))
	for name, p := range c.Repos {
		c.Repos[name] = filepath.Clean(ExpandHome(p))
	}
	if err := os.MkdirAll(c.ScratchDir, 0o755); err != nil {
		return Config{}, err
	}
	return c, nil
}

// RepoList returns the configured repos sorted by name.
func (c Config) RepoList() []Repo {
	out := make([]Repo, 0, len(c.Repos))
	for _, name := range slices.Sorted(maps.Keys(c.Repos)) {
		out = append(out, Repo{Name: name, Dir: c.Repos[name]})
	}
	return out
}

// RepoName is how a folder is shown: its config name, "scratch", or the path itself.
func (c Config) RepoName(dir string) string {
	dir = filepath.Clean(dir)
	for _, r := range c.RepoList() {
		if r.Dir == dir {
			return r.Name
		}
	}
	if c.ScratchDir != "" && dir == filepath.Clean(c.ScratchDir) {
		return "scratch"
	}
	return dir
}

// ResolveRepo turns a `-r` argument into a folder: empty means cwd, then a config repo
// name, "scratch", or a path (relative paths are relative to cwd).
func (c Config) ResolveRepo(arg, cwd string) (string, error) {
	switch {
	case arg == "":
		return cwd, nil
	case c.Repos[arg] != "":
		return c.Repos[arg], nil
	case arg == "scratch":
		return c.ScratchDir, nil
	}
	p := ExpandHome(arg)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%q is neither a repo in config.toml nor a folder", arg)
	}
	return filepath.Clean(p), nil
}
```

- [ ] **Step 5: Run tests**

Run: `go mod tidy && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit and push**

```bash
git add go.mod go.sum internal/config
git commit -F - <<'EOF'
Add config loading with repos and defaults

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 3: Naming

**Files:**
- Create: `internal/store/naming.go`, `internal/store/naming_test.go`

**Interfaces:**
- Produces:
  - `const QuickSlug = "quick"`
  - `func Slug(name string) string`
  - `func SessionName(slug string, n int) string`
  - `func WorktreeName(slug string, n int) string`
  - `func ParseSessionName(name string) (slug string, n int, ok bool)`

- [ ] **Step 1: Write the failing tests** — `internal/store/naming_test.go`

```go
package store

import "testing"

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Payments V2":                 "payments-v2",
		"  q4 search!! ":              "q4-search",
		"!!!":                         "",
		"a very long workflow name that keeps going": "a-very-long-workflow-nam",
		"abcdefghijklmnopqrstuvw xyz": "abcdefghijklmnopqrstuvw", // cut lands on "-", which is trimmed
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	if got := SessionName("payments-v2", 3); got != "cb-payments-v2-3" {
		t.Errorf("SessionName = %q", got)
	}
	if got := WorktreeName("payments-v2", 3); got != "payments-v2-3" {
		t.Errorf("WorktreeName = %q", got)
	}
}

func TestParseSessionName(t *testing.T) {
	good := map[string]struct {
		slug string
		n    int
	}{
		"cb-payments-v2-3": {"payments-v2", 3},
		"cb-quick-12":      {"quick", 12},
	}
	for name, want := range good {
		slug, n, ok := ParseSessionName(name)
		if !ok || slug != want.slug || n != want.n {
			t.Errorf("ParseSessionName(%q) = %q, %d, %v", name, slug, n, ok)
		}
	}
	for _, name := range []string{"arbybot-8d", "cb--1", "cb-x-0", "cb-x-y", "cb-x", ""} {
		if _, _, ok := ParseSessionName(name); ok {
			t.Errorf("ParseSessionName(%q) should fail", name)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/store`
Expected: FAIL, `undefined: Slug`

- [ ] **Step 3: Implement** — `internal/store/naming.go`

```go
package store

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// QuickSlug is the workflow quick one-offs go into.
const QuickSlug = "quick"

const maxSlugLen = 24

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a workflow name into its slug: lowercase, every run of other characters
// replaced by "-", trimmed to 24 characters. It is empty when the name has no letter
// or digit.
func Slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	return s
}

// SessionName is the name claudeboard gives the n-th session of a workflow.
func SessionName(slug string, n int) string { return fmt.Sprintf("cb-%s-%d", slug, n) }

// WorktreeName is the git worktree name for the n-th session of a workflow.
func WorktreeName(slug string, n int) string { return fmt.Sprintf("%s-%d", slug, n) }

// ParseSessionName splits "cb-<slug>-<n>" into its slug and number.
func ParseSessionName(name string) (slug string, n int, ok bool) {
	rest, found := strings.CutPrefix(name, "cb-")
	if !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(rest, '-')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return rest[:i], n, true
}
```

- [ ] **Step 4: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit and push**

```bash
git add internal/store
git commit -F - <<'EOF'
Add workflow and session naming

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 4: State and store

**Files:**
- Create: `internal/store/state.go`, `internal/store/store.go`, `internal/store/state_test.go`, `internal/store/store_test.go`

**Interfaces:**
- Consumes: `Slug`, `ParseSessionName` (Task 3).
- Produces:
  - `type Workflow struct { Slug, Name string; CreatedAt time.Time; Archived bool; LastRepo string }`
  - `type Session struct { SessionID, ShortID, Name, Workflow, Repo, Dir, Worktree string; CreatedAt time.Time }`
  - `func (s Session) WorkDir() string`
  - `type State struct { Version int; Workflows []Workflow; Sessions []Session }`
  - `func (st *State) Workflow(slug string) *Workflow`
  - `func (st *State) EnsureWorkflow(name string, now time.Time) (*Workflow, error)`
  - `func (st *State) NextSessionNumber(slug string, extraNames ...string) int`
  - `func (st *State) Session(sessionID string) *Session`
  - `func (st *State) UpsertSession(s Session)`
  - `func (st *State) RemoveSession(sessionID, name string) bool`
  - `type Listed struct { SessionID, ShortID, Name, Cwd string }`
  - `func (st *State) NeedsRelink(listed []Listed) bool`
  - `func (st *State) Relink(listed []Listed, now time.Time) int`
  - `type Store`, `func Open(home string) *Store`
  - `func (s *Store) Load() (State, bool, error)` (the bool means a corrupt file was set aside)
  - `func (s *Store) Update(fn func(*State) error) error`

- [ ] **Step 1: Write the failing state tests** — `internal/store/state_test.go`

```go
package store

import (
	"testing"
	"time"
)

var now = time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)

func TestEnsureWorkflow(t *testing.T) {
	var st State
	wf, err := st.EnsureWorkflow("Payments V2", now)
	if err != nil || wf.Slug != "payments-v2" || wf.Name != "Payments V2" {
		t.Fatalf("EnsureWorkflow = %+v, %v", wf, err)
	}
	again, _ := st.EnsureWorkflow("payments v2", now)
	if len(st.Workflows) != 1 || again.Name != "Payments V2" {
		t.Errorf("same slug should reuse the workflow, got %+v", st.Workflows)
	}
	if _, err := st.EnsureWorkflow("!!!", now); err == nil {
		t.Error("a name with no letters or digits should be rejected")
	}
}

func TestNextSessionNumber(t *testing.T) {
	st := State{Sessions: []Session{{Name: "cb-w-1"}, {Name: "cb-w-3"}, {Name: "cb-other-9"}}}
	if n := st.NextSessionNumber("w"); n != 4 {
		t.Errorf("NextSessionNumber = %d, want 4", n)
	}
	if n := st.NextSessionNumber("w", "cb-w-7", "arbybot-8d"); n != 8 {
		t.Errorf("with extra names = %d, want 8", n)
	}
	if n := st.NextSessionNumber("new"); n != 1 {
		t.Errorf("empty workflow = %d, want 1", n)
	}
}

func TestUpsertAndRemoveSession(t *testing.T) {
	var st State
	st.UpsertSession(Session{Name: "cb-w-1", ShortID: "a"})
	st.UpsertSession(Session{Name: "cb-w-1", SessionID: "id-1", ShortID: "a"}) // fills the ID by name
	st.UpsertSession(Session{Name: "cb-w-2", SessionID: "id-2"})
	st.UpsertSession(Session{Name: "cb-w-2", SessionID: "id-2", Repo: "api"}) // replaces by ID
	if len(st.Sessions) != 2 || st.Session("id-1") == nil || st.Session("id-2").Repo != "api" {
		t.Fatalf("sessions = %+v", st.Sessions)
	}
	if !st.RemoveSession("id-1", "") || st.Session("id-1") != nil {
		t.Error("RemoveSession by ID failed")
	}
	st.UpsertSession(Session{Name: "cb-w-3"})
	if !st.RemoveSession("", "cb-w-3") || len(st.Sessions) != 1 {
		t.Errorf("RemoveSession by name failed: %+v", st.Sessions)
	}
}

func TestWorkDir(t *testing.T) {
	if got := (Session{Dir: "/r"}).WorkDir(); got != "/r" {
		t.Errorf("WorkDir = %q", got)
	}
	if got := (Session{Dir: "/r", Worktree: "w-2"}).WorkDir(); got != "/r/.claude/worktrees/w-2" {
		t.Errorf("WorkDir with worktree = %q", got)
	}
}

func TestRelink(t *testing.T) {
	st := State{
		Workflows: []Workflow{{Slug: "w", Name: "w"}},
		Sessions: []Session{
			{SessionID: "id-1", ShortID: "old", Name: "cb-w-1", Workflow: "w"},
			{Name: "cb-w-2", Workflow: "w"}, // launched, but its ID was never resolved
		},
	}
	listed := []Listed{
		{SessionID: "id-1", ShortID: "new", Name: "cb-w-1"},
		{SessionID: "id-2", ShortID: "s2", Name: "cb-w-2"},
		{SessionID: "id-3", ShortID: "s3", Name: "cb-bugs-1", Cwd: "/work/api"},
		{SessionID: "id-4", ShortID: "s4", Name: "arbybot-8d"},
	}
	if !st.NeedsRelink(listed) {
		t.Fatal("NeedsRelink = false, want true")
	}
	if n := st.Relink(listed, now); n != 3 {
		t.Errorf("Relink changed %d sessions, want 3", n)
	}
	if st.Session("id-1").ShortID != "new" {
		t.Error("short ID not refreshed")
	}
	if s := st.Session("id-2"); s == nil || s.ShortID != "s2" {
		t.Error("ID not filled in for session known by name")
	}
	if s := st.Session("id-3"); s == nil || s.Workflow != "bugs" || s.Dir != "/work/api" || st.Workflow("bugs") == nil {
		t.Errorf("cb-bugs-1 not adopted: %+v", st)
	}
	if st.Session("id-4") != nil {
		t.Error("a session without a cb- name should not be adopted")
	}
	if st.NeedsRelink(listed) {
		t.Error("NeedsRelink should be false after Relink")
	}
}
```

- [ ] **Step 2: Write the failing store tests** — `internal/store/store_test.go`

```go
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	st, recovered, err := Open(t.TempDir()).Load()
	if err != nil || recovered || st.Version != 1 || len(st.Sessions) != 0 {
		t.Fatalf("Load = %+v, %v, %v", st, recovered, err)
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	err := s.Update(func(st *State) error {
		st.UpsertSession(Session{SessionID: "id-1", Name: "cb-w-1", Workflow: "w"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := s.Load()
	if err != nil || st.Session("id-1") == nil {
		t.Fatalf("after Update, Load = %+v, %v", st, err)
	}
}

func TestUpdateKeepsFileWhenCallbackFails(t *testing.T) {
	home := t.TempDir()
	s := Open(home)
	if err := s.Update(func(*State) error { return errors.New("nope") }); err == nil {
		t.Fatal("Update should return the callback's error")
	}
	if _, err := os.Stat(filepath.Join(home, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state.json written despite the error: %v", err)
	}
}

func TestLoadSetsCorruptFileAside(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, recovered, err := Open(home).Load()
	if err != nil || !recovered || len(st.Sessions) != 0 {
		t.Fatalf("Load = %+v, %v, %v", st, recovered, err)
	}
	if m, _ := filepath.Glob(filepath.Join(home, "state.json.corrupt-*")); len(m) != 1 {
		t.Errorf("corrupt file not set aside: %v", m)
	}
}

func TestUpdateIsSerialised(t *testing.T) {
	s := Open(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.Update(func(st *State) error {
				st.UpsertSession(Session{SessionID: fmt.Sprint("id-", i), Name: fmt.Sprint("cb-w-", i+1)})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	st, _, _ := s.Load()
	if len(st.Sessions) != 20 {
		t.Errorf("got %d sessions after 20 concurrent updates, want 20", len(st.Sessions))
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/store`
Expected: FAIL, `undefined: State` (and the other names)

- [ ] **Step 4: Implement the state** — `internal/store/state.go`

```go
// Package store keeps claudeboard's own records: which workflows exist and which
// Claude Code sessions belong to them. Everything lives in <home>/state.json.
package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Workflow is a named group of sessions.
type Workflow struct {
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Archived  bool      `json:"archived"`
	LastRepo  string    `json:"last_repo"`
}

// Session is a Claude Code background session claudeboard started (or adopted).
type Session struct {
	SessionID string    `json:"session_id"`
	ShortID   string    `json:"short_id"`
	Name      string    `json:"name"`
	Workflow  string    `json:"workflow"`
	Repo      string    `json:"repo"`
	Dir       string    `json:"dir"`
	Worktree  string    `json:"worktree"`
	CreatedAt time.Time `json:"created_at"`
}

// WorkDir is the folder the session runs in: its worktree if it has one, else Dir.
func (s Session) WorkDir() string {
	if s.Worktree == "" {
		return s.Dir
	}
	return filepath.Join(s.Dir, ".claude", "worktrees", s.Worktree)
}

// State is the contents of state.json.
type State struct {
	Version   int        `json:"version"`
	Workflows []Workflow `json:"workflows"`
	Sessions  []Session  `json:"sessions"`
}

// Workflow returns the workflow with this slug, or nil.
func (st *State) Workflow(slug string) *Workflow {
	for i := range st.Workflows {
		if st.Workflows[i].Slug == slug {
			return &st.Workflows[i]
		}
	}
	return nil
}

// EnsureWorkflow returns the workflow for name, creating it if needed.
func (st *State) EnsureWorkflow(name string, now time.Time) (*Workflow, error) {
	slug := Slug(name)
	if slug == "" {
		return nil, fmt.Errorf("workflow name %q needs at least one letter or digit", name)
	}
	if wf := st.Workflow(slug); wf != nil {
		return wf, nil
	}
	st.Workflows = append(st.Workflows, Workflow{Slug: slug, Name: strings.TrimSpace(name), CreatedAt: now})
	return &st.Workflows[len(st.Workflows)-1], nil
}

// NextSessionNumber returns one more than the highest session number used in the
// workflow, counting recorded sessions and extraNames (such as names Claude lists).
func (st *State) NextSessionNumber(slug string, extraNames ...string) int {
	highest := 0
	check := func(name string) {
		if s, n, ok := ParseSessionName(name); ok && s == slug && n > highest {
			highest = n
		}
	}
	for _, s := range st.Sessions {
		check(s.Name)
	}
	for _, name := range extraNames {
		check(name)
	}
	return highest + 1
}

// Session returns the session with this ID, or nil.
func (st *State) Session(sessionID string) *Session {
	if sessionID == "" {
		return nil
	}
	for i := range st.Sessions {
		if st.Sessions[i].SessionID == sessionID {
			return &st.Sessions[i]
		}
	}
	return nil
}

func (st *State) sessionByName(name string) *Session {
	for i := range st.Sessions {
		if st.Sessions[i].Name == name {
			return &st.Sessions[i]
		}
	}
	return nil
}

// UpsertSession replaces the session with the same ID (or, failing that, the same
// name), or appends it.
func (st *State) UpsertSession(s Session) {
	existing := st.Session(s.SessionID)
	if existing == nil {
		existing = st.sessionByName(s.Name)
	}
	if existing != nil {
		*existing = s
		return
	}
	st.Sessions = append(st.Sessions, s)
}

// RemoveSession deletes the session with this ID, or with this name when the ID is
// empty. It reports whether anything was removed.
func (st *State) RemoveSession(sessionID, name string) bool {
	for i, s := range st.Sessions {
		if (sessionID != "" && s.SessionID == sessionID) || (sessionID == "" && s.Name == name) {
			st.Sessions = append(st.Sessions[:i], st.Sessions[i+1:]...)
			return true
		}
	}
	return false
}

// Listed is what Relink needs to know about a background session Claude lists.
type Listed struct {
	SessionID string
	ShortID   string
	Name      string
	Cwd       string
}

// NeedsRelink reports whether Relink would change anything, so callers can skip
// taking the store lock on every refresh.
func (st *State) NeedsRelink(listed []Listed) bool {
	for _, l := range listed {
		if l.SessionID == "" {
			continue
		}
		if s := st.Session(l.SessionID); s != nil {
			if l.ShortID != "" && s.ShortID != l.ShortID {
				return true
			}
			continue
		}
		if _, _, ok := ParseSessionName(l.Name); ok {
			return true
		}
	}
	return false
}

// Relink brings the state in line with what Claude lists: it refreshes short IDs,
// fills in IDs for sessions known only by name, and adopts sessions named
// cb-<slug>-<n> that the state doesn't know (creating their workflows). It returns how
// many sessions changed.
func (st *State) Relink(listed []Listed, now time.Time) int {
	changed := 0
	for _, l := range listed {
		if l.SessionID == "" {
			continue
		}
		if s := st.Session(l.SessionID); s != nil {
			if l.ShortID != "" && s.ShortID != l.ShortID {
				s.ShortID = l.ShortID
				changed++
			}
			continue
		}
		slug, _, ok := ParseSessionName(l.Name)
		if !ok {
			continue
		}
		if s := st.sessionByName(l.Name); s != nil && s.SessionID == "" {
			s.SessionID, s.ShortID = l.SessionID, l.ShortID
			changed++
			continue
		}
		if st.Workflow(slug) == nil {
			st.Workflows = append(st.Workflows, Workflow{Slug: slug, Name: slug, CreatedAt: now})
		}
		st.Sessions = append(st.Sessions, Session{
			SessionID: l.SessionID, ShortID: l.ShortID, Name: l.Name,
			Workflow: slug, Repo: l.Cwd, Dir: l.Cwd, CreatedAt: now,
		})
		changed++
	}
	return changed
}
```

- [ ] **Step 5: Implement the store** — `internal/store/store.go`

```go
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Store reads and writes <home>/state.json.
type Store struct {
	dir string
}

// Open returns the store kept in home. Nothing is read until Load or Update.
func Open(home string) *Store { return &Store{dir: home} }

func (s *Store) path() string { return filepath.Join(s.dir, "state.json") }

// Load reads the state without locking; writers replace the file atomically, so a
// reader always sees a complete version. A missing file is an empty state. A file that
// can't be parsed is renamed to state.json.corrupt-<unix-time>, and an empty state is
// returned with recovered set.
func (s *Store) Load() (st State, recovered bool, err error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: 1}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		bad := fmt.Sprintf("%s.corrupt-%d", s.path(), time.Now().Unix())
		if rerr := os.Rename(s.path(), bad); rerr != nil {
			return State{}, false, rerr
		}
		return State{Version: 1}, true, nil
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return st, false, nil
}

// Update loads the state, lets fn change it, and saves the result, all while holding
// an exclusive lock on <home>/state.lock so concurrent writers (the board and
// `claudeboard new`) take turns. If fn returns an error nothing is saved.
func (s *Store) Update(fn func(*State) error) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	st, _, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return s.save(st)
}

// save writes st to a temporary file and renames it over state.json.
func (s *Store) save(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "state-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}
```

- [ ] **Step 6: Run tests**

Run: `go vet ./... && go test -race ./internal/store && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit and push**

```bash
git add internal/store
git commit -F - <<'EOF'
Add state file with locking, recovery and re-linking

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 5: claudecli

**Files:**
- Create: `internal/claudecli/parse.go`, `internal/claudecli/env.go`, `internal/claudecli/cli.go`, `internal/claudecli/claudecli_test.go`, `internal/claudecli/realclaude_test.go`, `internal/claudecli/testdata/agents.json`

**Interfaces:**
- Produces:
  - `const MinVersion = "2.1.312"`, `const KindBackground = "background"`, `const KindInteractive = "interactive"`
  - `type Session struct { PID int; ShortID, Cwd, Kind, SessionID, Name, Status, State, WaitingFor string }` (JSON tags `pid id cwd kind sessionId name status state waitingFor`)
  - `func ParseAgents(data []byte) ([]Session, error)`
  - `func ParseLaunch(out string) (shortID, name string, err error)`
  - `func ParseVersion(out string) string`
  - `func CompareVersions(a, b string) int`
  - `func IsUntrusted(err error) bool`
  - `var MarkerVars []string`, `func CleanEnv(env []string) []string`
  - `type LaunchOpts struct { Dir, Name, Worktree, Model, PermissionMode string; IDE bool; Prompt string }`
  - `func LaunchArgs(o LaunchOpts) []string`
  - `type CLI struct { Bin string }`, `func New() CLI`
  - methods on `CLI`, all with value receivers:
    - `Version(ctx) (string, error)`
    - `List(ctx) ([]Session, error)`
    - `Launch(ctx, LaunchOpts) (string, error)`
    - `Stop(ctx, shortID) (string, error)`
    - `Remove(ctx, shortID) (string, error)`
    - `Resume(ctx, sessionID, dir) (string, error)`
    - `AttachCmd(shortID) *exec.Cmd`
    - `TrustCmd(dir) *exec.Cmd`

- [ ] **Step 1: Add the fixture** — `internal/claudecli/testdata/agents.json` (shapes recorded in the spike; IDs and paths made up)

```json
[
  {"pid": 101, "id": "aaaa1111", "cwd": "/work/api", "kind": "background", "startedAt": 1793248550286, "sessionId": "aaaa1111-0000-4000-8000-000000000001", "name": "cb-payments-v2-1", "status": "busy", "state": "working"},
  {"pid": 102, "id": "bbbb2222", "cwd": "/work/web", "kind": "background", "startedAt": 1793248550286, "sessionId": "bbbb2222-0000-4000-8000-000000000002", "name": "cb-payments-v2-2", "status": "waiting", "waitingFor": "permission prompt", "state": "blocked"},
  {"id": "cccc3333", "cwd": "/work/api", "kind": "background", "sessionId": "cccc3333-0000-4000-8000-000000000003", "name": "cb-q4-search-1", "state": "stopped"},
  {"id": "dddd4444", "kind": "background", "sessionId": "dddd4444-0000-4000-8000-000000000004", "state": "failed"},
  {"pid": 105, "cwd": "/Users/me/arbybot", "kind": "interactive", "startedAt": 1793248550286, "sessionId": "eeee5555-0000-4000-8000-000000000005", "name": "arbybot-8d", "status": "idle"}
]
```

- [ ] **Step 2: Write the failing tests** — `internal/claudecli/claudecli_test.go`

```go
package claudecli

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"testing"
)

func TestParseAgents(t *testing.T) {
	data, err := os.ReadFile("testdata/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseAgents(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d sessions, want 5", len(got))
	}
	want := Session{PID: 102, ShortID: "bbbb2222", Cwd: "/work/web", Kind: KindBackground,
		SessionID: "bbbb2222-0000-4000-8000-000000000002", Name: "cb-payments-v2-2",
		Status: "waiting", State: "blocked", WaitingFor: "permission prompt"}
	if got[1] != want {
		t.Errorf("blocked entry = %+v", got[1])
	}
	if got[2].State != "stopped" || got[2].PID != 0 {
		t.Errorf("stopped entry = %+v", got[2])
	}
	if got[4].Kind != KindInteractive || got[4].ShortID != "" || got[4].State != "" {
		t.Errorf("interactive entry = %+v", got[4])
	}
}

func TestParseAgentsEdgeCases(t *testing.T) {
	for _, in := range []string{"", "null", "[]", "  \n"} {
		if got, err := ParseAgents([]byte(in)); err != nil || len(got) != 0 {
			t.Errorf("ParseAgents(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseAgents([]byte("not json")); err == nil {
		t.Error("ParseAgents(garbage) should fail")
	}
}

func TestParseLaunch(t *testing.T) {
	cases := []struct{ out, id, name string }{
		{"Starting background service…\nbackgrounded · 301e18a1 · cb-spike-1\n  claude agents   list sessions\n", "301e18a1", "cb-spike-1"},
		{"backgrounded · bbbc96fd · cb-probe-1 (idle — send a prompt to start)\n", "bbbc96fd", "cb-probe-1"},
		{"note: woke session eaf7dcf2 with its saved options (--name, --model).\nbackgrounded · eaf7dcf2 · cb-verify-1 (idle — send a prompt to start)", "eaf7dcf2", "cb-verify-1"},
		{"note: session eaf7dcf2 is already running in the background, so this started a copy as e56cb95f.\nbackgrounded · e56cb95f (idle — send a prompt to start)", "e56cb95f", ""},
	}
	for _, c := range cases {
		id, name, err := ParseLaunch(c.out)
		if err != nil || id != c.id || name != c.name {
			t.Errorf("ParseLaunch(%q) = %q, %q, %v; want %q, %q", c.out, id, name, err, c.id, c.name)
		}
	}
	if _, _, err := ParseLaunch("Workspace not trusted. Run `claude` in /x once."); err == nil {
		t.Error("ParseLaunch should fail without a backgrounded line")
	}
}

func TestLaunchArgs(t *testing.T) {
	got := LaunchArgs(LaunchOpts{Name: "cb-w-1"})
	if want := []string{"--bg", "--name", "cb-w-1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("minimal = %q", got)
	}
	got = LaunchArgs(LaunchOpts{Name: "cb-w-2", Worktree: "w-2", Model: "opus", PermissionMode: "plan", IDE: true, Prompt: "fix it"})
	want := []string{"--bg", "--name", "cb-w-2", "--worktree", "w-2", "--model", "opus", "--permission-mode", "plan", "--ide", "fix it"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("full = %q", got)
	}
	got = LaunchArgs(LaunchOpts{Name: "cb-w-3", Prompt: "-rf is scary"})
	if got[len(got)-1] != " -rf is scary" {
		t.Errorf("a prompt starting with - must not look like a flag: %q", got)
	}
}

func TestCleanEnv(t *testing.T) {
	env := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_PID=9",
		"CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=x", "CLAUDE_CODE_SESSION_ID=abc"}
	got := CleanEnv(env)
	want := []string{"PATH=/bin", "CLAUDE_CODE_USE_BEDROCK=1", "ANTHROPIC_MODEL=x"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CleanEnv = %q, want %q", got, want)
	}
	if len(MarkerVars) != 10 || !slices.Contains(MarkerVars, "CLAUDE_CODE_MESSAGING_TOKEN") {
		t.Errorf("MarkerVars = %q", MarkerVars)
	}
}

func TestVersions(t *testing.T) {
	if v := ParseVersion("2.1.312 (Claude Code)\n"); v != "2.1.312" {
		t.Errorf("ParseVersion = %q", v)
	}
	cases := []struct {
		a, b string
		want int
	}{{"2.1.312", "2.1.312", 0}, {"2.1.311", "2.1.312", -1}, {"2.2.0", "2.1.312", 1}, {"2.1", "2.1.0", 0}, {"3", "2.9.9", 1}}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsUntrusted(t *testing.T) {
	if !IsUntrusted(errors.New("claude --bg: Workspace not trusted. Run `claude` in /x once")) {
		t.Error("should detect the untrusted message")
	}
	if IsUntrusted(nil) || IsUntrusted(errors.New("boom")) {
		t.Error("false positive")
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/claudecli`
Expected: FAIL, `undefined: ParseAgents` (and the other names)

- [ ] **Step 4: Implement parsing** — `internal/claudecli/parse.go`

```go
// Package claudecli runs the claude command and parses what it prints. Everything
// claudeboard assumes about Claude Code's command line lives here; section 4 of the
// design spec lists those assumptions and how they were verified.
package claudecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MinVersion is the Claude Code version claudeboard was verified against.
const MinVersion = "2.1.312"

// Session kinds reported by `claude agents --json`.
const (
	KindBackground  = "background"
	KindInteractive = "interactive"
)

// Session is one entry of `claude agents --json --all`. Interactive sessions have no
// ShortID or State; stopped ones have no PID or Status.
type Session struct {
	PID        int    `json:"pid"`
	ShortID    string `json:"id"`
	Cwd        string `json:"cwd"`
	Kind       string `json:"kind"`
	SessionID  string `json:"sessionId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	State      string `json:"state"`
	WaitingFor string `json:"waitingFor"`
}

// ParseAgents parses the output of `claude agents --json --all`.
func ParseAgents(data []byte) ([]Session, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var out []Session
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing claude agents --json: %w", err)
	}
	return out, nil
}

var launchLine = regexp.MustCompile(`backgrounded · ([0-9a-f]+)(?: · (\S+))?`)

// ParseLaunch finds the "backgrounded · <shortId> · <name>" line that `claude --bg`
// prints. The name is missing when Claude started a copy of a running session.
func ParseLaunch(out string) (shortID, name string, err error) {
	m := launchLine.FindStringSubmatch(out)
	if m == nil {
		return "", "", fmt.Errorf("unexpected output from claude --bg: %q", firstLine(out))
	}
	return m[1], m[2], nil
}

// ParseVersion extracts "2.1.312" from "2.1.312 (Claude Code)".
func ParseVersion(out string) string {
	if f := strings.Fields(out); len(f) > 0 {
		return f[0]
	}
	return ""
}

// CompareVersions compares dotted version numbers, returning -1, 0 or 1. Missing or
// non-numeric parts count as 0.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := versionPart(pa, i), versionPart(pb, i)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

// IsUntrusted reports whether err is Claude refusing to start in a folder whose trust
// prompt hasn't been accepted yet.
func IsUntrusted(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Workspace not trusted")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
```

- [ ] **Step 5: Implement the environment cleaning** — `internal/claudecli/env.go`

```go
package claudecli

import (
	"slices"
	"strings"
)

// MarkerVars are set by Claude Code in the shells it runs. A claude started with them
// believes it is a child of that session and doesn't save its transcript, so they are
// removed before claudeboard runs claude. Everything else, including company
// CLAUDE_CODE_* and ANTHROPIC_* settings, passes through.
var MarkerVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_EFFORT",
	"CLAUDE_PID",
}

// CleanEnv returns env without MarkerVars.
func CleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(MarkerVars, name) {
			out = append(out, kv)
		}
	}
	return out
}
```

- [ ] **Step 6: Implement the CLI wrappers** — `internal/claudecli/cli.go`

```go
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
```

- [ ] **Step 7: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 8: Add the opt-in real-Claude check** — `internal/claudecli/realclaude_test.go`

```go
//go:build realclaude

package claudecli

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestRealClaudeLifecycle starts an idle background session in this repo (no prompt,
// so no tokens are used), checks claude lists it, stops it, and deletes it.
//
//	go test -tags realclaude -v ./internal/claudecli
//
// The repo folder must be trusted: run `claude` in it once first.
func TestRealClaudeLifecycle(t *testing.T) {
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	c, ctx := New(), context.Background()

	start := time.Now()
	id, err := c.Launch(ctx, LaunchOpts{Dir: dir, Name: "cb-realcheck-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("launched %s in %s", id, time.Since(start).Round(time.Millisecond))
	defer func() {
		out, err := c.Remove(ctx, id)
		if err != nil {
			t.Errorf("rm: %v", err)
		}
		t.Log(out)
	}()

	find := func() *Session {
		start := time.Now()
		live, err := c.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("claude agents --json took %s", time.Since(start).Round(time.Millisecond))
		for i := range live {
			if live[i].ShortID == id {
				return &live[i]
			}
		}
		return nil
	}
	var s *Session
	for i := 0; i < 20 && s == nil; i++ {
		if s = find(); s == nil {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if s == nil {
		t.Fatalf("session %s never appeared in claude agents", id)
	}
	if s.Name != "cb-realcheck-1" || s.Kind != KindBackground {
		t.Errorf("listed as %+v", *s)
	}
	if _, err := c.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if s = find(); s != nil && s.State == "stopped" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("after stop, listed as %+v", s)
}
```

- [ ] **Step 9: Run the real check once** (no tokens: no prompt is sent)

Run: `go test -tags realclaude -v -run TestRealClaudeLifecycle ./internal/claudecli`
Expected: PASS, with log lines for the launch time and the `claude agents --json` time. Write both timings down for the README's "How it works" section. If listing takes more than about 1 s, raise `refreshEvery` in Task 13 to 3 s.

- [ ] **Step 10: Commit and push**

```bash
git add internal/claudecli
git commit -F - <<'EOF'
Add claude CLI wrapper and output parsing

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 6: Status mapping

**Files:**
- Create: `internal/status/status.go`, `internal/status/status_test.go`

**Interfaces:**
- Consumes: `claudecli.Session`, `claudecli.KindInteractive` (Task 5).
- Produces:
  - `type Kind int` with constants `Unknown, New, Working, NeedsYou, Done, Stopped`
  - `type Status struct { Kind Kind; Detail string }`
  - `func From(live *claudecli.Session, promptSent bool) Status`
  - `func (s Status) Icon() string`
  - `func (s Status) Text() string`

- [ ] **Step 1: Write the failing tests** — `internal/status/status_test.go`

```go
package status

import (
	"testing"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
)

func bg(state, waitingFor string) *claudecli.Session {
	return &claudecli.Session{Kind: claudecli.KindBackground, State: state, WaitingFor: waitingFor}
}

func TestFrom(t *testing.T) {
	cases := []struct {
		name   string
		live   *claudecli.Session
		prompt bool
		want   Status
	}{
		{"not listed", nil, true, Status{Kind: Stopped}},
		{"working", bg("working", ""), true, Status{Kind: Working}},
		{"waiting with a reason", bg("blocked", "permission prompt"), true, Status{Kind: NeedsYou, Detail: "permission prompt"}},
		{"fresh session", bg("blocked", ""), false, Status{Kind: New}},
		{"blocked after a prompt", bg("blocked", ""), true, Status{Kind: NeedsYou}},
		{"done", bg("done", ""), true, Status{Kind: Done}},
		{"stopped", bg("stopped", ""), true, Status{Kind: Stopped}},
		{"failed", bg("failed", ""), false, Status{Kind: Unknown, Detail: "failed"}},
		{"interactive busy", &claudecli.Session{Kind: claudecli.KindInteractive, Status: "busy"}, true, Status{Kind: Working}},
		{"interactive idle", &claudecli.Session{Kind: claudecli.KindInteractive, Status: "idle"}, true, Status{Kind: Done}},
	}
	for _, c := range cases {
		if got := From(c.live, c.prompt); got != c.want {
			t.Errorf("%s: From = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestIconAndText(t *testing.T) {
	cases := []struct {
		s          Status
		icon, text string
	}{
		{Status{Kind: Working}, "●", "working"},
		{Status{Kind: NeedsYou, Detail: "permission prompt"}, "!", "needs you — permission prompt"},
		{Status{Kind: NeedsYou}, "!", "needs you"},
		{Status{Kind: Done}, "✓", "done"},
		{Status{Kind: New}, "○", "new"},
		{Status{Kind: Stopped}, "○", "stopped"},
		{Status{Kind: Unknown, Detail: "failed"}, "?", "failed"},
		{Status{Kind: Unknown}, "?", "unknown"},
	}
	for _, c := range cases {
		if c.s.Icon() != c.icon || c.s.Text() != c.text {
			t.Errorf("%+v: Icon %q Text %q, want %q %q", c.s, c.s.Icon(), c.s.Text(), c.icon, c.text)
		}
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/status`
Expected: FAIL, `undefined: From`

- [ ] **Step 3: Implement** — `internal/status/status.go`

```go
// Package status turns what `claude agents --json` reports about a session into what
// the board shows (section 4 of the design spec, "Status mapping").
package status

import "github.com/Harris-Ferguson/claudeboard/internal/claudecli"

// Kind is a session's state as the board shows it.
type Kind int

const (
	Unknown Kind = iota
	New
	Working
	NeedsYou
	Done
	Stopped
)

// Status is a Kind plus detail: what a session is waiting for, or the raw state Claude
// reported when it isn't one claudeboard knows.
type Status struct {
	Kind   Kind
	Detail string
}

// From maps a session's listing (nil when Claude doesn't list it) and whether a prompt
// has been sent to it to a Status.
func From(live *claudecli.Session, promptSent bool) Status {
	if live == nil {
		return Status{Kind: Stopped}
	}
	if live.Kind == claudecli.KindInteractive && live.State == "" {
		if live.Status == "busy" {
			return Status{Kind: Working}
		}
		return Status{Kind: Done}
	}
	switch live.State {
	case "working":
		return Status{Kind: Working}
	case "done":
		return Status{Kind: Done}
	case "stopped":
		return Status{Kind: Stopped}
	case "blocked":
		switch {
		case live.WaitingFor != "":
			return Status{Kind: NeedsYou, Detail: live.WaitingFor}
		case !promptSent:
			return Status{Kind: New}
		default:
			return Status{Kind: NeedsYou}
		}
	}
	return Status{Kind: Unknown, Detail: live.State}
}

// Icon is the one-character marker shown before a session.
func (s Status) Icon() string {
	switch s.Kind {
	case Working:
		return "●"
	case NeedsYou:
		return "!"
	case Done:
		return "✓"
	case New, Stopped:
		return "○"
	}
	return "?"
}

// Text describes the status in a few words.
func (s Status) Text() string {
	switch s.Kind {
	case Working:
		return "working"
	case NeedsYou:
		if s.Detail != "" {
			return "needs you — " + s.Detail
		}
		return "needs you"
	case Done:
		return "done"
	case New:
		return "new"
	case Stopped:
		return "stopped"
	}
	if s.Detail != "" {
		return s.Detail
	}
	return "unknown"
}
```

- [ ] **Step 4: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit and push**

```bash
git add internal/status
git commit -F - <<'EOF'
Add status mapping from claude agents output

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 7: Transcript parsing

**Files:**
- Create: `internal/transcript/transcript.go`, `internal/transcript/accumulate.go`, `internal/transcript/accumulate_test.go`, `internal/transcript/testdata/session.jsonl`

**Interfaces:**
- Produces:
  - `type ItemKind int` with `ItemPrompt, ItemText, ItemTool`
  - `type Item struct { Kind ItemKind; Text string }`
  - `type Usage struct { Input, Output, CacheRead, CacheWrite int64 }`, `func (u Usage) Total() int64`
  - `type Snapshot struct { Found bool; Items []Item; Pending string; Usage Usage; FirstPrompt string; Prompts int; Subagents int }`
  - `func Summarize(name string, input json.RawMessage) string`
  - unexported `newAccumulator()`, `(*accumulator).add(line []byte, main bool)`, `(*accumulator).snapshot() Snapshot` (used by Task 8)

- [ ] **Step 1: Add the fixture** — `internal/transcript/testdata/session.jsonl` (made-up content in the record shapes Claude Code writes; one line per record, including a malformed line on purpose)

```
{"type":"user","isMeta":true,"message":{"role":"user","content":"<local-command-caveat>ignored</local-command-caveat>"}}
{"type":"user","message":{"role":"user","content":"wire PaymentForm into checkout\nand run the tests"}}
{"type":"assistant","message":{"id":"msg_1","model":"claude-opus-5-5","content":[{"type":"text","text":"Looking at the checkout page."}],"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":40}}}
{"type":"assistant","message":{"id":"msg_1","model":"claude-opus-5-5","content":[{"type":"tool_use","id":"toolu_1","name":"Edit","input":{"file_path":"/repo/src/CheckoutPage.vue","old_string":"a","new_string":"b"}}],"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":300,"cache_creation_input_tokens":40}}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}
{"type":"file-history-snapshot","snapshot":{}}
not json at all
{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>"}}
{"type":"assistant","message":{"id":"msg_2","model":"claude-opus-5-5","content":[{"type":"text","text":"Running the tests."},{"type":"tool_use","id":"toolu_2","name":"Bash","input":{"command":"npm test","description":"run tests"}}],"usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":400,"cache_creation_input_tokens":0}}}
```

- [ ] **Step 2: Write the failing tests** — `internal/transcript/accumulate_test.go`

```go
package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func fixtureLines(t *testing.T) [][]byte {
	t.Helper()
	data, err := os.ReadFile("testdata/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, l := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			lines = append(lines, l)
		}
	}
	return lines
}

func TestAccumulatorBuildsPreviewAndUsage(t *testing.T) {
	a := newAccumulator()
	for _, l := range fixtureLines(t) {
		a.add(l, true)
	}
	s := a.snapshot()
	want := []Item{
		{Kind: ItemPrompt, Text: "wire PaymentForm into checkout\nand run the tests"},
		{Kind: ItemText, Text: "Looking at the checkout page."},
		{Kind: ItemTool, Text: "Edit CheckoutPage.vue"},
		{Kind: ItemText, Text: "Running the tests."},
		{Kind: ItemTool, Text: "Bash npm test"},
	}
	if !reflect.DeepEqual(s.Items, want) {
		t.Errorf("Items = %+v", s.Items)
	}
	if s.Pending != "Bash npm test" {
		t.Errorf("Pending = %q (the Edit was answered, the Bash wasn't)", s.Pending)
	}
	if s.Prompts != 1 || s.FirstPrompt != "wire PaymentForm into checkout\nand run the tests" {
		t.Errorf("Prompts = %d, FirstPrompt = %q", s.Prompts, s.FirstPrompt)
	}
	// msg_1 is written twice and must be counted once.
	if want := (Usage{Input: 15, Output: 27, CacheRead: 700, CacheWrite: 40}); s.Usage != want {
		t.Errorf("Usage = %+v, want %+v", s.Usage, want)
	}
	if s.Usage.Total() != 782 || !s.Found {
		t.Errorf("Total = %d, Found = %v", s.Usage.Total(), s.Found)
	}
}

func TestSubagentLinesOnlyCountTokens(t *testing.T) {
	a := newAccumulator()
	a.add([]byte(`{"type":"user","message":{"content":"sub prompt"}}`), false)
	a.add([]byte(`{"type":"assistant","message":{"id":"m","content":[{"type":"text","text":"sub"}],"usage":{"output_tokens":100}}}`), false)
	s := a.snapshot()
	if len(s.Items) != 0 || s.Prompts != 0 || s.Usage.Output != 100 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestItemsAreCapped(t *testing.T) {
	a := newAccumulator()
	for i := 0; i < 250; i++ {
		a.add([]byte(fmt.Sprintf(`{"type":"user","message":{"content":"prompt %d"}}`, i)), true)
	}
	s := a.snapshot()
	if len(s.Items) != maxItems || s.Items[maxItems-1].Text != "prompt 249" || s.Prompts != 250 {
		t.Errorf("len %d, last %q, prompts %d", len(s.Items), s.Items[len(s.Items)-1].Text, s.Prompts)
	}
	if s.FirstPrompt != "prompt 0" {
		t.Errorf("FirstPrompt = %q", s.FirstPrompt)
	}
}

func TestSummarize(t *testing.T) {
	long := strings.Repeat("x", 100)
	cases := []struct{ name, input, want string }{
		{"Edit", `{"file_path":"/repo/src/CheckoutPage.vue"}`, "Edit CheckoutPage.vue"},
		{"Read", `{}`, "Read"},
		{"NotebookEdit", `{"notebook_path":"/n/a.ipynb"}`, "NotebookEdit a.ipynb"},
		{"Bash", `{"command":"npm test\n  --watch"}`, "Bash npm test --watch"},
		{"Bash", `{"command":"` + long + `"}`, "Bash " + strings.Repeat("x", 60) + "…"},
		{"Bash", `not json`, "Bash"},
		{"Grep", `{"pattern":"TODO"}`, "Grep TODO"},
		{"Task", `{"prompt":"go"}`, "Task"},
	}
	for _, c := range cases {
		if got := Summarize(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("Summarize(%s, %s) = %q, want %q", c.name, c.input, got, c.want)
		}
	}
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/transcript`
Expected: FAIL, `undefined: newAccumulator`

- [ ] **Step 4: Implement the types** — `internal/transcript/transcript.go`

```go
// Package transcript reads Claude Code conversation files (one JSON record per line)
// and turns them into preview items and token totals.
package transcript

// ItemKind says who an Item comes from.
type ItemKind int

const (
	ItemPrompt ItemKind = iota // something the user typed
	ItemText                   // Claude's reply text
	ItemTool                   // a tool call, summarised
)

// Item is one line of the preview.
type Item struct {
	Kind ItemKind
	Text string
}

// Usage is a token count split by type.
type Usage struct {
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
}

// Total adds up every token type.
func (u Usage) Total() int64 { return u.Input + u.Output + u.CacheRead + u.CacheWrite }

func (u Usage) plus(v Usage) Usage {
	return Usage{u.Input + v.Input, u.Output + v.Output, u.CacheRead + v.CacheRead, u.CacheWrite + v.CacheWrite}
}

// Snapshot is what the board needs to know about a session's conversation.
type Snapshot struct {
	Found       bool   // the conversation file exists
	Items       []Item // the most recent preview items, oldest first
	Pending     string // the tool call still waiting for a result, summarised
	Usage       Usage  // tokens, including subagents
	FirstPrompt string
	Prompts     int
	Subagents   int
}
```

- [ ] **Step 5: Implement the parsing** — `internal/transcript/accumulate.go`

```go
package transcript

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// maxItems is how many preview items a snapshot keeps.
const maxItems = 200

type record struct {
	Type        string          `json:"type"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

type message struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Usage   *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
}

// blocks decodes message content, which is either a plain string or a list of blocks.
func blocks(content json.RawMessage) []block {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return []block{{Type: "text", Text: s}}
	}
	var bs []block
	_ = json.Unmarshal(content, &bs)
	return bs
}

// accumulator folds transcript lines into a Snapshot.
type accumulator struct {
	items        []Item
	usage        map[string]Usage  // by API message ID, so repeated records count once
	pending      map[string]string // tool_use ID → summary, until its result arrives
	pendingOrder []string
	firstPrompt  string
	prompts      int
}

func newAccumulator() *accumulator {
	return &accumulator{usage: map[string]Usage{}, pending: map[string]string{}}
}

// add applies one line. Lines that aren't JSON, or are record types claudeboard doesn't
// use, are ignored. Subagent files (main == false) only contribute tokens.
func (a *accumulator) add(line []byte, main bool) {
	var r record
	if json.Unmarshal(line, &r) != nil || len(r.Message) == 0 {
		return
	}
	var m message
	if json.Unmarshal(r.Message, &m) != nil {
		return
	}
	switch r.Type {
	case "assistant":
		if m.Usage != nil && m.ID != "" {
			a.usage[m.ID] = Usage{
				Input:      m.Usage.InputTokens,
				Output:     m.Usage.OutputTokens,
				CacheRead:  m.Usage.CacheReadInputTokens,
				CacheWrite: m.Usage.CacheCreationInputTokens,
			}
		}
		if !main || r.IsSidechain {
			return
		}
		for _, b := range blocks(m.Content) {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" {
					a.push(Item{Kind: ItemText, Text: t})
				}
			case "tool_use":
				s := Summarize(b.Name, b.Input)
				a.push(Item{Kind: ItemTool, Text: s})
				if b.ID != "" {
					a.pending[b.ID] = s
					a.pendingOrder = append(a.pendingOrder, b.ID)
				}
			}
		}
	case "user":
		if !main || r.IsMeta || r.IsSidechain {
			return
		}
		var texts []string
		for _, b := range blocks(m.Content) {
			switch b.Type {
			case "text":
				if t := strings.TrimSpace(b.Text); t != "" && !isSystemText(t) {
					texts = append(texts, t)
				}
			case "tool_result":
				delete(a.pending, b.ToolUseID)
			}
		}
		if len(texts) == 0 {
			return
		}
		text := strings.Join(texts, "\n")
		a.prompts++
		if a.firstPrompt == "" {
			a.firstPrompt = text
		}
		a.push(Item{Kind: ItemPrompt, Text: text})
	}
}

// isSystemText spots user-role text that Claude Code writes itself: command wrappers,
// reminders and interruption notices.
func isSystemText(t string) bool {
	return strings.HasPrefix(t, "<") || strings.HasPrefix(t, "[Request interrupted")
}

func (a *accumulator) push(it Item) {
	a.items = append(a.items, it)
	if len(a.items) > maxItems {
		a.items = a.items[len(a.items)-maxItems:]
	}
}

// pendingTool is the most recent tool call that has no result yet.
func (a *accumulator) pendingTool() string {
	for i := len(a.pendingOrder) - 1; i >= 0; i-- {
		if s, ok := a.pending[a.pendingOrder[i]]; ok {
			return s
		}
	}
	a.pendingOrder = a.pendingOrder[:0]
	return ""
}

func (a *accumulator) snapshot() Snapshot {
	var u Usage
	for _, v := range a.usage {
		u = u.plus(v)
	}
	return Snapshot{
		Found:       true,
		Items:       append([]Item(nil), a.items...),
		Pending:     a.pendingTool(),
		Usage:       u,
		FirstPrompt: a.firstPrompt,
		Prompts:     a.prompts,
	}
}

// Summarize describes a tool call in a few words: the tool name plus its most telling
// argument (a file's base name, a shortened command, or a search pattern).
func Summarize(name string, input json.RawMessage) string {
	var in map[string]any
	_ = json.Unmarshal(input, &in)
	arg := func(k string) string {
		s, _ := in[k].(string)
		return s
	}
	var detail string
	switch name {
	case "Read", "Edit", "Write", "MultiEdit":
		if p := arg("file_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "NotebookEdit":
		if p := arg("notebook_path"); p != "" {
			detail = filepath.Base(p)
		}
	case "Bash":
		detail = strings.Join(strings.Fields(arg("command")), " ")
		if r := []rune(detail); len(r) > 60 {
			detail = string(r[:60]) + "…"
		}
	case "Grep", "Glob":
		detail = arg("pattern")
	}
	if detail == "" {
		return name
	}
	return name + " " + detail
}
```

- [ ] **Step 6: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit and push**

```bash
git add internal/transcript
git commit -F - <<'EOF'
Add transcript parsing for previews and token totals

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 8: Transcript reader

**Files:**
- Create: `internal/transcript/reader.go`, `internal/transcript/reader_test.go`

**Interfaces:**
- Consumes: the accumulator from Task 7.
- Produces:
  - `func ProjectsDir() string`
  - `type Reader`
  - `func NewReader(projectsDir, sessionID string) *Reader`
  - `func (r *Reader) Update() (Snapshot, error)`. Not safe for concurrent use; the board gives each refresh exclusive ownership of its readers.

- [ ] **Step 1: Write the failing tests** — `internal/transcript/reader_test.go`

```go
package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const promptLine = `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n"

func assistantLine(id string, out int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":%d}}}`+"\n", id, out)
}

// writeSession writes projects/-work-api/<id>.jsonl and returns its path.
func writeSession(t *testing.T, projects, id, content string) string {
	t.Helper()
	dir := filepath.Join(projects, "-work-api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func mustUpdate(t *testing.T, r *Reader) Snapshot {
	t.Helper()
	s, err := r.Update()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReaderMissingSession(t *testing.T) {
	if s := mustUpdate(t, NewReader(t.TempDir(), "nope")); s.Found {
		t.Errorf("Found = true for a missing session")
	}
}

func TestReaderReadsIncrementally(t *testing.T) {
	projects := t.TempDir()
	path := writeSession(t, projects, "s1", promptLine+assistantLine("m1", 10))
	r := NewReader(projects, "s1")
	if s := mustUpdate(t, r); !s.Found || s.Prompts != 1 || s.Usage.Output != 10 {
		t.Fatalf("first read = %+v", s)
	}
	appendTo(t, path, assistantLine("m2", 5))
	if s := mustUpdate(t, r); s.Usage.Output != 15 {
		t.Fatalf("after append, Output = %d, want 15", s.Usage.Output)
	}
	line := assistantLine("m3", 7)
	appendTo(t, path, line[:20]) // half-written line
	if s := mustUpdate(t, r); s.Usage.Output != 15 {
		t.Fatalf("half a line was counted: Output = %d", s.Usage.Output)
	}
	appendTo(t, path, line[20:])
	if s := mustUpdate(t, r); s.Usage.Output != 22 {
		t.Fatalf("completed line not counted: Output = %d, want 22", s.Usage.Output)
	}
}

func TestReaderStartsOverWhenFileShrinks(t *testing.T) {
	projects := t.TempDir()
	path := writeSession(t, projects, "s1", promptLine+promptLine)
	r := NewReader(projects, "s1")
	if s := mustUpdate(t, r); s.Prompts != 2 {
		t.Fatalf("Prompts = %d", s.Prompts)
	}
	if err := os.WriteFile(path, []byte(promptLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := mustUpdate(t, r); s.Prompts != 1 {
		t.Errorf("after the file shrank, Prompts = %d, want 1", s.Prompts)
	}
}

func TestReaderCountsSubagents(t *testing.T) {
	projects := t.TempDir()
	path := writeSession(t, projects, "s1", promptLine+assistantLine("m1", 10))
	subDir := filepath.Join(strings.TrimSuffix(path, ".jsonl"), "subagents")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "agent-a.jsonl"), []byte(assistantLine("sub1", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	s := mustUpdate(t, NewReader(projects, "s1"))
	if s.Subagents != 1 || s.Usage.Output != 110 || len(s.Items) != 2 {
		t.Errorf("snapshot = %+v (want 1 subagent, 110 output tokens, 2 items from the main file)", s)
	}
}

func TestReaderHandlesHugeLines(t *testing.T) {
	projects := t.TempDir()
	huge := strings.Repeat("x", 3<<20)
	line := fmt.Sprintf(`{"type":"assistant","message":{"id":"big","content":[{"type":"text","text":%q}],"usage":{"output_tokens":9}}}`+"\n", huge)
	writeSession(t, projects, "s1", promptLine+line)
	s := mustUpdate(t, NewReader(projects, "s1"))
	if s.Usage.Output != 9 || len(s.Items) != 2 || len(s.Items[1].Text) != len(huge) {
		t.Errorf("huge line not read: output %d, %d items", s.Usage.Output, len(s.Items))
	}
}

func TestProjectsDir(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/x")
	if got := ProjectsDir(); got != "/x/projects" {
		t.Errorf("ProjectsDir = %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/Users/tester")
	if got := ProjectsDir(); got != "/Users/tester/.claude/projects" {
		t.Errorf("ProjectsDir = %q", got)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/transcript`
Expected: FAIL, `undefined: NewReader`

- [ ] **Step 3: Implement** — `internal/transcript/reader.go`

```go
package transcript

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ProjectsDir is where Claude Code keeps conversations: $CLAUDE_CONFIG_DIR/projects,
// or ~/.claude/projects.
func ProjectsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// Reader follows one session's conversation files, reading only what was appended
// since the last Update.
type Reader struct {
	projectsDir string
	sessionID   string
	mainPath    string
	files       map[string]*tail
	acc         *accumulator
}

// tail is how far a file has been read, plus an incomplete last line.
type tail struct {
	offset  int64
	partial []byte
}

// NewReader returns a Reader for sessionID. Nothing is read until Update.
func NewReader(projectsDir, sessionID string) *Reader {
	return &Reader{projectsDir: projectsDir, sessionID: sessionID, files: map[string]*tail{}, acc: newAccumulator()}
}

// Update reads new lines from the main file and any subagent files and returns the
// result. A session whose file doesn't exist yet gives a Snapshot with Found false.
func (r *Reader) Update() (Snapshot, error) {
	if r.mainPath == "" {
		matches, _ := filepath.Glob(filepath.Join(r.projectsDir, "*", r.sessionID+".jsonl"))
		if len(matches) == 0 {
			return Snapshot{}, nil
		}
		r.mainPath = matches[0]
	}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(r.mainPath, ".jsonl"), "subagents", "*.jsonl"))
	for attempt := 0; attempt < 2; attempt++ {
		shrunk, err := r.readAll(subs)
		if err != nil {
			return Snapshot{}, err
		}
		if !shrunk {
			break
		}
		// A file got shorter (rewritten or truncated): start over from scratch.
		r.files, r.acc = map[string]*tail{}, newAccumulator()
	}
	s := r.acc.snapshot()
	s.Subagents = len(subs)
	return s, nil
}

func (r *Reader) readAll(subs []string) (shrunk bool, err error) {
	for _, p := range append([]string{r.mainPath}, subs...) {
		shrunk, err := r.read(p, p == r.mainPath)
		if err != nil || shrunk {
			return shrunk, err
		}
	}
	return false, nil
}

// read feeds the complete lines appended to path since the last call to the
// accumulator. It reports whether the file is now shorter than what was already read.
func (r *Reader) read(path string, main bool) (shrunk bool, err error) {
	t := r.files[path]
	if t == nil {
		t = &tail{}
		r.files[path] = t
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	switch {
	case info.Size() < t.offset:
		return true, nil
	case info.Size() == t.offset:
		return false, nil
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}
	t.offset += int64(len(data))
	buf := append(t.partial, data...)
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		if line := bytes.TrimSpace(buf[:i]); len(line) > 0 {
			r.acc.add(line, main)
		}
		buf = buf[i+1:]
	}
	t.partial = append([]byte(nil), buf...)
	return false, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit and push**

```bash
git add internal/transcript
git commit -F - <<'EOF'
Add incremental transcript reader with subagent support

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 9: Launching sessions

**Files:**
- Create: `internal/launch/launch.go`, `internal/launch/launch_test.go`

**Interfaces:**
- Consumes:
  - `claudecli.LaunchOpts`, `claudecli.Session` (Task 5)
  - `config.Config` (Task 2)
  - `store.Store`, `store.State`, `EnsureWorkflow`, `NextSessionNumber`, `SessionName`, `WorktreeName`, `UpsertSession`, `QuickSlug` (Tasks 3–4)
- Produces:
  - `type Launcher interface { Launch(ctx, claudecli.LaunchOpts) (string, error); List(ctx) ([]claudecli.Session, error) }`
  - `type Request struct { Workflow, Dir, Repo string; Worktree bool; Prompt string }`
  - `func Run(ctx context.Context, l Launcher, st *store.Store, cfg config.Config, req Request, now time.Time) (store.Session, error)`
  - `func DirBusy(st store.State, live []claudecli.Session, dir string) bool`

- [ ] **Step 1: Write the failing tests** — `internal/launch/launch_test.go`

```go
package launch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

var now = time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)

// stubLauncher stands in for claudecli.CLI: launched sessions show up in List.
type stubLauncher struct {
	live     []claudecli.Session
	launched []claudecli.LaunchOpts
	err      error
}

func (s *stubLauncher) Launch(_ context.Context, o claudecli.LaunchOpts) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.launched = append(s.launched, o)
	id := fmt.Sprintf("%08d", len(s.launched))
	s.live = append(s.live, claudecli.Session{ShortID: id, SessionID: "sess-" + id, Name: o.Name, Kind: claudecli.KindBackground, State: "blocked"})
	return id, nil
}

func (s *stubLauncher) List(context.Context) ([]claudecli.Session, error) { return s.live, nil }

func TestRunRecordsSession(t *testing.T) {
	l, st := &stubLauncher{}, store.Open(t.TempDir())
	cfg := config.Config{Defaults: config.Defaults{Model: "opus", IDE: true}}
	sess, err := Run(context.Background(), l, st, cfg, Request{Workflow: "Payments V2", Dir: "/work/api", Repo: "api"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Name != "cb-payments-v2-1" || sess.ShortID != "00000001" || sess.SessionID != "sess-00000001" {
		t.Errorf("session = %+v", sess)
	}
	if o := l.launched[0]; o.Dir != "/work/api" || o.Model != "opus" || !o.IDE || o.Worktree != "" || o.Prompt != "" {
		t.Errorf("launch options = %+v", o)
	}
	state, _, _ := st.Load()
	if wf := state.Workflow("payments-v2"); wf == nil || wf.LastRepo != "api" || wf.Name != "Payments V2" {
		t.Errorf("workflow = %+v", wf)
	}
	if state.Session("sess-00000001") == nil {
		t.Error("session not recorded")
	}
}

func TestRunNumbersSessionsAndAddsWorktrees(t *testing.T) {
	l, st := &stubLauncher{}, store.Open(t.TempDir())
	ctx := context.Background()
	if _, err := Run(ctx, l, st, config.Config{}, Request{Workflow: "w", Dir: "/r"}, now); err != nil {
		t.Fatal(err)
	}
	sess, err := Run(ctx, l, st, config.Config{}, Request{Workflow: "w", Dir: "/r", Worktree: true, Prompt: "go"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Name != "cb-w-2" || sess.Worktree != "w-2" || l.launched[1].Worktree != "w-2" || l.launched[1].Prompt != "go" {
		t.Errorf("second session = %+v, opts %+v", sess, l.launched[1])
	}
}

func TestRunDefaultsToQuick(t *testing.T) {
	sess, err := Run(context.Background(), &stubLauncher{}, store.Open(t.TempDir()), config.Config{}, Request{Dir: "/s"}, now)
	if err != nil || sess.Name != "cb-quick-1" {
		t.Fatalf("session = %+v, %v", sess, err)
	}
}

func TestRunAvoidsNamesClaudeAlreadyUses(t *testing.T) {
	l := &stubLauncher{live: []claudecli.Session{{Name: "cb-quick-4"}}}
	sess, err := Run(context.Background(), l, store.Open(t.TempDir()), config.Config{}, Request{Dir: "/s"}, now)
	if err != nil || sess.Name != "cb-quick-5" {
		t.Fatalf("session = %+v, %v", sess, err)
	}
}

func TestRunLeavesStateAloneOnError(t *testing.T) {
	st := store.Open(t.TempDir())
	_, err := Run(context.Background(), &stubLauncher{err: errors.New("boom")}, st, config.Config{}, Request{Workflow: "w", Dir: "/r"}, now)
	if err == nil {
		t.Fatal("want an error")
	}
	if state, _, _ := st.Load(); len(state.Workflows) != 0 || len(state.Sessions) != 0 {
		t.Errorf("state changed despite the failed launch: %+v", state)
	}
}

func TestDirBusy(t *testing.T) {
	st := store.State{Sessions: []store.Session{
		{SessionID: "a", Dir: "/w"},
		{SessionID: "b", Dir: "/w", Worktree: "x"},
		{SessionID: "c", Dir: "/v"},
	}}
	live := []claudecli.Session{
		{SessionID: "a", State: "working"},
		{SessionID: "b", State: "done"},
		{SessionID: "c", State: "stopped"},
	}
	for dir, want := range map[string]bool{"/w": true, "/w/.claude/worktrees/x": true, "/v": false, "/z": false} {
		if got := DirBusy(st, live, dir); got != want {
			t.Errorf("DirBusy(%q) = %v, want %v", dir, got, want)
		}
	}
	live[0].State = "failed"
	if DirBusy(st, live, "/w") {
		t.Error("a failed session shouldn't count as busy")
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/launch`
Expected: FAIL, `undefined: Run`

- [ ] **Step 3: Implement** — `internal/launch/launch.go`

```go
// Package launch starts a new claudeboard session and records it in the store. The
// board and `claudeboard new` both use it.
package launch

import (
	"context"
	"path/filepath"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// Launcher is the part of claudecli.CLI that launching needs.
type Launcher interface {
	Launch(ctx context.Context, o claudecli.LaunchOpts) (string, error)
	List(ctx context.Context) ([]claudecli.Session, error)
}

// Request describes the session to start.
type Request struct {
	Workflow string // workflow name; empty means the quick workflow
	Dir      string // folder to run in
	Repo     string // how the folder is shown: a config repo name, "scratch", or the path
	Worktree bool
	Prompt   string // empty: start idle and wait for the user
}

// How long Run waits for a new session to show up in `claude agents`.
var (
	pollInterval = 300 * time.Millisecond
	pollAttempts = 10
)

// Run starts the session and records it. It holds the store lock for the whole launch,
// so two launches can't pick the same session number, and nothing is recorded if
// Claude refuses to start (for example in an untrusted folder; see
// claudecli.IsUntrusted).
func Run(ctx context.Context, l Launcher, st *store.Store, cfg config.Config, req Request, now time.Time) (store.Session, error) {
	workflow := req.Workflow
	if workflow == "" {
		workflow = store.QuickSlug
	}
	var liveNames []string
	if live, err := l.List(ctx); err == nil {
		for _, s := range live {
			liveNames = append(liveNames, s.Name)
		}
	}
	var sess store.Session
	err := st.Update(func(s *store.State) error {
		wf, err := s.EnsureWorkflow(workflow, now)
		if err != nil {
			return err
		}
		n := s.NextSessionNumber(wf.Slug, liveNames...)
		sess = store.Session{Name: store.SessionName(wf.Slug, n), Workflow: wf.Slug, Repo: req.Repo, Dir: req.Dir, CreatedAt: now}
		if req.Worktree {
			sess.Worktree = store.WorktreeName(wf.Slug, n)
		}
		shortID, err := l.Launch(ctx, claudecli.LaunchOpts{
			Dir:            req.Dir,
			Name:           sess.Name,
			Worktree:       sess.Worktree,
			Model:          cfg.Defaults.Model,
			PermissionMode: cfg.Defaults.PermissionMode,
			IDE:            cfg.Defaults.IDE,
			Prompt:         req.Prompt,
		})
		if err != nil {
			return err
		}
		sess.ShortID = shortID
		sess.SessionID = resolveSessionID(ctx, l, shortID)
		wf.LastRepo = req.Repo
		s.UpsertSession(sess)
		return nil
	})
	if err != nil {
		return store.Session{}, err
	}
	return sess, nil
}

// resolveSessionID waits for the new session to be listed and returns its full ID, or
// "" if it doesn't appear in time (the board fills it in later by name).
func resolveSessionID(ctx context.Context, l Launcher, shortID string) string {
	for i := 0; i < pollAttempts; i++ {
		if live, err := l.List(ctx); err == nil {
			for _, s := range live {
				if s.ShortID == shortID && s.SessionID != "" {
					return s.SessionID
				}
			}
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(pollInterval):
		}
	}
	return ""
}

// DirBusy reports whether a claudeboard session that is neither stopped nor failed
// already runs in dir. That is when the board offers a worktree.
func DirBusy(st store.State, live []claudecli.Session, dir string) bool {
	states := make(map[string]string, len(live))
	for _, l := range live {
		states[l.SessionID] = l.State
	}
	dir = filepath.Clean(dir)
	for _, s := range st.Sessions {
		if filepath.Clean(s.WorkDir()) != dir {
			continue
		}
		if state, ok := states[s.SessionID]; ok && state != "stopped" && state != "failed" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit and push**

```bash
git add internal/launch
git commit -F - <<'EOF'
Add session launching shared by the board and CLI

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 10: `claudeboard new`

**Files:**
- Create: `cmd/claudeboard/new.go`, `cmd/claudeboard/new_test.go`
- Modify: `cmd/claudeboard/main.go`. Replace `run` and add `usage`.

**Interfaces:**
- Consumes: `config.Home`, `config.Load`, `Config.ResolveRepo`, `Config.RepoName`, `launch.Run`, `claudecli.New`, `claudecli.IsUntrusted`, `CLI.AttachCmd`, `store.Open`.
- Produces: `parseNewArgs(args []string) (newArgs, error)`, `runNew(args []string, stdout, stderr io.Writer) int`, `const usage`, `const newUsage`.

- [ ] **Step 1: Write the failing tests** — `cmd/claudeboard/new_test.go`

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseNewArgs(t *testing.T) {
	cases := []struct {
		args []string
		want newArgs
	}{
		{nil, newArgs{}},
		{[]string{"-w", "payments", "-r", "api", "--worktree", "fix", "the", "bug"},
			newArgs{workflow: "payments", repo: "api", worktree: true, prompt: "fix the bug"}},
		{[]string{"fix", "-w", "x"}, newArgs{prompt: "fix -w x"}}, // flags after the prompt are prompt text
	}
	for _, c := range cases {
		got, err := parseNewArgs(c.args)
		if err != nil || got != c.want {
			t.Errorf("parseNewArgs(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	if _, err := parseNewArgs([]string{"-x"}); err == nil {
		t.Error("unknown flag should fail")
	}
}

func TestNewHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"new", "-h"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "usage: claudeboard new") {
		t.Errorf("code %d, stdout %q", code, out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"bogus"}, &out, &errOut); code != 2 || !strings.Contains(errOut.String(), "usage: claudeboard") {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/claudeboard`
Expected: FAIL, `undefined: newArgs`

- [ ] **Step 3: Implement** — `cmd/claudeboard/new.go`

```go
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
```

- [ ] **Step 4: Replace `run` in `cmd/claudeboard/main.go`** (keep `main`, `version` and `buildVersion` as they are)

```go
const usage = `usage: claudeboard [command]

  claudeboard              open the board
  claudeboard new …        start a session (claudeboard new -h for options)
  claudeboard --version    print the version
`

// run dispatches subcommands and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "claudeboard: the board isn't built yet")
		return 1
	}
	switch args[0] {
	case "--version", "-v", "version":
		fmt.Fprintln(stdout, "claudeboard", buildVersion())
		return 0
	case "new":
		return runNew(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "claudeboard: unknown command %q\n\n%s", args[0], usage)
	return 2
}
```

- [ ] **Step 5: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Try it for real** (no tokens: no prompt is sent)

Run `claudeboard new` in a pseudo-terminal, press Ctrl+Z after Claude opens, and check what it recorded. This uses a throwaway home and this repo, which is already trusted.

```bash
S=/private/tmp/claude-502/-Users-harrisferguson-organizer/28a97ac4-b213-4024-84d5-6b1e97e06db0/scratchpad/new-check
mkdir -p "$S" && go build -o "$S/claudeboard" ./cmd/claudeboard
export CLAUDEBOARD_HOME="$S/home"
python3 - "$S/claudeboard" <<'PY'
import os, pty, select, sys, time
pid, fd = pty.fork()
if pid == 0:
    os.execvp(sys.argv[1], [sys.argv[1], "new", "-w", "smoke", "-r", "."])
end = time.time() + 8
while time.time() < end:          # let Claude draw, draining its output
    if select.select([fd], [], [], 0.1)[0]:
        try: os.read(fd, 65536)
        except OSError: break
os.write(fd, b"\x1a")             # ctrl+z: leave the session running
_, st = os.waitpid(pid, 0)
print("claudeboard new exited with", os.waitstatus_to_exitcode(st))
PY
python3 -c "import json;s=json.load(open('$CLAUDEBOARD_HOME/state.json'))['sessions'];print(s);open('$S/id','w').write(s[0]['short_id'])"
```

Expected: exit code 0, and one session named `cb-smoke-1` with a short ID and a session ID. Clean up:

```bash
id=$(cat "$S/id"); claude stop "$id"; claude rm "$id"; rm -rf "$S"; unset CLAUDEBOARD_HOME
```

- [ ] **Step 7: Commit and push**

```bash
git add cmd/claudeboard
git commit -F - <<'EOF'
Add claudeboard new for starting sessions from any terminal

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

- [ ] **Step 8: Checkpoint: dev-experience critic.** Dispatch a fresh critic agent (see "Critic checkpoints" at the end of this plan) on the repository as it stands. Fix everything it raises that is in scope until it scores at least 8/10.

---

### Task 11: Board formatting and rows

**Files:**
- Create: `internal/board/format.go`, `internal/board/rows.go`, `internal/board/format_test.go`, `internal/board/rows_test.go`, `internal/board/fixture_test.go`

**Interfaces:**
- Consumes:
  - `claudecli.Session`, `claudecli.KindInteractive`
  - `status.From`, `status.Status`, `status.Kind` constants
  - `store.State`, `store.Workflow`, `store.Session`
  - `transcript.Snapshot`, `transcript.Usage`
- Produces:
  - helpers: `formatTokens(int64) string`, `tokensOrBlank(int64) string`, `truncate(string, int) string`, `pad(string, int) string`, `fit(left, right string, width int) string`, `clean(string) string`, `firstLine(string) string`, `repoLabel(string) string`, `sessionLabel(transcript.Snapshot) string`, `plural(int, string) string`, `clamp(v, lo, hi int) int`
  - `type RowKind int` with `RowWorkflow, RowSession, RowOtherHeader, RowOther`
  - `type Row struct { Kind RowKind; Workflow store.Workflow; Session store.Session; Live *claudecli.Session; Snap transcript.Snapshot; Status status.Status; Tokens int64; Stats Counts; Sessions int; Collapsed bool }`
  - `func (r Row) Key() string`, `func (r Row) ShortID() string`, `func (r Row) Name() string`, `func (r Row) Interactive() bool`
  - `type Counts struct { Working, NeedsYou int; Tokens int64 }`
  - `func BuildRows(st store.State, live []claudecli.Session, snaps map[string]transcript.Snapshot, collapsed map[string]bool, showArchived bool) ([]Row, Counts)`
  - test helper `fixture() (store.State, []claudecli.Session, map[string]transcript.Snapshot)` in `fixture_test.go`. Its rows are: 0 workflow payments-v2, 1 cb-payments-v2-1 (working), 2 cb-payments-v2-2 (needs you), 3 workflow q4-search, 4 cb-q4-search-1 (done), 5 workflow quick, 6 cb-quick-1 (new), 7 "Other sessions", 8 arbybot-8d (interactive).

- [ ] **Step 1: Add the x/ansi dependency**

```bash
go get github.com/charmbracelet/x/ansi@v0.11.9
```

- [ ] **Step 2: Write the shared fixture** — `internal/board/fixture_test.go`

```go
package board

import (
	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// fixture is three workflows and an interactive session started elsewhere, matching
// the mockup in the spec. Totals: payments-v2 53k, q4-search 12k, everything 65k.
func fixture() (store.State, []claudecli.Session, map[string]transcript.Snapshot) {
	st := store.State{
		Version: 1,
		Workflows: []store.Workflow{
			{Slug: "payments-v2", Name: "payments-v2", LastRepo: "api"},
			{Slug: "q4-search", Name: "q4-search", LastRepo: "api"},
			{Slug: "quick", Name: "quick", LastRepo: "scratch"},
		},
		Sessions: []store.Session{
			{SessionID: "id-1", ShortID: "aaaa0001", Name: "cb-payments-v2-1", Workflow: "payments-v2", Repo: "api", Dir: "/work/api"},
			{SessionID: "id-2", ShortID: "aaaa0002", Name: "cb-payments-v2-2", Workflow: "payments-v2", Repo: "web", Dir: "/work/web"},
			{SessionID: "id-3", ShortID: "aaaa0003", Name: "cb-q4-search-1", Workflow: "q4-search", Repo: "api", Dir: "/work/api"},
			{SessionID: "id-4", ShortID: "aaaa0004", Name: "cb-quick-1", Workflow: "quick", Repo: "scratch", Dir: "/home/.claudeboard/scratch"},
		},
	}
	live := []claudecli.Session{
		{PID: 1, ShortID: "aaaa0001", SessionID: "id-1", Name: "cb-payments-v2-1", Kind: claudecli.KindBackground, Status: "busy", State: "working"},
		{PID: 2, ShortID: "aaaa0002", SessionID: "id-2", Name: "cb-payments-v2-2", Kind: claudecli.KindBackground, Status: "waiting", State: "blocked", WaitingFor: "permission prompt"},
		{PID: 3, ShortID: "aaaa0003", SessionID: "id-3", Name: "cb-q4-search-1", Kind: claudecli.KindBackground, Status: "idle", State: "done"},
		{PID: 4, ShortID: "aaaa0004", SessionID: "id-4", Name: "cb-quick-1", Kind: claudecli.KindBackground, Status: "idle", State: "blocked"},
		{PID: 5, SessionID: "id-other", Name: "arbybot-8d", Kind: claudecli.KindInteractive, Status: "busy"},
	}
	snaps := map[string]transcript.Snapshot{
		"id-1": {Found: true, FirstPrompt: "wire the new AfterPay button into checkout", Prompts: 1,
			Usage: transcript.Usage{Input: 1000, Output: 5000, CacheRead: 35000}},
		"id-2": {Found: true, FirstPrompt: "hook PaymentForm into checkout", Prompts: 1, Pending: "Bash npm test",
			Usage: transcript.Usage{Input: 1200, Output: 8400, CacheRead: 2100, CacheWrite: 300},
			Items: []transcript.Item{
				{Kind: transcript.ItemPrompt, Text: "hook PaymentForm into checkout"},
				{Kind: transcript.ItemTool, Text: "Edit CheckoutPage.vue"},
				{Kind: transcript.ItemTool, Text: "Bash npm test"},
				{Kind: transcript.ItemText, Text: "Wired it in. Running the tests to check checkout still works."},
			}},
		"id-3": {Found: true, FirstPrompt: "plan the search rewrite", Prompts: 1,
			Usage: transcript.Usage{Output: 2000, CacheRead: 10000}},
		"id-4": {},
	}
	return st, live, snaps
}
```

- [ ] **Step 3: Write the failing format tests** — `internal/board/format_test.go`

```go
package board

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

func TestFormatTokens(t *testing.T) {
	cases := map[int64]string{0: "0", 950: "950", 1234: "1.2k", 9949: "9.9k", 9950: "10k", 41234: "41k",
		999_499: "999k", 999_500: "1.00M", 1_240_000: "1.24M"}
	for n, want := range cases {
		if got := formatTokens(n); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", n, got, want)
		}
	}
	if tokensOrBlank(0) != "" || tokensOrBlank(5) != "5" {
		t.Error("tokensOrBlank")
	}
}

func TestFitAndPad(t *testing.T) {
	if got := fit("abc", "12k", 10); got != "abc    12k" {
		t.Errorf("fit short = %q", got)
	}
	if got := fit("abcdefghijkl", "12k", 10); got != "abcde… 12k" {
		t.Errorf("fit long = %q", got)
	}
	if got := fit("abc", "", 5); got != "abc  " {
		t.Errorf("fit no right = %q", got)
	}
	for _, s := range []string{"日本語のテキスト", "😀😀😀😀", "plain text here"} {
		if w := ansi.StringWidth(pad(s, 7)); w != 7 {
			t.Errorf("pad(%q, 7) is %d cells wide", s, w)
		}
	}
}

func TestClean(t *testing.T) {
	if got := clean("a\tb\r\n\x1b[31mred\x1b[0m"); got != "a    b\nred" {
		t.Errorf("clean = %q", got)
	}
}

func TestLabels(t *testing.T) {
	if got := sessionLabel(transcript.Snapshot{FirstPrompt: "  fix the bug\nmore detail"}); got != "fix the bug" {
		t.Errorf("sessionLabel = %q", got)
	}
	if got := sessionLabel(transcript.Snapshot{}); got != "new session" {
		t.Errorf("empty sessionLabel = %q", got)
	}
	if repoLabel("/work/api") != "api" || repoLabel("api") != "api" {
		t.Error("repoLabel")
	}
	if plural(1, "session") != "1 session" || plural(3, "session") != "3 sessions" {
		t.Error("plural")
	}
}
```

- [ ] **Step 4: Write the failing rows tests** — `internal/board/rows_test.go`

```go
package board

import (
	"testing"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
)

func kinds(rows []Row) []RowKind {
	out := make([]RowKind, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func TestBuildRows(t *testing.T) {
	st, live, snaps := fixture()
	rows, counts := BuildRows(st, live, snaps, map[string]bool{}, false)
	want := []RowKind{RowWorkflow, RowSession, RowSession, RowWorkflow, RowSession, RowWorkflow, RowSession, RowOtherHeader, RowOther}
	if got := kinds(rows); len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i, k := range want {
		if rows[i].Kind != k {
			t.Fatalf("row %d is %v, want %v", i, rows[i].Kind, k)
		}
	}
	wantStatus := map[int]status.Status{
		1: {Kind: status.Working},
		2: {Kind: status.NeedsYou, Detail: "permission prompt"},
		4: {Kind: status.Done},
		6: {Kind: status.New},
		8: {Kind: status.Working},
	}
	for i, s := range wantStatus {
		if rows[i].Status != s {
			t.Errorf("row %d status = %+v, want %+v", i, rows[i].Status, s)
		}
	}
	if counts != (Counts{Working: 1, NeedsYou: 1, Tokens: 65000}) {
		t.Errorf("counts = %+v (the Other session must not count)", counts)
	}
	if r := rows[0]; r.Tokens != 53000 || r.Sessions != 2 || r.Stats.NeedsYou != 1 {
		t.Errorf("payments-v2 row = %+v", r)
	}
	if rows[8].Name() != "arbybot-8d" || !rows[8].Interactive() || rows[2].ShortID() != "aaaa0002" {
		t.Error("row helpers")
	}
}

func TestBuildRowsCollapsedAndArchived(t *testing.T) {
	st, live, snaps := fixture()
	rows, counts := BuildRows(st, live, snaps, map[string]bool{"payments-v2": true}, false)
	if len(rows) != 7 || !rows[0].Collapsed || counts.NeedsYou != 1 {
		t.Errorf("collapsed: %d rows, counts %+v", len(rows), counts)
	}
	st.Workflows[1].Archived = true
	rows, counts = BuildRows(st, live, snaps, map[string]bool{}, false)
	for _, r := range rows {
		if r.Workflow.Slug == "q4-search" || (r.Kind == RowOther && r.Live.SessionID == "id-3") {
			t.Errorf("archived workflow still shown: %+v", r)
		}
	}
	if counts.Tokens != 53000 {
		t.Errorf("archived tokens still counted: %+v", counts)
	}
	rows, _ = BuildRows(st, live, snaps, map[string]bool{}, true)
	if len(rows) != 9 {
		t.Errorf("showArchived: %d rows, want 9", len(rows))
	}
}

func TestBuildRowsUnlistedAndUnresolved(t *testing.T) {
	st, live, snaps := fixture()
	rows, _ := BuildRows(st, live[1:], snaps, map[string]bool{}, false) // id-1 not listed
	if rows[1].Status.Kind != status.Stopped || rows[1].Live != nil {
		t.Errorf("unlisted session = %+v", rows[1])
	}
	st.Sessions[0].SessionID = "" // launched, ID not resolved yet: match by short ID
	rows, _ = BuildRows(st, live, snaps, map[string]bool{}, false)
	if rows[1].Live == nil || rows[1].Status.Kind != status.Working || len(rows) != 9 {
		t.Errorf("short-ID match failed: %+v (%d rows)", rows[1], len(rows))
	}
}
```

- [ ] **Step 5: Run them to see them fail**

Run: `go test ./internal/board`
Expected: FAIL, `undefined: formatTokens` (and others)

- [ ] **Step 6: Implement the helpers** — `internal/board/format.go`

```go
package board

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// maxLabel is the most of a first prompt shown as a row label.
const maxLabel = 40

// formatTokens renders a token count compactly: 950, 1.2k, 41k, 1.24M.
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 9950:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 999_500:
		return fmt.Sprintf("%dk", (n+500)/1000)
	default:
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
}

// tokensOrBlank is formatTokens, but empty for zero so idle rows stay quiet.
func tokensOrBlank(n int64) string {
	if n == 0 {
		return ""
	}
	return formatTokens(n)
}

// truncate shortens s to at most w terminal cells, ending in "…" when it cuts.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// pad truncates or space-pads s to exactly w cells.
func pad(s string, w int) string {
	s = truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// fit lays out left and right on one line of exactly width cells, cutting left first.
func fit(left, right string, width int) string {
	if right == "" {
		return pad(left, width)
	}
	rw := ansi.StringWidth(right) + 1
	if width-rw < 1 {
		return pad(left, width)
	}
	return pad(left, width-rw) + " " + right
}

// clean makes transcript text safe to draw: no escape codes, tabs or carriage returns.
func clean(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\t", "    ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// repoLabel shows configured repos by name and other folders by their last element.
func repoLabel(repo string) string {
	if filepath.IsAbs(repo) {
		return filepath.Base(repo)
	}
	return repo
}

// sessionLabel is the start of the first prompt, or "new session".
func sessionLabel(s transcript.Snapshot) string {
	if l := firstLine(clean(s.FirstPrompt)); l != "" {
		return truncate(l, maxLabel)
	}
	return "new session"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }
```

- [ ] **Step 7: Implement the rows** — `internal/board/rows.go`

```go
package board

import (
	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// RowKind is what a line in the left pane shows.
type RowKind int

const (
	RowWorkflow    RowKind = iota // a workflow heading
	RowSession                    // a claudeboard session
	RowOtherHeader                // the "Other sessions" heading; never selected
	RowOther                      // a session claudeboard didn't start
)

// Row is one line of the left pane plus everything the preview needs about it.
type Row struct {
	Kind      RowKind
	Workflow  store.Workflow     // RowWorkflow, and the parent of a RowSession
	Session   store.Session      // RowSession
	Live      *claudecli.Session // what Claude lists; nil when it doesn't list the session
	Snap      transcript.Snapshot
	Status    status.Status
	Tokens    int64  // the session's total, or the workflow's
	Stats     Counts // RowWorkflow: its sessions
	Sessions  int    // RowWorkflow: how many sessions it has
	Collapsed bool   // RowWorkflow
}

// Counts summarises sessions for the header and workflow rows.
type Counts struct {
	Working  int
	NeedsYou int
	Tokens   int64
}

// Key identifies a row across refreshes so the cursor stays on it.
func (r Row) Key() string {
	switch r.Kind {
	case RowWorkflow:
		return "w:" + r.Workflow.Slug
	case RowSession:
		if r.Session.SessionID != "" {
			return "s:" + r.Session.SessionID
		}
		return "s:" + r.Session.Name
	case RowOtherHeader:
		return "other"
	}
	return "o:" + r.Live.SessionID
}

// ShortID is the ID `claude attach`, `stop` and `rm` take.
func (r Row) ShortID() string {
	if r.Live != nil && r.Live.ShortID != "" {
		return r.Live.ShortID
	}
	return r.Session.ShortID
}

// Name is how the row's session is referred to in messages.
func (r Row) Name() string {
	switch {
	case r.Kind == RowSession:
		return r.Session.Name
	case r.Kind == RowWorkflow:
		return r.Workflow.Name
	case r.Live == nil:
		return ""
	case r.Live.Name != "":
		return r.Live.Name
	}
	return r.Live.ShortID
}

// Interactive reports whether the row is a session running in someone's terminal,
// which claudeboard can't open, stop or delete.
func (r Row) Interactive() bool {
	return r.Kind == RowOther && r.Live.Kind == claudecli.KindInteractive
}

// BuildRows lays out the left pane: each workflow (in creation order) followed by its
// sessions unless collapsed, then sessions Claude lists that claudeboard didn't start.
// Archived workflows are left out unless showArchived. Counts covers the visible
// claudeboard sessions only.
func BuildRows(st store.State, live []claudecli.Session, snaps map[string]transcript.Snapshot, collapsed map[string]bool, showArchived bool) ([]Row, Counts) {
	byID := map[string]*claudecli.Session{}
	byShort := map[string]*claudecli.Session{}
	for i := range live {
		l := &live[i]
		if l.SessionID != "" {
			byID[l.SessionID] = l
		}
		if l.ShortID != "" {
			byShort[l.ShortID] = l
		}
	}
	known := map[string]bool{}
	var rows []Row
	var total Counts
	for _, wf := range st.Workflows {
		head := Row{Kind: RowWorkflow, Workflow: wf, Collapsed: collapsed[wf.Slug]}
		var children []Row
		for _, s := range st.Sessions {
			if s.Workflow != wf.Slug {
				continue
			}
			l := byID[s.SessionID]
			if l == nil && s.SessionID == "" {
				l = byShort[s.ShortID]
			}
			snapID := s.SessionID
			if l != nil {
				known[l.SessionID] = true
				snapID = l.SessionID
			}
			known[s.SessionID] = true
			snap := snaps[snapID]
			r := Row{Kind: RowSession, Workflow: wf, Session: s, Live: l, Snap: snap,
				Status: status.From(l, snap.Prompts > 0), Tokens: snap.Usage.Total()}
			head.Sessions++
			head.Tokens += r.Tokens
			head.Stats.Tokens += r.Tokens
			switch r.Status.Kind {
			case status.Working:
				head.Stats.Working++
			case status.NeedsYou:
				head.Stats.NeedsYou++
			}
			children = append(children, r)
		}
		if wf.Archived && !showArchived {
			continue
		}
		total.Working += head.Stats.Working
		total.NeedsYou += head.Stats.NeedsYou
		total.Tokens += head.Stats.Tokens
		rows = append(rows, head)
		if !head.Collapsed {
			rows = append(rows, children...)
		}
	}
	var others []Row
	for i := range live {
		l := &live[i]
		if l.SessionID == "" || known[l.SessionID] {
			continue
		}
		snap := snaps[l.SessionID]
		others = append(others, Row{Kind: RowOther, Live: l, Snap: snap,
			Status: status.From(l, snap.Prompts > 0), Tokens: snap.Usage.Total()})
	}
	if len(others) > 0 {
		rows = append(rows, Row{Kind: RowOtherHeader})
		rows = append(rows, others...)
	}
	return rows, total
}
```

- [ ] **Step 8: Run tests**

Run: `go mod tidy && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 9: Commit and push**

```bash
git add go.mod go.sum internal/board
git commit -F - <<'EOF'
Add board row layout and formatting helpers

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 12: Board model and view

**Files:**
- Create: `internal/board/model.go`, `internal/board/view.go`, `internal/board/view_test.go`, `internal/board/stub_test.go`, `internal/board/testdata/*.golden` (generated)

**Interfaces:**
- Consumes: Task 11's helpers and `BuildRows`; `config.Config`, `config.Repo`; `launch.Request`.
- Produces:
  - `type Source interface` (methods below), satisfied by `claudecli.CLI`
  - `type Deps struct { Src Source; Store *store.Store; Cfg config.Config; ProjectsDir, Cwd string; Now func() time.Time }`
  - `type mode int` with `modeList, modeHelp, modePickWorkflow, modeNameWorkflow, modePickRepo, modeAskWorktree, modeAskTrust, modeConfirmDelete`
  - `type choice struct { label, value string }`, `type newFlow struct { choices []choice; cursor int; input textinput.Model; req launch.Request }`
  - `type Model struct` (fields below)
  - `func New(d Deps, warning string) Model`
  - `(Model) selected() (Row, bool)`, `(*Model) rebuild()`, `(*Model) move(int)`, `(*Model) ensureVisible()`, `(Model) bodyHeight() int`, `(Model) previewVisible() bool`
  - `(Model) View() tea.View`, `(Model) render() string`
  - test helpers: `stubSource`, `testNow`, `testConfig()`, `testModel(t, w, h) (Model, *stubSource)`

```go
type Source interface {
	List(ctx context.Context) ([]claudecli.Session, error)
	Launch(ctx context.Context, o claudecli.LaunchOpts) (string, error)
	Stop(ctx context.Context, shortID string) (string, error)
	Remove(ctx context.Context, shortID string) (string, error)
	Resume(ctx context.Context, sessionID, dir string) (string, error)
	AttachCmd(shortID string) *exec.Cmd
	TrustCmd(dir string) *exec.Cmd
}
```

- [ ] **Step 1: Add the UI dependencies**

```bash
go get charm.land/bubbletea/v2@v2.1.1 charm.land/bubbles/v2@v2.2.1 charm.land/lipgloss/v2@v2.0.6
```

- [ ] **Step 2: Write the test stub and model helper** — `internal/board/stub_test.go`

```go
package board

import (
	"context"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

var testNow = time.Date(2026, 10, 28, 22, 0, 0, 0, time.UTC)

// stubSource stands in for claudecli.CLI and records what the board asked it to do.
type stubSource struct {
	live      []claudecli.Session
	listErr   error
	launchErr error
	rmOutput  string
	launched  []claudecli.LaunchOpts
	stopped   []string
	removed   []string
	resumed   []string // "sessionID dir"
	attached  []string
	trusted   []string
}

func (s *stubSource) List(context.Context) ([]claudecli.Session, error) { return s.live, s.listErr }

func (s *stubSource) Launch(_ context.Context, o claudecli.LaunchOpts) (string, error) {
	if s.launchErr != nil {
		return "", s.launchErr
	}
	s.launched = append(s.launched, o)
	id := fmt.Sprintf("new%05d", len(s.launched))
	s.live = append(s.live, claudecli.Session{PID: 99, ShortID: id, SessionID: "sess-" + id, Name: o.Name,
		Kind: claudecli.KindBackground, Status: "idle", State: "blocked"})
	return id, nil
}

func (s *stubSource) Stop(_ context.Context, id string) (string, error) {
	s.stopped = append(s.stopped, id)
	return "stopped " + id, nil
}

func (s *stubSource) Remove(_ context.Context, id string) (string, error) {
	s.removed = append(s.removed, id)
	if s.rmOutput != "" {
		return s.rmOutput, nil
	}
	return "removed " + id, nil
}

func (s *stubSource) Resume(_ context.Context, sessionID, dir string) (string, error) {
	s.resumed = append(s.resumed, sessionID+" "+dir)
	return "res00001", nil
}

func (s *stubSource) AttachCmd(id string) *exec.Cmd {
	s.attached = append(s.attached, id)
	return exec.Command("true")
}

func (s *stubSource) TrustCmd(dir string) *exec.Cmd {
	s.trusted = append(s.trusted, dir)
	return exec.Command("true")
}

func testConfig() config.Config {
	return config.Config{
		ScratchDir: "/home/.claudeboard/scratch",
		Repos:      map[string]string{"api": "/work/api", "web": "/work/web"},
	}
}

// testModel is a board showing fixture(), with the same state saved in a temporary
// store so actions that write it work.
func testModel(t *testing.T, w, h int) (Model, *stubSource) {
	t.Helper()
	st, live, snaps := fixture()
	src := &stubSource{live: append([]claudecli.Session(nil), live...)}
	s := store.Open(t.TempDir())
	if err := s.Update(func(x *store.State) error { *x = st; return nil }); err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Src: src, Store: s, Cfg: testConfig(), ProjectsDir: t.TempDir(), Cwd: "/work",
		Now: func() time.Time { return testNow }}, "")
	m.state, m.live, m.snaps, m.lastOK = st, live, snaps, testNow
	m.width, m.height = w, h
	m.rebuild()
	return m, src
}
```

- [ ] **Step 3: Write the failing view tests** — `internal/board/view_test.go`

```go
package board

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/<name>.golden (rewriting it with -update).
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/board -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden file:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertBox checks the screen is exactly h lines of exactly w cells.
func assertBox(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("%d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, got, w, ansi.Strip(l))
		}
	}
}

func TestRenderWide(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2 // cb-payments-v2-2, waiting on a permission prompt
	out := m.render()
	assertBox(t, out, 100, 16)
	plain := ansi.Strip(out)
	for _, want := range []string{
		"claudeboard · 1 working · 1 needs you · 65k tokens",
		"cb-payments-v2-2 · web · 12k tokens",
		"needs you — permission prompt: Bash npm test",
		"you: hook PaymentForm into checkout",
		"Other sessions",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("screen is missing %q:\n%s", want, plain)
		}
	}
	golden(t, "wide", plain)
}

func TestRenderNarrowHidesPreview(t *testing.T) {
	m, _ := testModel(t, 70, 16)
	out := m.render()
	assertBox(t, out, 70, 16)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "cache r") {
		t.Errorf("preview shown below %d columns:\n%s", narrowWidth, plain)
	}
	golden(t, "narrow", plain)
	m.previewToggled = true
	assertBox(t, m.render(), 70, 16)
	if !strings.Contains(ansi.Strip(m.render()), "cache r") {
		t.Error("p should show the preview on a narrow screen")
	}
}

func TestRenderHelp(t *testing.T) {
	m, _ := testModel(t, 100, 20)
	m.mode = modeHelp
	out := m.render()
	assertBox(t, out, 100, 20)
	golden(t, "help", ansi.Strip(out))
}

func TestHeaderShowsStaleStatus(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.deps.Now = func() time.Time { return testNow.Add(12 * time.Second) }
	if !strings.Contains(ansi.Strip(m.render()), "status stale (12s)") {
		t.Error("stale status not shown")
	}
}

func TestEmptyBoard(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.state, m.live = store.State{}, nil
	m.rebuild()
	out := m.render()
	assertBox(t, out, 100, 16)
	if !strings.Contains(ansi.Strip(out), "No sessions yet") {
		t.Errorf("empty board:\n%s", ansi.Strip(out))
	}
}

func TestListScrollsToCursor(t *testing.T) {
	m, _ := testModel(t, 100, 10)
	st := store.State{Version: 1, Workflows: []store.Workflow{{Slug: "big", Name: "big"}}}
	snaps := map[string]transcript.Snapshot{}
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("big-%d", i)
		st.Sessions = append(st.Sessions, store.Session{SessionID: id, ShortID: id, Name: fmt.Sprintf("cb-big-%d", i), Workflow: "big", Repo: "api"})
		snaps[id] = transcript.Snapshot{Found: true, FirstPrompt: fmt.Sprintf("task %d", i), Prompts: 1}
	}
	m.state, m.live, m.snaps = st, nil, snaps
	m.rebuild()
	for i := 0; i < 40; i++ {
		m.move(1)
	}
	out := m.render()
	assertBox(t, out, 100, 10)
	if !strings.Contains(ansi.Strip(out), "task 30") || m.listTop == 0 {
		t.Errorf("cursor row not visible (listTop %d):\n%s", m.listTop, ansi.Strip(out))
	}
}

func TestWideCharactersKeepBoxAligned(t *testing.T) {
	for _, w := range []int{100, 70} {
		m, _ := testModel(t, w, 16)
		snap := m.snaps["id-2"]
		snap.FirstPrompt = "修复结账页面的错误 😀 then \x1b[31mred\x1b[0m\ttabs"
		snap.Items = append(snap.Items, transcript.Item{Kind: transcript.ItemText, Text: "完成了 ✅ 所有测试都通过了，结账页面现在可以正常工作。\ttab"})
		m.snaps["id-2"] = snap
		m.rebuild()
		m.cursor = 2
		assertBox(t, m.render(), w, 16)
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `go test ./internal/board`
Expected: FAIL, `undefined: New`

- [ ] **Step 5: Implement the model** — `internal/board/model.go`

```go
// Package board is claudeboard's terminal UI: workflows and their sessions on the
// left, a live preview of the selected one on the right.
package board

import (
	"context"
	"os/exec"
	"time"

	"charm.land/bubbles/v2/textinput"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

const (
	refreshEvery = 1500 * time.Millisecond
	staleAfter   = 5 * time.Second
	narrowWidth  = 90 // below this the preview is hidden unless toggled with p
)

// Source is everything the board needs from Claude Code. claudecli.CLI implements it;
// tests use a stub.
type Source interface {
	List(ctx context.Context) ([]claudecli.Session, error)
	Launch(ctx context.Context, o claudecli.LaunchOpts) (string, error)
	Stop(ctx context.Context, shortID string) (string, error)
	Remove(ctx context.Context, shortID string) (string, error)
	Resume(ctx context.Context, sessionID, dir string) (string, error)
	AttachCmd(shortID string) *exec.Cmd
	TrustCmd(dir string) *exec.Cmd
}

var _ Source = claudecli.CLI{}

// Deps are the board's connections to the outside world.
type Deps struct {
	Src         Source
	Store       *store.Store
	Cfg         config.Config
	ProjectsDir string // where Claude Code keeps transcripts
	Cwd         string // the folder claudeboard was started in
	Now         func() time.Time
}

type mode int

const (
	modeList mode = iota
	modeHelp
	modePickWorkflow
	modeNameWorkflow
	modePickRepo
	modeAskWorktree
	modeAskTrust
	modeConfirmDelete
)

type choice struct {
	label string
	value string
}

// newFlow holds the answers so far while starting a session.
type newFlow struct {
	choices []choice
	cursor  int
	input   textinput.Model
	req     launch.Request
}

// Model is the board's whole state.
type Model struct {
	deps Deps

	width, height int

	state   store.State
	live    []claudecli.Session
	snaps   map[string]transcript.Snapshot
	readers map[string]*transcript.Reader // nil while a refresh owns them

	rows      []Row
	counts    Counts
	cursor    int
	listTop   int // first row shown in the left pane
	collapsed map[string]bool

	showArchived   bool
	previewToggled bool // p flips the default: shown when wide, hidden when narrow
	previewScroll  int  // lines scrolled back from the newest preview line

	refreshing bool
	lastOK     time.Time // last successful `claude agents --json`
	listErr    error
	message    string // one-line note in the footer

	mode    mode
	flow    newFlow
	pending Row // the row a y/N question is about
}

// New returns a board. warning, if not empty, is shown in the footer until the first
// key press.
func New(d Deps, warning string) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Model{
		deps:      d,
		snaps:     map[string]transcript.Snapshot{},
		readers:   map[string]*transcript.Reader{},
		collapsed: map[string]bool{},
		message:   warning,
	}
}

func (m Model) selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// rebuild recomputes the rows, keeping the cursor on the same row when it still
// exists and otherwise at the same position.
func (m *Model) rebuild() {
	key := ""
	if r, ok := m.selected(); ok {
		key = r.Key()
	}
	m.rows, m.counts = BuildRows(m.state, m.live, m.snaps, m.collapsed, m.showArchived)
	found := false
	for i, r := range m.rows {
		if r.Key() == key {
			m.cursor, found = i, true
			break
		}
	}
	if !found {
		m.cursor = clamp(m.cursor, 0, len(m.rows)-1)
	}
	m.skipHeader(1)
	m.ensureVisible()
}

// skipHeader moves the cursor off the "Other sessions" heading, in direction dir if
// possible.
func (m *Model) skipHeader(dir int) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].Kind != RowOtherHeader {
		return
	}
	if next := m.cursor + dir; next >= 0 && next < len(m.rows) {
		m.cursor = next
	} else {
		m.cursor -= dir
	}
}

func (m *Model) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = clamp(m.cursor+d, 0, len(m.rows)-1)
	m.skipHeader(d)
	m.previewScroll = 0
	m.ensureVisible()
}

// ensureVisible scrolls the left pane so the cursor's row is on screen.
func (m *Model) ensureVisible() {
	h := m.bodyHeight()
	if m.cursor < m.listTop {
		m.listTop = m.cursor
	}
	if m.cursor >= m.listTop+h {
		m.listTop = m.cursor - h + 1
	}
	m.listTop = clamp(m.listTop, 0, max(0, len(m.rows)-h))
}

// bodyHeight is the number of lines between the header and the footer.
func (m Model) bodyHeight() int { return max(1, m.height-4) }

func (m Model) previewVisible() bool { return (m.width >= narrowWidth) != m.previewToggled }
```

- [ ] **Step 6: Implement the view** — `internal/board/view.go`

```go
package board

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

var (
	styleWorking = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleNeeds   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	styleDone    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleFaint   = lipgloss.NewStyle().Faint(true)
	styleBold    = lipgloss.NewStyle().Bold(true)
)

const (
	defaultHelp  = "↑↓ move  enter open  tab next waiting  n new  s quick  ? help  q quit"
	maxItemLines = 8 // a long prompt or reply takes at most this many preview lines
)

// View implements tea.Model.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

// render draws the screen: exactly m.height lines of exactly m.width cells.
func (m Model) render() string {
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		return "loading…"
	}
	if w < 30 || h < 6 {
		return "claudeboard: terminal too small"
	}
	bh := m.bodyHeight()
	wide := m.previewVisible()
	lw, rw := w-2, 0
	if wide {
		lw = clamp(w*2/5, 28, 50)
		rw = w - lw - 3
	}
	var left, right []string
	if !wide && m.panelMode() {
		left = m.panelLines(lw, bh)
	} else {
		left = m.leftLines(lw, bh)
	}
	if wide {
		right = m.panelLines(rw, bh)
	}
	lines := make([]string, 0, h)
	lines = append(lines, topBorder(m.header(), w))
	for i := 0; i < bh; i++ {
		line := "│" + pad(at(left, i), lw) + "│"
		if wide {
			line += pad(at(right, i), rw) + "│"
		}
		lines = append(lines, line)
	}
	if wide {
		lines = append(lines, "├"+strings.Repeat("─", lw)+"┴"+strings.Repeat("─", rw)+"┤")
	} else {
		lines = append(lines, "├"+strings.Repeat("─", lw)+"┤")
	}
	lines = append(lines, "│"+pad(" "+m.footer(), w-2)+"│", "└"+strings.Repeat("─", w-2)+"┘")
	return strings.Join(lines, "\n")
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func topBorder(title string, w int) string {
	title = truncate(title, w-4)
	return "┌ " + title + " " + strings.Repeat("─", max(0, w-4-ansi.StringWidth(title))) + "┐"
}

func (m Model) header() string {
	parts := []string{"claudeboard"}
	if m.counts.Working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", m.counts.Working))
	}
	if m.counts.NeedsYou > 0 {
		parts = append(parts, styleNeeds.Render(fmt.Sprintf("%d needs you", m.counts.NeedsYou)))
	}
	parts = append(parts, formatTokens(m.counts.Tokens)+" tokens")
	switch {
	case m.lastOK.IsZero() && m.listErr != nil:
		parts = append(parts, styleNeeds.Render("status unavailable"))
	case !m.lastOK.IsZero():
		if age := m.deps.Now().Sub(m.lastOK); age > staleAfter {
			parts = append(parts, styleNeeds.Render(fmt.Sprintf("status stale (%ds)", int(age.Seconds()))))
		}
	}
	return strings.Join(parts, " · ")
}

func (m Model) footer() string {
	switch m.mode {
	case modeConfirmDelete:
		return styleNeeds.Render("Delete " + m.pending.Name() + " (and its worktree, if any)? y/N")
	case modeAskWorktree:
		return styleNeeds.Render(repoLabel(m.flow.req.Repo) + " already has a session running. Use a git worktree? y/N")
	case modeAskTrust:
		return styleNeeds.Render(m.flow.req.Dir + " isn't trusted yet. Open Claude there to accept, then /exit? y/N")
	}
	if m.message != "" {
		return m.message
	}
	return styleFaint.Render(defaultHelp)
}

// panelMode reports whether the right-hand panel shows something other than the
// preview (and so takes over the whole body on a narrow screen).
func (m Model) panelMode() bool {
	switch m.mode {
	case modeHelp, modePickWorkflow, modeNameWorkflow, modePickRepo:
		return true
	}
	return false
}

func (m Model) leftLines(width, height int) []string {
	if len(m.rows) == 0 {
		return []string{"", " No sessions yet.", "", " n  start a session", " s  quick one-off in the scratch folder"}
	}
	lines := make([]string, 0, height)
	for i := m.listTop; i < len(m.rows) && len(lines) < height; i++ {
		lines = append(lines, m.rowLine(m.rows[i], i == m.cursor, width))
	}
	return lines
}

func (m Model) rowLine(r Row, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = styleBold.Render("▸") + " "
	}
	switch r.Kind {
	case RowWorkflow:
		name := styleBold.Render(r.Workflow.Name)
		if r.Stats.NeedsYou > 0 {
			name += "  " + styleNeeds.Render("!")
		}
		if r.Collapsed {
			name += "  [+]"
		}
		if r.Workflow.Archived {
			name += "  " + styleFaint.Render("archived")
		}
		return fit(marker+name, tokensOrBlank(r.Tokens), width)
	case RowOtherHeader:
		return styleFaint.Render("  Other sessions")
	case RowSession:
		return fit("  "+marker+statusIcon(r.Status)+" "+repoLabel(r.Session.Repo)+"  "+sessionLabel(r.Snap), tokensOrBlank(r.Tokens), width)
	}
	return fit("  "+marker+statusIcon(r.Status)+" "+r.Name()+"  "+sessionLabel(r.Snap), tokensOrBlank(r.Tokens), width)
}

func statusIcon(s status.Status) string {
	switch s.Kind {
	case status.Working:
		return styleWorking.Render(s.Icon())
	case status.NeedsYou:
		return styleNeeds.Render(s.Icon())
	case status.Done:
		return styleDone.Render(s.Icon())
	}
	return styleFaint.Render(s.Icon())
}

// panelLines is the right-hand panel: a picker, help, or the selected row's preview.
func (m Model) panelLines(width, height int) []string {
	switch m.mode {
	case modeHelp:
		return helpLines()
	case modePickWorkflow:
		return m.pickerLines("New session: pick a workflow")
	case modePickRepo:
		return m.pickerLines("New " + m.flow.req.Workflow + " session: pick a folder")
	case modeNameWorkflow:
		return []string{" " + styleBold.Render("New workflow name"), "", " " + m.flow.input.View(), "",
			styleFaint.Render(" enter continue · esc cancel")}
	}
	r, ok := m.selected()
	if !ok {
		return []string{"", " Press n to start a session, or s for a quick one-off."}
	}
	return m.previewLines(r, width, height)
}

func (m Model) pickerLines(title string) []string {
	lines := []string{" " + styleBold.Render(title), ""}
	for i, c := range m.flow.choices {
		marker := "  "
		if i == m.flow.cursor {
			marker = styleBold.Render("▸") + " "
		}
		lines = append(lines, " "+marker+c.label)
	}
	return append(lines, "", styleFaint.Render(" ↑↓ choose · enter select · esc cancel"))
}

func helpLines() []string {
	keys := [][2]string{
		{"↑↓ / j k", "move"},
		{"enter", "open the session (ctrl+z inside it comes back here) · fold a workflow"},
		{"tab", "jump to the next session that needs you"},
		{"n", "new session"},
		{"s", "quick one-off in the scratch folder"},
		{"x", "stop the session (keeps its conversation and worktree)"},
		{"r", "resume a stopped session in the background"},
		{"D", "delete the session and its worktree"},
		{"a / A", "archive the workflow / show archived workflows"},
		{"p", "show or hide the preview"},
		{"pgup pgdn", "scroll the preview"},
		{"q", "quit (sessions keep running)"},
	}
	lines := []string{" " + styleBold.Render("Keys"), ""}
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf(" %-10s %s", k[0], k[1]))
	}
	return append(lines, "", styleFaint.Render(" press any key to close"))
}

func (m Model) previewLines(r Row, width, height int) []string {
	switch r.Kind {
	case RowWorkflow:
		return m.workflowLines(r)
	case RowOtherHeader:
		return nil
	}
	title := " " + styleBold.Render(r.Name())
	switch {
	case r.Kind == RowSession:
		where := repoLabel(r.Session.Repo)
		if r.Session.Worktree != "" {
			where += " (worktree " + r.Session.Worktree + ")"
		}
		title += " · " + where
	case r.Interactive():
		title += " · running in another terminal"
	}
	title += " · " + formatTokens(r.Tokens) + " tokens"
	u := r.Snap.Usage
	usage := fmt.Sprintf(" in %s · out %s · cache r %s · w %s",
		formatTokens(u.Input), formatTokens(u.Output), formatTokens(u.CacheRead), formatTokens(u.CacheWrite))
	if r.Snap.Subagents > 0 {
		usage += " · " + plural(r.Snap.Subagents, "subagent")
	}
	state := " " + statusIcon(r.Status) + " " + r.Status.Text()
	if p := r.Snap.Pending; p != "" && (r.Status.Kind == status.NeedsYou || r.Status.Kind == status.Working) {
		state += ": " + p
	}
	head := []string{title, styleFaint.Render(usage), state, ""}

	var body []string
	if !r.Snap.Found {
		body = []string{styleFaint.Render(" no messages yet")}
	}
	for _, it := range r.Snap.Items {
		body = append(body, itemLines(it, width-2)...)
	}
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
		" " + styleBold.Render(r.Workflow.Name) + " · " + plural(r.Sessions, "session") + " · " + formatTokens(r.Tokens) + " tokens",
		fmt.Sprintf(" %d working · %d need you", r.Stats.Working, r.Stats.NeedsYou),
	}
	if r.Workflow.LastRepo != "" {
		lines = append(lines, " last folder: "+repoLabel(r.Workflow.LastRepo))
	}
	return append(lines, "", styleFaint.Render(" enter fold/unfold · n new session here · a archive"))
}

// itemLines wraps one preview item to width, capped at maxItemLines lines.
func itemLines(it transcript.Item, width int) []string {
	prefix := "• "
	switch it.Kind {
	case transcript.ItemPrompt:
		prefix = "you: "
	case transcript.ItemText:
		prefix = "claude: "
	}
	text := clean(it.Text)
	if it.Kind == transcript.ItemTool {
		text = firstLine(text)
	}
	wrapped := strings.Split(ansi.Wrap(prefix+text, max(10, width), ""), "\n")
	if len(wrapped) > maxItemLines {
		wrapped = append(wrapped[:maxItemLines-1], "…")
	}
	for i, l := range wrapped {
		wrapped[i] = " " + l
	}
	return wrapped
}
```

- [ ] **Step 7: Generate the golden files and review them**

Run: `go test ./internal/board -run 'TestRender' -update`
Then open `internal/board/testdata/wide.golden`, `narrow.golden` and `help.golden` and check them against the spec's mockup (section 7):
- **Header:** reads `claudeboard · 1 working · 1 needs you · 65k tokens`.
- **Left pane:**
  - workflows sit at column 2 with token totals right-aligned (53k, 12k; quick shows none)
  - sessions are indented, with icon, repo and label
  - `▸` marks `web`
  - the "Other sessions" heading and `arbybot-8d` come last
- **Preview:** shows the title, the token breakdown, `! needs you — permission prompt: Bash npm test`, then the four items.
- **narrow.golden:** has no right pane.
- **Box:** the borders close on every line.

Fix the rendering code if anything is off, then regenerate.

- [ ] **Step 8: Run tests**

Run: `go mod tidy && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 9: Commit and push**

```bash
git add go.mod go.sum internal/board
git commit -F - <<'EOF'
Add board model and screen rendering

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 13: Update loop, refresh and navigation

**Files:**
- Create: `internal/board/update.go`, `internal/board/keys.go`, `internal/board/update_test.go`

**Interfaces:**
- Consumes: everything from Task 12; `store.Listed`, `NeedsRelink`, `Relink`; `transcript.NewReader`.
- Produces:
  - `(Model) Init() tea.Cmd`, `(Model) Update(tea.Msg) (tea.Model, tea.Cmd)`; Model now implements `tea.Model`
  - `type tickMsg time.Time`
  - `type refreshedMsg struct { state store.State; live []claudecli.Session; listErr, storeErr error; snaps map[string]transcript.Snapshot; readers map[string]*transcript.Reader; at time.Time }`
  - `type applier interface { apply(Model) (Model, tea.Cmd) }`. Messages from Tasks 14–15 implement it, and Update dispatches them without changing.
  - `(*Model) startRefresh() tea.Cmd`, `doRefresh(Deps, map[string]*transcript.Reader) refreshedMsg`, `(*Model) applyRefresh(refreshedMsg)`, `(*Model) nextWaiting()`
  - `(Model) handleKey`, `(Model) listKey`
  - in keys.go: stubs `(Model) commandKey(tea.KeyPressMsg) (tea.Model, tea.Cmd)` and `(Model) flowKey(tea.KeyPressMsg) (tea.Model, tea.Cmd)`, replaced in Tasks 14 and 15
  - test helpers `key(string) tea.KeyPressMsg`, `press(t, Model, ...string) (Model, tea.Cmd)`, `send(Model, tea.Msg) (Model, tea.Cmd)`

- [ ] **Step 1: Write the failing tests** — `internal/board/update_test.go`

```go
package board

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		m, cmd = send(m, key(k))
	}
	return m, cmd
}

func TestDoRefreshAdoptsSessionsAndReadsTranscripts(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-work-api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(`{"type":"user","message":{"role":"user","content":"fix the bug"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := &stubSource{live: []claudecli.Session{
		{PID: 1, ShortID: "aaaa0001", SessionID: "sess-1", Name: "cb-bugs-1", Kind: claudecli.KindBackground, Cwd: "/work/api", State: "working"},
		{PID: 2, SessionID: "sess-other", Name: "cb-looks-like-ours-1", Kind: claudecli.KindInteractive, Status: "idle"},
	}}
	d := Deps{Src: src, Store: store.Open(t.TempDir()), ProjectsDir: projects, Now: func() time.Time { return testNow }}
	msg := doRefresh(d, nil)
	if msg.listErr != nil || msg.storeErr != nil {
		t.Fatalf("errors: %v, %v", msg.listErr, msg.storeErr)
	}
	if s := msg.state.Session("sess-1"); s == nil || s.Workflow != "bugs" || s.Dir != "/work/api" || msg.state.Workflow("bugs") == nil {
		t.Errorf("cb-bugs-1 not adopted: %+v", msg.state)
	}
	if msg.state.Session("sess-other") != nil {
		t.Error("interactive sessions must not be adopted")
	}
	if s := msg.snaps["sess-1"]; !s.Found || s.FirstPrompt != "fix the bug" {
		t.Errorf("snapshot = %+v", s)
	}
	if msg.readers["sess-1"] == nil {
		t.Error("reader not kept for the next refresh")
	}
}

func TestRefreshKeepsLastStatusWhenListFails(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.listErr = errors.New("claude agents --json: boom")
	m, _ = send(m, doRefresh(m.deps, nil))
	if m.listErr == nil || len(m.live) != 5 || !m.lastOK.Equal(testNow) {
		t.Errorf("listErr %v, %d live, lastOK %v", m.listErr, len(m.live), m.lastOK)
	}
}

func TestTickStartsOneRefreshAtATime(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := send(m, tickMsg(testNow))
	if !m.refreshing || m.readers != nil || cmd == nil {
		t.Fatalf("after first tick: refreshing %v, readers %v", m.refreshing, m.readers)
	}
	if m.startRefresh() != nil {
		t.Error("a second refresh started while one was running")
	}
}

func TestRebuildKeepsSelection(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 4 // cb-q4-search-1
	m.collapsed["payments-v2"] = true
	m.rebuild()
	if r, _ := m.selected(); r.Session.Name != "cb-q4-search-1" {
		t.Errorf("selection moved to %+v", r)
	}
}

func TestSelectionClampsWhenSessionDisappears(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 8 // arbybot-8d, the only Other session
	m.live = m.live[:4]
	m.rebuild()
	if m.cursor != len(m.rows)-1 || m.rows[m.cursor].Kind == RowOtherHeader {
		t.Errorf("cursor %d of %d rows", m.cursor, len(m.rows))
	}
}

func TestMoveSkipsOtherHeader(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 6
	m, _ = press(t, m, "down")
	if m.cursor != 8 {
		t.Errorf("down from 6 went to %d, want 8", m.cursor)
	}
	m, _ = press(t, m, "k")
	if m.cursor != 6 {
		t.Errorf("up from 8 went to %d, want 6", m.cursor)
	}
}

func TestTabJumpsToWaitingSession(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "tab")
	if m.cursor != 2 {
		t.Errorf("tab went to %d, want 2", m.cursor)
	}
	m.live[1].State, m.live[1].WaitingFor = "working", ""
	m.rebuild()
	m, _ = press(t, m, "tab")
	if m.cursor != 2 || m.message != "nothing needs you right now" {
		t.Errorf("cursor %d, message %q", m.cursor, m.message)
	}
}

func TestQuitHelpAndPreviewKeys(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	if _, cmd := press(t, m, "q"); cmd == nil {
		t.Fatal("q returned no command")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q should quit")
	}
	m, _ = press(t, m, "?")
	if m.mode != modeHelp {
		t.Fatal("? should open help")
	}
	m, _ = press(t, m, "x")
	if m.mode != modeList {
		t.Error("any key should close help")
	}
	m, _ = press(t, m, "p")
	if m.previewVisible() {
		t.Error("p should hide the preview on a wide screen")
	}
	m, _ = press(t, m, "pgup")
	if m.previewScroll == 0 {
		t.Error("pgup should scroll the preview back")
	}
	m, _ = press(t, m, "A")
	if !m.showArchived {
		t.Error("A should show archived workflows")
	}
}

func TestWindowSize(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 30})
	if m.width != 80 || m.height != 30 {
		t.Errorf("size = %dx%d", m.width, m.height)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/board`
Expected: FAIL, `m.Update undefined` / `undefined: doRefresh`

- [ ] **Step 3: Implement** — `internal/board/update.go`

```go
package board

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

type tickMsg time.Time

// refreshedMsg carries one refresh's results back to Update.
type refreshedMsg struct {
	state    store.State
	live     []claudecli.Session
	listErr  error
	storeErr error
	snaps    map[string]transcript.Snapshot
	readers  map[string]*transcript.Reader
	at       time.Time
}

// applier is a message that knows how to apply itself, so action messages added
// later don't need new cases in Update.
type applier interface {
	apply(m Model) (Model, tea.Cmd)
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the refresh cycle straight away.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return tickMsg(time.Now()) }
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil
	case tickMsg:
		cmd := m.startRefresh()
		return m, tea.Batch(cmd, tick())
	case refreshedMsg:
		m.applyRefresh(msg)
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case applier:
		return msg.apply(m)
	}
	if m.mode == modeNameWorkflow {
		var cmd tea.Cmd
		m.flow.input, cmd = m.flow.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// startRefresh begins a refresh unless one is running. The refresh owns the
// transcript readers until its refreshedMsg hands them back.
func (m *Model) startRefresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	d, readers := m.deps, m.readers
	m.readers = nil
	return func() tea.Msg { return doRefresh(d, readers) }
}

// doRefresh asks Claude for its sessions, re-reads the state (adopting cb- sessions
// it doesn't know), and reads new transcript lines. It runs off the UI goroutine.
func doRefresh(d Deps, readers map[string]*transcript.Reader) refreshedMsg {
	if readers == nil {
		readers = map[string]*transcript.Reader{}
	}
	msg := refreshedMsg{readers: readers, at: d.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), staleAfter)
	defer cancel()
	msg.live, msg.listErr = d.Src.List(ctx)

	st, _, err := d.Store.Load()
	if err != nil {
		msg.storeErr = err
		return msg
	}
	if msg.listErr == nil {
		listed := toListed(msg.live)
		if st.NeedsRelink(listed) {
			now := d.Now()
			if err := d.Store.Update(func(s *store.State) error { s.Relink(listed, now); return nil }); err == nil {
				if fresh, _, err := d.Store.Load(); err == nil {
					st = fresh
				}
			}
		}
	}
	msg.state = st

	ids := map[string]bool{}
	for _, s := range st.Sessions {
		if s.SessionID != "" {
			ids[s.SessionID] = true
		}
	}
	for _, l := range msg.live {
		if l.SessionID != "" {
			ids[l.SessionID] = true
		}
	}
	msg.snaps = make(map[string]transcript.Snapshot, len(ids))
	for id := range ids {
		r := readers[id]
		if r == nil {
			r = transcript.NewReader(d.ProjectsDir, id)
			readers[id] = r
		}
		snap, _ := r.Update()
		msg.snaps[id] = snap
	}
	for id := range readers {
		if !ids[id] {
			delete(readers, id)
		}
	}
	return msg
}

// toListed keeps the background sessions; interactive ones are never adopted.
func toListed(live []claudecli.Session) []store.Listed {
	var out []store.Listed
	for _, l := range live {
		if l.Kind == claudecli.KindBackground {
			out = append(out, store.Listed{SessionID: l.SessionID, ShortID: l.ShortID, Name: l.Name, Cwd: l.Cwd})
		}
	}
	return out
}

func (m *Model) applyRefresh(msg refreshedMsg) {
	m.refreshing = false
	m.readers = msg.readers
	if msg.storeErr != nil {
		m.message = "reading state.json: " + msg.storeErr.Error()
	} else {
		m.state = msg.state
	}
	if msg.snaps != nil {
		m.snaps = msg.snaps
	}
	if msg.listErr != nil {
		m.listErr = msg.listErr
	} else {
		m.live, m.listErr, m.lastOK = msg.live, nil, msg.at
	}
	m.rebuild()
}

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeList:
		return m.listKey(k)
	case modeHelp:
		m.mode = modeList
		return m, nil
	}
	return m.flowKey(k)
}

func (m Model) listKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "tab":
		m.nextWaiting()
	case "A":
		m.showArchived = !m.showArchived
		m.rebuild()
	case "p":
		m.previewToggled = !m.previewToggled
	case "pgup":
		m.previewScroll += page
	case "pgdown":
		m.previewScroll = max(0, m.previewScroll-page)
	case "?":
		m.mode = modeHelp
	default:
		return m.commandKey(k)
	}
	return m, nil
}

// nextWaiting moves to the next session (after the cursor, wrapping) that needs you.
func (m *Model) nextWaiting() {
	n := len(m.rows)
	for i := 1; i <= n; i++ {
		j := (m.cursor + i) % n
		if r := m.rows[j]; (r.Kind == RowSession || r.Kind == RowOther) && r.Status.Kind == status.NeedsYou {
			m.cursor, m.previewScroll = j, 0
			m.ensureVisible()
			return
		}
	}
	m.message = "nothing needs you right now"
}
```

- [ ] **Step 4: Add the key stubs** — `internal/board/keys.go`

```go
package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on sessions. Tasks 14 and 15 fill it in.
func (m Model) commandKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m, nil }

// flowKey handles keys while a question is open. Tasks 14 and 15 fill it in.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeList
	return m, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go vet ./... && go test -race ./internal/board && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit and push**

```bash
git add internal/board
git commit -F - <<'EOF'
Add board refresh cycle and navigation keys

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 14: Session actions (open, stop, resume, delete, archive)

**Files:**
- Create: `internal/board/actions.go`, `internal/board/actions_test.go`
- Modify: `internal/board/keys.go`. Replace the whole file.

**Interfaces:**
- Consumes: `Source` methods; `store.Update`, `RemoveSession`, `Workflow`; Task 13's `applier` and `startRefresh`.
- Produces:
  - messages implementing `applier`: `actionDoneMsg{text string; err error}`, `resumedMsg{shortID string}`, `attachDoneMsg{err error}`
  - `(Model) attach(shortID string) tea.Cmd`. Task 15 uses it.
  - `(Model) enter/stop/resume/askDelete/toggleArchive() (tea.Model, tea.Cmd)`
  - `(Model) confirmDelete(tea.KeyPressMsg) (tea.Model, tea.Cmd)`
  - `(Model) resumeCmd(Row, bool) tea.Cmd`, `(Model) deleteCmd(Row) tea.Cmd`, `rmDetails(string) string`
  - `const msgInteractive`

- [ ] **Step 1: Write the failing tests** — `internal/board/actions_test.go`

```go
package board

import (
	"strings"
	"testing"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
)

func TestEnterFoldsWorkflow(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "enter") // cursor starts on payments-v2
	if len(m.rows) != 7 || !m.rows[0].Collapsed {
		t.Fatalf("after enter: %d rows", len(m.rows))
	}
	m, _ = press(t, m, "enter")
	if len(m.rows) != 9 {
		t.Errorf("after second enter: %d rows", len(m.rows))
	}
}

func TestEnterOpensSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 2
	_, cmd := press(t, m, "enter")
	if cmd == nil || len(src.attached) != 1 || src.attached[0] != "aaaa0002" {
		t.Errorf("cmd %v, attached %v", cmd, src.attached)
	}
}

func TestEnterOnInteractiveSessionExplains(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 8
	m, cmd := press(t, m, "enter")
	if cmd != nil || m.message != msgInteractive || len(src.attached) != 0 {
		t.Errorf("cmd %v, message %q", cmd, m.message)
	}
}

func TestEnterResumesUnlistedSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live = append(m.live[:2:2], m.live[3:]...) // Claude no longer lists cb-q4-search-1
	m.rebuild()
	m.cursor = 4
	m, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	msg := cmd()
	if len(src.resumed) != 1 || src.resumed[0] != "id-3 /work/api" {
		t.Fatalf("resumed %v", src.resumed)
	}
	if _, cmd = send(m, msg); cmd == nil || len(src.attached) != 1 || src.attached[0] != "res00001" {
		t.Errorf("after resuming: attached %v", src.attached)
	}
}

func TestStop(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 1
	m, cmd := press(t, m, "x")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	if len(src.stopped) != 1 || src.stopped[0] != "aaaa0001" || m.message != "stopped cb-payments-v2-1" {
		t.Errorf("stopped %v, message %q", src.stopped, m.message)
	}
	m.live[2].State = "stopped"
	m.rebuild()
	m.cursor = 4
	if m, cmd = press(t, m, "x"); cmd != nil || m.message != "cb-q4-search-1 is already stopped" {
		t.Errorf("stopping a stopped session: cmd %v, message %q", cmd, m.message)
	}
}

func TestResumeOnlyWhenStopped(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.cursor = 1
	if m, _ = press(t, m, "r"); !strings.Contains(m.message, "already running") {
		t.Errorf("message %q", m.message)
	}
	m.live[2].State = "stopped"
	m.rebuild()
	m.cursor = 4
	m, cmd := press(t, m, "r")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	if len(src.resumed) != 1 || m.message != "resumed cb-q4-search-1" || len(src.attached) != 0 {
		t.Errorf("resumed %v, message %q, attached %v", src.resumed, m.message, src.attached)
	}
}

func TestDeleteAsksFirst(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.rmOutput = "removed aaaa0003\n  worktree: “/work/api/.claude/worktrees/q4-search-1”"
	m.cursor = 4
	m, _ = press(t, m, "D")
	if m.mode != modeConfirmDelete || !strings.Contains(m.footer(), "Delete cb-q4-search-1") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer())
	}
	m, cmd := press(t, m, "n")
	if cmd != nil || m.message != "cancelled" || len(src.removed) != 0 {
		t.Fatalf("n should cancel: %q", m.message)
	}
	m, cmd = press(t, m, "D", "y")
	if cmd == nil {
		t.Fatal("y returned no command")
	}
	m, _ = send(m, cmd())
	if len(src.removed) != 1 || !strings.HasPrefix(m.message, "deleted cb-q4-search-1 · worktree:") {
		t.Errorf("removed %v, message %q", src.removed, m.message)
	}
	if st, _, _ := m.deps.Store.Load(); st.Session("id-3") != nil {
		t.Error("session still in state.json")
	}
}

func TestDeleteOtherBackgroundSession(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.live = append(m.live, claudecli.Session{PID: 7, ShortID: "ffff0007", SessionID: "id-7", Name: "manual", Kind: claudecli.KindBackground, State: "done"})
	m.rebuild()
	m.cursor = len(m.rows) - 1
	m, cmd := press(t, m, "D", "y")
	if cmd == nil {
		t.Fatal("no command")
	}
	send(m, cmd())
	if len(src.removed) != 1 || src.removed[0] != "ffff0007" {
		t.Errorf("removed %v", src.removed)
	}
}

func TestArchiveToggles(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, cmd := press(t, m, "a")
	if cmd == nil {
		t.Fatal("no command")
	}
	m, _ = send(m, cmd())
	st, _, _ := m.deps.Store.Load()
	if !st.Workflow("payments-v2").Archived || !strings.HasPrefix(m.message, "archived payments-v2") {
		t.Errorf("archived %v, message %q", st.Workflow("payments-v2").Archived, m.message)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/board`
Expected: FAIL, `undefined: msgInteractive`

- [ ] **Step 3: Implement** — `internal/board/actions.go`

```go
package board

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

const msgInteractive = "that session runs in another terminal; switch to it there"

// actionDoneMsg reports the result of a stop, delete, resume or archive.
type actionDoneMsg struct {
	text string
	err  error
}

func (msg actionDoneMsg) apply(m Model) (Model, tea.Cmd) {
	m.message = msg.text
	if msg.err != nil {
		m.message = msg.err.Error()
	}
	cmd := m.startRefresh()
	return m, cmd
}

// resumedMsg says a session was resumed so it can be opened.
type resumedMsg struct{ shortID string }

func (msg resumedMsg) apply(m Model) (Model, tea.Cmd) { return m, m.attach(msg.shortID) }

// attachDoneMsg arrives when the user comes back from a session.
type attachDoneMsg struct{ err error }

func (msg attachDoneMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		m.message = "claude attach: " + msg.err.Error()
	}
	cmd := m.startRefresh()
	return m, cmd
}

// attach hands the terminal to `claude attach` until the user presses ctrl+z.
func (m Model) attach(shortID string) tea.Cmd {
	if shortID == "" {
		return nil
	}
	return tea.ExecProcess(m.deps.Src.AttachCmd(shortID), func(err error) tea.Msg { return attachDoneMsg{err: err} })
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		return m, nil
	}
	switch {
	case r.Kind == RowWorkflow:
		m.collapsed[r.Workflow.Slug] = !m.collapsed[r.Workflow.Slug]
		m.rebuild()
		return m, nil
	case r.Interactive():
		m.message = msgInteractive
		return m, nil
	case r.Kind == RowSession && r.Live == nil:
		// Claude no longer lists it (deleted elsewhere, or its background service
		// restarted): resume from the saved conversation, then open it.
		if r.Session.SessionID == "" {
			m.message = "can't reopen " + r.Name() + ": Claude doesn't list it and its session ID is unknown"
			return m, nil
		}
		m.message = "resuming " + r.Name() + "…"
		return m, m.resumeCmd(r, true)
	}
	// Stopped background sessions are listed too; `claude attach` wakes them.
	return m, m.attach(r.ShortID())
}

func (m Model) resumeCmd(r Row, thenAttach bool) tea.Cmd {
	src, s := m.deps.Src, r.Session
	return func() tea.Msg {
		id, err := src.Resume(context.Background(), s.SessionID, s.WorkDir())
		switch {
		case err != nil:
			return actionDoneMsg{err: err}
		case thenAttach:
			return resumedMsg{shortID: id}
		}
		return actionDoneMsg{text: "resumed " + s.Name}
	}
}

// actionable reports whether x and D can act on the row, explaining in the footer
// when they can't.
func (m *Model) actionable(r Row) bool {
	switch {
	case r.Kind != RowSession && r.Kind != RowOther:
		return false
	case r.Interactive():
		m.message = msgInteractive
		return false
	}
	return true
}

func (m Model) stop() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || !m.actionable(r) {
		return m, nil
	}
	name := r.Name()
	if r.Status.Kind == status.Stopped {
		m.message = name + " is already stopped"
		return m, nil
	}
	src, id := m.deps.Src, r.ShortID()
	m.message = "stopping " + name + "…"
	return m, func() tea.Msg {
		_, err := src.Stop(context.Background(), id)
		return actionDoneMsg{text: "stopped " + name, err: err}
	}
}

func (m Model) resume() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || r.Kind != RowSession {
		return m, nil
	}
	switch {
	case r.Status.Kind != status.Stopped:
		m.message = r.Name() + " is already running"
		return m, nil
	case r.Session.SessionID == "":
		m.message = "can't resume " + r.Name() + ": its session ID is unknown"
		return m, nil
	}
	m.message = "resuming " + r.Name() + "…"
	return m, m.resumeCmd(r, false)
}

func (m Model) askDelete() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || !m.actionable(r) {
		return m, nil
	}
	m.pending, m.mode = r, modeConfirmDelete
	return m, nil
}

func (m Model) confirmDelete(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := k.String(); s != "y" && s != "Y" {
		m.message = "cancelled"
		return m, nil
	}
	m.message = "deleting " + m.pending.Name() + "…"
	return m, m.deleteCmd(m.pending)
}

// deleteCmd runs `claude rm` (unless Claude no longer lists the session) and, for
// claudeboard's own sessions, removes the record from state.json.
func (m Model) deleteCmd(r Row) tea.Cmd {
	src, st, name := m.deps.Src, m.deps.Store, r.Name()
	return func() tea.Msg {
		var out string
		if r.Live != nil {
			o, err := src.Remove(context.Background(), r.ShortID())
			if err != nil {
				return actionDoneMsg{err: err}
			}
			out = o
		}
		if r.Kind == RowSession {
			err := st.Update(func(s *store.State) error {
				s.RemoveSession(r.Session.SessionID, r.Session.Name)
				return nil
			})
			if err != nil {
				return actionDoneMsg{err: err}
			}
		}
		text := "deleted " + name
		if d := rmDetails(out); d != "" {
			text += " · " + d
		}
		return actionDoneMsg{text: text}
	}
}

// rmDetails keeps what `claude rm` printed beyond its "removed <id>" line, such as a
// note about a worktree it kept.
func rmDetails(out string) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "removed ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, " ")
}

func (m Model) toggleArchive() (tea.Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok || (r.Kind != RowWorkflow && r.Kind != RowSession) {
		return m, nil
	}
	slug, st := r.Workflow.Slug, m.deps.Store
	return m, func() tea.Msg {
		archived := false
		err := st.Update(func(s *store.State) error {
			wf := s.Workflow(slug)
			if wf == nil {
				return fmt.Errorf("workflow %s not found", slug)
			}
			wf.Archived = !wf.Archived
			archived = wf.Archived
			return nil
		})
		switch {
		case err != nil:
			return actionDoneMsg{err: err}
		case archived:
			return actionDoneMsg{text: "archived " + slug + " (A shows archived workflows)"}
		}
		return actionDoneMsg{text: "unarchived " + slug}
	}
}
```

- [ ] **Step 4: Replace `internal/board/keys.go`**

```go
package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on the selected row. Task 15 adds n and s.
func (m Model) commandKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		return m.enter()
	case "x":
		return m.stop()
	case "r":
		return m.resume()
	case "D":
		return m.askDelete()
	case "a":
		return m.toggleArchive()
	}
	return m, nil
}

// flowKey handles keys while a question is open. Task 15 adds the new-session flow.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mode := m.mode
	m.mode = modeList
	if mode == modeConfirmDelete {
		return m.confirmDelete(k)
	}
	return m, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit and push**

```bash
git add internal/board
git commit -F - <<'EOF'
Add open, stop, resume, delete and archive to the board

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 15: New-session flow (n, s, worktree and trust questions)

**Files:**
- Create: `internal/board/newflow.go`, `internal/board/newflow_test.go`
- Modify: `internal/board/keys.go`. Replace the whole file.

**Interfaces:**
- Consumes:
  - `launch.Run`, `launch.DirBusy`, `launch.Request` (Task 9)
  - `claudecli.IsUntrusted` (Task 5)
  - `config.RepoList`, `config.RepoName` (Task 2)
  - `store.Slug`, `store.QuickSlug`
  - Task 14's `attach`
- Produces:
  - `const newWorkflowChoice`
  - `(Model) startNew()`, `(Model) quick()`, `(Model) pickerKey(k)`, `(Model) nameKey(k)`, `(Model) chooseRepo(dir string)`, `(Model) answerWorktree(k)`, `(Model) answerTrust(k)`. All return `(tea.Model, tea.Cmd)`.
  - `(*Model) pickRepoFor(workflow string)`, `(*Model) launchCmd(launch.Request) tea.Cmd`
  - messages implementing `applier`: `launchedMsg{sess store.Session; req launch.Request; err error}`, `trustDoneMsg{req launch.Request}`

- [ ] **Step 1: Write the failing tests** — `internal/board/newflow_test.go`

```go
package board

import (
	"errors"
	"strings"
	"testing"
)

func labels(cs []choice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.label
	}
	return out
}

func TestNewSessionInBusyRepoOffersWorktreeThenOpens(t *testing.T) {
	m, src := testModel(t, 100, 16) // cursor on payments-v2
	m, _ = press(t, m, "n")
	if m.mode != modePickWorkflow || strings.Join(labels(m.flow.choices), ",") != "payments-v2,q4-search,quick,+ new workflow…" || m.flow.cursor != 0 {
		t.Fatalf("workflow picker: mode %v, %q, cursor %d", m.mode, labels(m.flow.choices), m.flow.cursor)
	}
	m, _ = press(t, m, "enter")
	if m.mode != modePickRepo || m.flow.cursor != 0 || m.flow.choices[0].value != "/work/api" {
		t.Fatalf("repo picker: mode %v, %q, cursor %d (want api, the workflow's last repo)", m.mode, labels(m.flow.choices), m.flow.cursor)
	}
	m, _ = press(t, m, "enter") // cb-payments-v2-1 is working in /work/api
	if m.mode != modeAskWorktree || !strings.Contains(m.footer(), "api already has a session running") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer())
	}
	m, cmd := press(t, m, "y")
	if cmd == nil || m.mode != modeList {
		t.Fatalf("y: cmd %v, mode %v", cmd, m.mode)
	}
	msg := cmd()
	if len(src.launched) != 1 {
		t.Fatalf("launched %v", src.launched)
	}
	if o := src.launched[0]; o.Name != "cb-payments-v2-3" || o.Worktree != "payments-v2-3" || o.Dir != "/work/api" {
		t.Errorf("launch options = %+v", o)
	}
	m, cmd = send(m, msg)
	if cmd == nil || len(src.attached) != 1 || src.attached[0] != "new00001" || m.message != "started cb-payments-v2-3" {
		t.Errorf("after launch: attached %v, message %q", src.attached, m.message)
	}
}

func TestNewWorkflowByName(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m, _ = press(t, m, "n", "down", "down", "down", "enter")
	if m.mode != modeNameWorkflow {
		t.Fatalf("mode %v, want the name prompt", m.mode)
	}
	m.flow.input.SetValue("!!!")
	m, _ = press(t, m, "enter")
	if m.mode != modeNameWorkflow || !strings.Contains(m.message, "letter or digit") {
		t.Fatalf("bad name accepted: mode %v, message %q", m.mode, m.message)
	}
	m.flow.input.SetValue("Bug Bash")
	m, _ = press(t, m, "enter")
	if m.mode != modePickRepo || m.flow.req.Workflow != "Bug Bash" {
		t.Fatalf("mode %v, req %+v", m.mode, m.flow.req)
	}
	if got := m.flow.choices[m.flow.cursor].value; got != "/work" {
		t.Errorf("default folder for a new workflow = %q, want the current folder", got)
	}
	m, cmd := press(t, m, "down", "enter") // scratch: cb-quick-1 runs there, but scratch never asks about worktrees
	if cmd == nil {
		t.Fatal("no launch command")
	}
	cmd()
	if o := src.launched[0]; o.Name != "cb-bug-bash-1" || o.Dir != "/home/.claudeboard/scratch" || o.Worktree != "" {
		t.Errorf("launch options = %+v", o)
	}
}

func TestQuickOneOff(t *testing.T) {
	m, src := testModel(t, 100, 16)
	_, cmd := press(t, m, "s")
	if cmd == nil {
		t.Fatal("no command")
	}
	cmd()
	if o := src.launched[0]; o.Name != "cb-quick-2" || o.Dir != "/home/.claudeboard/scratch" {
		t.Errorf("launch options = %+v", o)
	}
}

func TestUntrustedFolderOffersToOpenClaude(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.launchErr = errors.New("claude --bg: Workspace not trusted. Run `claude` in /home/.claudeboard/scratch once and accept the trust prompt, then retry.")
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())
	if m.mode != modeAskTrust || !strings.Contains(m.footer(), "isn't trusted yet") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer())
	}
	m2, cmd := press(t, m, "n")
	if cmd != nil || m2.message != "cancelled" {
		t.Errorf("n: cmd %v, message %q", cmd, m2.message)
	}
	m, cmd = press(t, m, "y")
	if cmd == nil || m.mode != modeList || len(src.trusted) != 1 || src.trusted[0] != "/home/.claudeboard/scratch" {
		t.Fatalf("y: cmd %v, trusted %v", cmd, src.trusted)
	}
	src.launchErr = nil
	m, cmd = send(m, trustDoneMsg{req: m.flow.req})
	if cmd == nil {
		t.Fatal("no retry after trusting")
	}
	cmd()
	if len(src.launched) != 1 {
		t.Errorf("retry didn't launch: %v", src.launched)
	}
}

func TestOtherLaunchErrorsAreShown(t *testing.T) {
	m, src := testModel(t, 100, 16)
	src.launchErr = errors.New("claude --bg: something broke")
	m, cmd := press(t, m, "s")
	m, _ = send(m, cmd())
	if m.mode != modeList || !strings.Contains(m.message, "something broke") {
		t.Errorf("mode %v, message %q", m.mode, m.message)
	}
}

func TestEscCancelsNewSession(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m, _ = press(t, m, "n", "esc")
	if m.mode != modeList || m.message != "cancelled" {
		t.Errorf("mode %v, message %q", m.mode, m.message)
	}
	m, _ = press(t, m, "n", "enter", "esc")
	if m.mode != modeList {
		t.Errorf("esc in the repo picker: mode %v", m.mode)
	}
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/board`
Expected: FAIL, `undefined: trustDoneMsg`

- [ ] **Step 3: Implement** — `internal/board/newflow.go`

```go
package board

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// newWorkflowChoice marks the "+ new workflow…" entry in the workflow picker.
const newWorkflowChoice = "\x00new"

// launchedMsg reports a launch; on success the board opens the new session.
type launchedMsg struct {
	sess store.Session
	req  launch.Request
	err  error
}

func (msg launchedMsg) apply(m Model) (Model, tea.Cmd) {
	if msg.err != nil {
		if claudecli.IsUntrusted(msg.err) {
			m.flow.req, m.mode, m.message = msg.req, modeAskTrust, ""
			return m, nil
		}
		m.message = "couldn't start a session: " + msg.err.Error()
		return m, nil
	}
	m.message = "started " + msg.sess.Name
	return m, m.attach(msg.sess.ShortID)
}

// trustDoneMsg arrives when the user leaves the claude they opened to accept the trust
// prompt; the launch is then retried.
type trustDoneMsg struct{ req launch.Request }

func (msg trustDoneMsg) apply(m Model) (Model, tea.Cmd) {
	cmd := m.launchCmd(msg.req)
	return m, cmd
}

// startNew opens the workflow picker, starting on the selected row's workflow.
func (m Model) startNew() (tea.Model, tea.Cmd) {
	sel := ""
	if r, ok := m.selected(); ok && (r.Kind == RowWorkflow || r.Kind == RowSession) {
		sel = r.Workflow.Slug
	}
	var choices []choice
	cursor := 0
	for _, wf := range m.state.Workflows {
		if wf.Archived {
			continue
		}
		if wf.Slug == sel {
			cursor = len(choices)
		}
		choices = append(choices, choice{label: wf.Name, value: wf.Name})
	}
	choices = append(choices, choice{label: "+ new workflow…", value: newWorkflowChoice})
	m.flow = newFlow{choices: choices, cursor: cursor}
	m.mode = modePickWorkflow
	return m, nil
}

// quick starts a session in the scratch folder, in the quick workflow, with no questions.
func (m Model) quick() (tea.Model, tea.Cmd) {
	dir := m.deps.Cfg.ScratchDir
	m.flow.req = launch.Request{Workflow: store.QuickSlug, Dir: dir, Repo: m.deps.Cfg.RepoName(dir)}
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) pickerKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc", "q":
		m.mode, m.message = modeList, "cancelled"
	case "up", "k":
		m.flow.cursor = max(0, m.flow.cursor-1)
	case "down", "j":
		m.flow.cursor = min(len(m.flow.choices)-1, m.flow.cursor+1)
	case "enter":
		c := m.flow.choices[m.flow.cursor]
		switch {
		case m.mode == modePickRepo:
			return m.chooseRepo(c.value)
		case c.value != newWorkflowChoice:
			m.pickRepoFor(c.value)
			return m, nil
		}
		m.flow.input = textinput.New()
		m.flow.input.Placeholder = "e.g. payments-v2"
		m.mode = modeNameWorkflow
		cmd := m.flow.input.Focus()
		return m, cmd
	}
	return m, nil
}

func (m Model) nameKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode, m.message = modeList, "cancelled"
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.flow.input.Value())
		if store.Slug(name) == "" {
			m.message = "a workflow name needs at least one letter or digit"
			return m, nil
		}
		m.message = ""
		m.pickRepoFor(name)
		return m, nil
	}
	var cmd tea.Cmd
	m.flow.input, cmd = m.flow.input.Update(k)
	return m, cmd
}

// pickRepoFor opens the folder picker for workflow: configured repos, the current
// folder and the scratch folder, starting on the workflow's last folder.
func (m *Model) pickRepoFor(workflow string) {
	m.flow.req = launch.Request{Workflow: workflow}
	cfg := m.deps.Cfg
	last := ""
	if wf := m.state.Workflow(store.Slug(workflow)); wf != nil {
		last = wf.LastRepo
	}
	var choices []choice
	for _, r := range cfg.RepoList() {
		choices = append(choices, choice{label: r.Name + "  " + r.Dir, value: r.Dir})
	}
	choices = append(choices,
		choice{label: "current folder  " + m.deps.Cwd, value: m.deps.Cwd},
		choice{label: "scratch  " + cfg.ScratchDir, value: cfg.ScratchDir})
	cursor := len(choices) - 2 // current folder
	for i, c := range choices {
		if last != "" && cfg.RepoName(c.value) == last {
			cursor = i
			break
		}
	}
	m.flow.choices, m.flow.cursor = choices, cursor
	m.mode = modePickRepo
}

// chooseRepo launches in dir, first asking about a worktree if a session already runs
// there. The scratch folder isn't a git repo, so it never asks there.
func (m Model) chooseRepo(dir string) (tea.Model, tea.Cmd) {
	m.flow.req.Dir = dir
	m.flow.req.Repo = m.deps.Cfg.RepoName(dir)
	if dir != m.deps.Cfg.ScratchDir && launch.DirBusy(m.state, m.live, dir) {
		m.mode = modeAskWorktree
		return m, nil
	}
	m.mode = modeList
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) answerWorktree(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "esc" {
		m.message = "cancelled"
		return m, nil
	}
	m.flow.req.Worktree = s == "y" || s == "Y"
	cmd := m.launchCmd(m.flow.req)
	return m, cmd
}

func (m Model) answerTrust(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if s := k.String(); s != "y" && s != "Y" {
		m.message = "cancelled"
		return m, nil
	}
	req := m.flow.req
	return m, tea.ExecProcess(m.deps.Src.TrustCmd(req.Dir), func(error) tea.Msg { return trustDoneMsg{req: req} })
}

// launchCmd starts the session in the background; launchedMsg reports back.
func (m *Model) launchCmd(req launch.Request) tea.Cmd {
	m.message = "starting a session…"
	d := m.deps
	return func() tea.Msg {
		sess, err := launch.Run(context.Background(), d.Src, d.Store, d.Cfg, req, d.Now())
		return launchedMsg{sess: sess, req: req, err: err}
	}
}
```

- [ ] **Step 4: Replace `internal/board/keys.go`**

```go
package board

import tea "charm.land/bubbletea/v2"

// commandKey handles the keys that act on the selected row or start sessions.
func (m Model) commandKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		return m.enter()
	case "x":
		return m.stop()
	case "r":
		return m.resume()
	case "D":
		return m.askDelete()
	case "a":
		return m.toggleArchive()
	case "n":
		return m.startNew()
	case "s":
		return m.quick()
	}
	return m, nil
}

// flowKey handles keys while a picker or a y/N question is open.
func (m Model) flowKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modePickWorkflow, modePickRepo:
		return m.pickerKey(k)
	case modeNameWorkflow:
		return m.nameKey(k)
	}
	mode := m.mode
	m.mode = modeList
	switch mode {
	case modeConfirmDelete:
		return m.confirmDelete(k)
	case modeAskWorktree:
		return m.answerWorktree(k)
	case modeAskTrust:
		return m.answerTrust(k)
	}
	return m, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go vet ./... && go test -race ./internal/board && go test ./...`
Expected: PASS

- [ ] **Step 6: Commit and push**

```bash
git add internal/board
git commit -F - <<'EOF'
Add new-session flow, quick one-offs and trust prompt handling

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

---

### Task 16: Run the board, README, end-to-end check

**Files:**
- Create: `cmd/claudeboard/board.go`, `README.md`
- Modify: `cmd/claudeboard/main.go`. In `run`, the `len(args) == 0` branch becomes `return runBoard(stderr)`.

**Interfaces:**
- Consumes: `board.New`, `board.Deps`, `claudecli.New`, `CLI.Version`, `CLI.List`, `CompareVersions`, `MinVersion`, `config.Home`, `config.Load`, `store.Open`, `transcript.ProjectsDir`.
- Produces: `runBoard(stderr io.Writer) int`.

- [ ] **Step 1: Implement** — `cmd/claudeboard/board.go`

```go
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
```

Then in `cmd/claudeboard/main.go`, replace

```go
	if len(args) == 0 {
		fmt.Fprintln(stderr, "claudeboard: the board isn't built yet")
		return 1
	}
```

with

```go
	if len(args) == 0 {
		return runBoard(stderr)
	}
```

- [ ] **Step 2: Build and run tests**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 3: Write `README.md`**

````markdown
# claudeboard

A terminal control panel for Claude Code. See every session you have going, across
all your repos, grouped into workflows. Jump into one with Enter and back out with
Ctrl+Z, start new ones in a couple of keystrokes, and keep an eye on token usage.

It's built on Claude Code's background sessions (`claude --bg`), so sessions keep
running when you quit the board.

```
┌ claudeboard · 1 working · 1 needs you · 65k tokens ───────────────────────────┐
│ payments-v2  !             53k │ cb-payments-v2-2 · web · 12k tokens          │
│   ● api  wire the new Aft… 41k │ in 1.2k · out 8.4k · cache r 2.1k · w 300    │
│ ▸ ! web  hook PaymentForm… 12k │ ! needs you — permission prompt: Bash npm t… │
│ q4-search                  12k │                                              │
│   ✓ api  plan the search…  12k │ you: hook PaymentForm into checkout          │
│ quick                          │ • Edit CheckoutPage.vue                      │
│   ○ scratch  new session       │ • Bash npm test                              │
│ Other sessions                 │ claude: Wired it in. Running the tests…      │
│   ● arbybot-8d                 │                                              │
├────────────────────────────────┴──────────────────────────────────────────────┤
│ ↑↓ move  enter open  tab next waiting  n new  s quick  ? help  q quit         │
└───────────────────────────────────────────────────────────────────────────────┘
```

## Requirements

- macOS (Linux probably works but is untested)
- Claude Code 2.1.312 or newer, with background sessions: `claude agents --json` must print JSON
- Go 1.26 or newer. With an older Go, `go install` downloads 1.26 by itself (`GOTOOLCHAIN=auto`, the default)

## Install

```sh
git clone https://github.com/Harris-Ferguson/claudeboard
cd claudeboard
go install ./cmd/claudeboard
```

That puts `claudeboard` in `$(go env GOPATH)/bin`, which needs to be on your `PATH`.
To update later, run `git pull && go install ./cmd/claudeboard`. `claudeboard --version`
shows which commit you're running.

## Using it

Run `claudeboard`.

| Key | Does |
|---|---|
| ↑↓ / j k | move |
| Enter | open the session; **Ctrl+Z inside it comes back to the board**. On a workflow: fold/unfold |
| Tab | jump to the next session that needs you |
| n | new session: pick a workflow, then a folder, and Claude opens straight away |
| s | quick one-off in the scratch folder, no questions |
| x | stop the session (keeps its conversation and worktree) |
| r | resume a stopped session in the background |
| D | delete the session and its worktree (asks first) |
| a / A | archive a workflow / show archived workflows |
| p | show or hide the preview (hidden by default below 90 columns) |
| PgUp / PgDn | scroll the preview |
| ? | help |
| q | quit; sessions keep running |

Statuses: `●` working, `!` needs you (with what it's waiting for), `✓` done (your turn),
`○` new or stopped.

"Other sessions" lists Claude sessions claudeboard didn't start, such as ones you
started in another terminal. You can preview them, and open, stop or delete the
background ones.

### From any terminal

```sh
claudeboard new                                  # quick session here, opens straight away
claudeboard new -w payments-v2 -r api            # into a workflow, in a configured repo
claudeboard new -w payments-v2 -r api --worktree # in its own git worktree
claudeboard new -w bugs "fix the flaky test"     # with a prompt: starts in the background and returns
```

## Config

`~/.claudeboard/config.toml` is created on first run:

```toml
# scratch_dir = "~/.claudeboard/scratch"   # where quick one-offs run

[repos]               # repos you start sessions in: name = path
api = "~/work/api"
web = "~/work/web"

[defaults]
model = ""            # empty = Claude Code's default
permission_mode = ""  # empty = Claude Code's default
ide = false           # pass --ide so diffs open in your IDE
```

`CLAUDEBOARD_HOME` moves the whole folder somewhere else.

## PHPStorm

- Run `claudeboard` in PHPStorm's terminal tool window.
- By default, Esc in that terminal moves focus to the editor instead of reaching Claude.
  Turn it off in **Settings → Tools → Terminal**: untick **Move focus to the editor with Escape**.
- To have diffs open in PHPStorm, set `ide = true`. This is untested with background
  sessions; see the checklist.

## First-run checklist

1. `claude --version` shows 2.1.312 or newer, and `claude agents --json` prints JSON.
2. Run `claude` once in the clone folder and accept the trust prompt, then
   `go test -tags realclaude -v ./internal/claudecli`. This starts and deletes an idle
   session; no prompt is sent, so it uses no tokens.
3. Add your repos to `~/.claudeboard/config.toml`, then run `claudeboard`.
4. Press `n`, choose "+ new workflow…", name it, and pick a repo. Claude opens. Type a
   prompt, then press Ctrl+Z. The row shows `●` working, then `✓` done, and the preview
   shows the conversation and token counts.
5. When a session asks for permission, the header says "1 needs you" and Tab jumps to it.
6. `x` stops a session (`○ stopped`). Enter reopens it.
7. `D` deletes it. For a worktree session, check the worktree folder is gone.
8. `s` the first time asks to trust the scratch folder. Press `y`, accept in Claude,
   type `/exit`, and the session starts.
9. In PHPStorm's terminal, after the Esc setting above, Esc interrupts Claude.
10. With `ide = true`, start a session that edits a file and see whether the diff opens
    in PHPStorm.

## Troubleshooting

- **"isn't trusted yet"**: Claude Code won't start background sessions in a folder you
  haven't trusted. Run `claude` there once and accept the prompt.
- **"status stale"**: `claude agents --json` is failing or slow. Run it yourself to see why.
- **A session shows "no messages yet" after you've talked to it**: was it started from
  inside another Claude session's shell? claudeboard removes the variables that turn
  off conversation saving, but sessions started some other way may not have saved.

## How it works

claudeboard runs `claude --bg` to start sessions, polls `claude agents --json` for their
state, and hands your terminal to `claude attach` to open one. It reads each session's
conversation file under `~/.claude/projects/` for the preview and token counts, and
keeps its own list of workflows in `~/.claudeboard/state.json`. Sessions it starts are
named `cb-<workflow>-<n>`, so they're recognisable in `claude agents` too. The design is
in `docs/superpowers/specs/2026-10-28-claudeboard-design.md`.

## Development

```sh
go test ./...                                     # everything, no real Claude needed
go test ./internal/board -update                  # rewrite the reference screens
go test -tags realclaude -v ./internal/claudecli  # check against your real Claude Code
```
````

- [ ] **Step 4: End-to-end check in a real terminal** (no tokens: no prompt is sent)

Write the throwaway driver to the scratchpad. It must not be committed.

```bash
S=/private/tmp/claude-502/-Users-harrisferguson-organizer/28a97ac4-b213-4024-84d5-6b1e97e06db0/scratchpad/e2e
mkdir -p "$S" && python3 -m venv "$S/venv" && "$S/venv/bin/pip" install -q pyte
go build -o "$S/claudeboard" ./cmd/claudeboard
cat > "$S/drive.py" <<'EOF'
"""Throwaway: run a command in a pty, type keys on a schedule, print the screen."""
import json, os, pty, select, sys, time, fcntl, termios, struct
import pyte
pyte.Screen.report_device_status = lambda self, *a, **k: None
steps, cwd, cmd = json.loads(sys.argv[1]), sys.argv[2], sys.argv[3:]
screen = pyte.Screen(110, 30); stream = pyte.ByteStream(screen)
pid, fd = pty.fork()
if pid == 0:
    os.chdir(cwd); os.execvp(cmd[0], cmd)
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 110, 0, 0))
def pump(d):
    end = time.time() + d
    while time.time() < end:
        if select.select([fd], [], [], 0.05)[0]:
            try: stream.feed(os.read(fd, 65536))
            except OSError: return
for kind, arg in steps:
    if kind == "wait": pump(arg)
    elif kind == "keys": os.write(fd, arg.encode())
    elif kind == "dump":
        print(f"--- {arg}"); print("\n".join(l.rstrip() for l in screen.display).rstrip())
EOF
```

Run the board with a throwaway home, in this repo (already trusted):

```bash
export CLAUDEBOARD_HOME="$S/home"
"$S/venv/bin/python" "$S/drive.py" '[["wait",3],["dump","empty board"],["keys","n"],["wait",0.5],["dump","workflow picker"],["keys","\r"],["wait",0.5],["keys","e2e"],["keys","\r"],["wait",0.5],["dump","folder picker"],["keys","\r"],["wait",6],["dump","inside claude"],["keys","\u001a"],["wait",3],["dump","back on the board"],["keys","D"],["wait",0.3],["keys","y"],["wait",3],["dump","after delete"],["keys","q"],["wait",1]]' "$PWD" "$S/claudeboard"
```

Expected screens:
- **"empty board":** "No sessions yet".
- **"workflow picker":** only "+ new workflow…".
- **"folder picker":** the cursor is on the current folder.
- **"inside claude":** Claude Code's own interface.
- **"back on the board":** `e2e` with a `○ cb… new session` row underneath.
- **"after delete":** the row is gone, with a "deleted cb-e2e-1" message.

Afterwards, confirm nothing was left behind, then clean up:

```bash
claude agents --json --all | python3 -c "import json,sys;print([r.get('name') for r in json.load(sys.stdin) if (r.get('name') or '').startswith('cb-e2e')])"   # expect []
rm -rf "$S"; unset CLAUDEBOARD_HOME
```

Fix anything that doesn't match, with a regression test where one fits, before going on.

- [ ] **Step 5: Commit and push**

```bash
git add cmd/claudeboard README.md
git commit -F - <<'EOF'
Run the board from claudeboard and add the README

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
git push origin main
```

- [ ] **Step 6: Checkpoint: dev-experience critic.** Dispatch a fresh critic agent (below) on the finished product. Fix what it raises until it scores at least 8/10, pushing after each round. Then update the project memory with where things stand.

---

## Critic checkpoints

The user asked for a critic agent at major checkpoints, scoring the product so far out of 10 with a focus on developer experience, with the goal of reaching at least 8/10. The checkpoints are after Task 10 (the CLI works end to end) and after Task 16 (the board is done). Each round:

1. Dispatch a fresh `general-purpose` agent with the prompt below. Fill in the checkpoint name and anything already fixed in earlier rounds.
2. Triage its list. Fix every in-scope issue, preferring the ones that cost the score the most. Push each fix to main with tests. If an issue is out of the spec's scope, note it under "Later" in the README instead of building it.
3. Re-run a fresh critic, telling it what changed. Stop when it scores 8 or more.

Critic prompt:

```
You are reviewing claudeboard, a Go terminal tool that manages Claude Code background
sessions, from the point of view of a developer who will install and use it every day
(they run 3+ Claude sessions across several repos, mostly from PHPStorm's terminal).
Checkpoint: <after `claudeboard new` | finished board>.

Repo: /Users/harrisferguson/organizer (spec: docs/superpowers/specs/2026-10-28-claudeboard-design.md).

Evaluate the developer experience, not code style:
- Install and update: is it obvious and does it work? (go build ./... and go test ./...)
- First run: what happens with no config, no sessions, missing claude, an untrusted folder?
- Day-to-day use: are the commands, keys, messages and help clear? Is anything surprising,
  slow, or easy to get wrong? Are error messages actionable?
- Docs: does the README get someone from zero to working, including PHPStorm?
- Reliability: anything that could lose sessions, leave junk behind, or hang?

You may run the binary. Use CLAUDEBOARD_HOME=$(mktemp -d) so you don't touch real data.
Do not send prompts to Claude (that spends tokens). Any session you start must be stopped
and removed with `claude stop <id>` and `claude rm <id>` before you finish. Don't edit
files in the repo.

Reply with: a score out of 10; the top issues ranked by how much they hurt the developer
experience, each with a concrete fix; and what's already good (so it isn't broken while
fixing the rest).
```
