// Package launch starts a new claudeboard session and records it in the store. The
// board and `claudeboard new` both use it.
package launch

import (
	"context"
	"fmt"
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
	// Resume is a session ID whose conversation the new session continues. Claude
	// starts a copy, leaving the original as it was; claudeboard uses it for an
	// interactive session that is still open in another terminal.
	Resume string
}

// How long Run waits for a new session to show up in `claude agents`.
var (
	pollInterval = 300 * time.Millisecond
	pollAttempts = 10
)

// Run starts the session and records it. It holds the store lock while launching, so
// two launches can't pick the same session number, and nothing is recorded if Claude
// refuses to start (for example in an untrusted folder; see claudecli.IsUntrusted).
// The full session ID is looked up afterwards, without the lock, and the session is
// recorded as having nothing seen, so its first result shows as unread on the board.
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
			Resume:         req.Resume,
		})
		if err != nil {
			return err
		}
		sess.ShortID = shortID
		wf.LastRepo, wf.LastNumber = req.Repo, n
		s.UpsertSession(sess)
		return nil
	})
	if err != nil {
		return store.Session{}, err
	}
	if id := resolveSessionID(ctx, l, sess.ShortID); id != "" {
		sess.SessionID = id
		if err := st.Update(func(s *store.State) error {
			s.UpsertSession(sess)
			s.MarkNew(id)
			return nil
		}); err != nil {
			return sess, err
		}
	}
	return sess, nil
}

// Adopt records a background session claudeboard didn't start as part of workflow,
// without restarting it: Claude can't rename a session, and resuming one with new
// options starts a copy. The session keeps its name, or goes by its short ID when it
// has none (as stopped sessions are listed).
func Adopt(st *store.Store, cfg config.Config, workflow string, l claudecli.Session, now time.Time) (store.Session, error) {
	if l.Kind != claudecli.KindBackground || l.SessionID == "" {
		return store.Session{}, fmt.Errorf("only background sessions can be added to a workflow as they are")
	}
	sess := store.Session{SessionID: l.SessionID, ShortID: l.ShortID, Name: l.Name,
		Repo: cfg.RepoName(l.Cwd), Dir: l.Cwd, Cwd: l.Cwd, CreatedAt: now}
	if sess.Name == "" {
		sess.Name = l.ShortID
	}
	err := st.Update(func(s *store.State) error {
		wf, err := s.EnsureWorkflow(workflow, now)
		if err != nil {
			return err
		}
		sess.Workflow = wf.Slug
		wf.LastRepo = sess.Repo
		// Match by ID only: names aren't unique, and UpsertSession's name fallback
		// would replace another session's record.
		if existing := s.Session(sess.SessionID); existing != nil {
			*existing = sess
		} else {
			s.Sessions = append(s.Sessions, sess)
		}
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
