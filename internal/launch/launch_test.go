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

func TestRunStartsTheSessionUnseen(t *testing.T) {
	st := store.Open(t.TempDir())
	sess, err := Run(context.Background(), &stubLauncher{}, st, config.Config{}, Request{Workflow: "bugs", Dir: "/r", Prompt: "fix it"}, now)
	if err != nil {
		t.Fatal(err)
	}
	state, _, _ := st.Load()
	if seen, ok := state.Seen[sess.SessionID]; !ok || seen != 0 {
		t.Errorf("seen = %v; nothing of a new session has been seen, so its first result shows as unread", state.Seen)
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

// lockProbe checks, while Run waits for the new session to be listed, whether another
// store writer (such as the board) can get in.
type lockProbe struct {
	stubLauncher
	st          *store.Store
	probed      bool
	writerGotIn bool
}

func (l *lockProbe) List(ctx context.Context) ([]claudecli.Session, error) {
	if len(l.launched) > 0 && !l.probed {
		l.probed = true
		done := make(chan struct{})
		go func() {
			_ = l.st.Update(func(*store.State) error { return nil })
			close(done)
		}()
		select {
		case <-done:
			l.writerGotIn = true
		case <-time.After(300 * time.Millisecond):
		}
	}
	return l.live, nil
}

func TestRunDoesNotHoldTheLockWhileWaitingForTheSession(t *testing.T) {
	st := store.Open(t.TempDir())
	l := &lockProbe{st: st}
	sess, err := Run(context.Background(), l, st, config.Config{}, Request{Dir: "/s"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !l.writerGotIn {
		t.Error("other writers were locked out while Run waited for claude agents to list the session")
	}
	if state, _, _ := st.Load(); sess.SessionID == "" || state.Session(sess.SessionID) == nil {
		t.Errorf("session ID not recorded: %+v", sess)
	}
}

func TestRunDoesNotReuseTheNumberOfADeletedSession(t *testing.T) {
	l, st := &stubLauncher{}, store.Open(t.TempDir())
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := Run(ctx, l, st, config.Config{}, Request{Workflow: "w", Dir: "/r"}, now); err != nil {
			t.Fatal(err)
		}
	}
	// cb-w-2 is deleted (claude rm may keep its worktree, w-2, if it has unpushed work)
	if err := st.Update(func(s *store.State) error { s.RemoveSession("", "cb-w-2"); return nil }); err != nil {
		t.Fatal(err)
	}
	l.live = l.live[:1]
	sess, err := Run(ctx, l, st, config.Config{}, Request{Workflow: "w", Dir: "/r", Worktree: true}, now)
	if err != nil || sess.Name != "cb-w-3" || sess.Worktree != "w-3" {
		t.Errorf("session = %+v, %v; want cb-w-3 in worktree w-3", sess, err)
	}
}

func TestRunCanContinueAnotherConversation(t *testing.T) {
	l, st := &stubLauncher{}, store.Open(t.TempDir())
	sess, err := Run(context.Background(), l, st, config.Config{}, Request{Workflow: "bots", Dir: "/work/bot", Repo: "/work/bot", Resume: "sess-x"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if o := l.launched[0]; o.Resume != "sess-x" || o.Name != "cb-bots-1" || o.Dir != "/work/bot" {
		t.Errorf("launch options = %+v", o)
	}
	if state, _, _ := st.Load(); state.Session(sess.SessionID) == nil || sess.Workflow != "bots" {
		t.Errorf("session not recorded: %+v", sess)
	}
}

func TestAdoptRecordsABackgroundSessionAsItIs(t *testing.T) {
	st := store.Open(t.TempDir())
	cfg := config.Config{Repos: map[string]string{"api": "/work/api"}}
	l := claudecli.Session{ShortID: "2a2165a3", SessionID: "sess-bg", Kind: claudecli.KindBackground, State: "stopped", Cwd: "/work/api"}
	sess, err := Adopt(st, cfg, "Payments V2", l, now)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Session{SessionID: "sess-bg", ShortID: "2a2165a3", Name: "2a2165a3", Workflow: "payments-v2",
		Repo: "api", Dir: "/work/api", Cwd: "/work/api", CreatedAt: now}
	if sess != want {
		t.Errorf("session = %+v, want %+v (a stopped session has no name, so it goes by its short ID)", sess, want)
	}
	state, _, _ := st.Load()
	if got := state.Session("sess-bg"); got == nil || *got != want {
		t.Errorf("recorded %+v", got)
	}
	if wf := state.Workflow("payments-v2"); wf == nil || wf.Name != "Payments V2" || wf.LastRepo != "api" {
		t.Errorf("workflow = %+v", wf)
	}
	l.Name = "my-agent"
	if sess, _ := Adopt(st, cfg, "Payments V2", l, now); sess.Name != "my-agent" {
		t.Errorf("a named session keeps its name, got %q", sess.Name)
	}
}

func TestAdoptKeepsAnotherSessionWithTheSameName(t *testing.T) {
	st := store.Open(t.TempDir())
	a := claudecli.Session{ShortID: "aaaa0001", SessionID: "sess-a", Name: "fix-tests", Kind: claudecli.KindBackground, Cwd: "/work/api"}
	b := claudecli.Session{ShortID: "bbbb0001", SessionID: "sess-b", Name: "fix-tests", Kind: claudecli.KindBackground, Cwd: "/work/api"}
	if _, err := Adopt(st, config.Config{}, "Payments V2", a, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Adopt(st, config.Config{}, "Q4 Search", b, now); err != nil {
		t.Fatal(err)
	}
	state, _, _ := st.Load()
	if got := state.Session("sess-a"); got == nil || got.Workflow != "payments-v2" {
		t.Errorf("sess-a = %+v, want it kept in payments-v2 (names aren't unique)", got)
	}
	if got := state.Session("sess-b"); got == nil || got.Workflow != "q4-search" {
		t.Errorf("sess-b = %+v, want it in q4-search", got)
	}
}

func TestAdoptRefusesInteractiveSessions(t *testing.T) {
	l := claudecli.Session{SessionID: "sess-i", Kind: claudecli.KindInteractive}
	if _, err := Adopt(store.Open(t.TempDir()), config.Config{}, "w", l, now); err == nil {
		t.Error("an interactive session can't be recorded as a background one")
	}
}
