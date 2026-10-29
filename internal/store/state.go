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
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	Archived   bool      `json:"archived"`
	LastRepo   string    `json:"last_repo"`
	LastNumber int       `json:"last_number,omitempty"` // highest session number ever used
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
	Cwd       string    `json:"cwd,omitempty"` // the folder Claude last reported the session in
	CreatedAt time.Time `json:"created_at"`
}

// WorkDir is the folder the session runs in: where Claude last reported it, else its
// worktree if it has one, else Dir.
func (s Session) WorkDir() string {
	if s.Cwd != "" {
		return s.Cwd
	}
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
// workflow, counting recorded sessions, extraNames (such as names Claude lists) and
// deleted sessions, so a number (and the worktree named after it) is never reused.
func (st *State) NextSessionNumber(slug string, extraNames ...string) int {
	highest := 0
	if wf := st.Workflow(slug); wf != nil {
		highest = wf.LastNumber
	}
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
			if (l.ShortID != "" && s.ShortID != l.ShortID) || (l.Cwd != "" && s.Cwd != l.Cwd) {
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
			if (l.ShortID != "" && s.ShortID != l.ShortID) || (l.Cwd != "" && s.Cwd != l.Cwd) {
				if l.ShortID != "" {
					s.ShortID = l.ShortID
				}
				if l.Cwd != "" {
					s.Cwd = l.Cwd
				}
				changed++
			}
			continue
		}
		slug, _, ok := ParseSessionName(l.Name)
		if !ok {
			continue
		}
		if s := st.sessionByName(l.Name); s != nil && s.SessionID == "" {
			s.SessionID, s.ShortID, s.Cwd = l.SessionID, l.ShortID, l.Cwd
			changed++
			continue
		}
		if st.Workflow(slug) == nil {
			st.Workflows = append(st.Workflows, Workflow{Slug: slug, Name: slug, CreatedAt: now})
		}
		st.Sessions = append(st.Sessions, Session{
			SessionID: l.SessionID, ShortID: l.ShortID, Name: l.Name,
			Workflow: slug, Repo: l.Cwd, Dir: l.Cwd, Cwd: l.Cwd, CreatedAt: now,
		})
		changed++
	}
	return changed
}
