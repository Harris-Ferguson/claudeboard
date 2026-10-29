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
