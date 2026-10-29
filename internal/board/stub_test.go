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
