package store

import (
	"strings"
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

func TestEnsureWorkflowUnarchivesIt(t *testing.T) {
	st := State{Workflows: []Workflow{{Slug: "q4-search", Name: "Q4 search", Archived: true}}}
	wf, err := st.EnsureWorkflow("q4-search", now)
	if err != nil || wf.Archived || st.Workflows[0].Archived || len(st.Workflows) != 1 {
		t.Errorf("EnsureWorkflow = %+v, %v; workflows %+v", wf, err, st.Workflows)
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

func TestRelinkStartsTheSessionsItLinksUnseen(t *testing.T) {
	st := State{
		Workflows: []Workflow{{Slug: "w", Name: "w"}},
		Sessions:  []Session{{Name: "cb-w-2", Workflow: "w"}}, // launched, but its ID was never resolved
		Seen:      map[string]int{"id-4": 7},
	}
	st.Relink([]Listed{
		{SessionID: "id-2", ShortID: "s2", Name: "cb-w-2"},
		{SessionID: "id-3", ShortID: "s3", Name: "cb-bugs-1"},
		{SessionID: "id-4", ShortID: "s4", Name: "cb-bugs-2"}, // already has a record
	}, now)
	for _, id := range []string{"id-2", "id-3"} {
		if seen, ok := st.Seen[id]; !ok || seen != 0 {
			t.Errorf("%s: seen = %v; claudeboard's own sessions start with nothing seen", id, st.Seen)
		}
	}
	if st.Seen["id-4"] != 7 {
		t.Errorf("id-4: seen = %d, want the record it had (7) kept", st.Seen["id-4"])
	}
}

func TestWorkDirPrefersTheFolderClaudeReports(t *testing.T) {
	s := Session{Dir: "/r/sub", Worktree: "w-2", Cwd: "/r/.claude/worktrees/w-2"}
	if got := s.WorkDir(); got != "/r/.claude/worktrees/w-2" {
		t.Errorf("WorkDir = %q", got)
	}
}

func TestRelinkTracksTheReportedFolder(t *testing.T) {
	st := State{Workflows: []Workflow{{Slug: "w"}}, Sessions: []Session{{SessionID: "id-1", ShortID: "a", Name: "cb-w-1", Dir: "/r"}}}
	listed := []Listed{{SessionID: "id-1", ShortID: "a", Name: "cb-w-1", Cwd: "/r/.claude/worktrees/w-1"}}
	if !st.NeedsRelink(listed) {
		t.Fatal("a changed folder should need a relink")
	}
	st.Relink(listed, now)
	if got := st.Session("id-1").Cwd; got != "/r/.claude/worktrees/w-1" {
		t.Errorf("Cwd = %q", got)
	}
	if st.NeedsRelink(listed) {
		t.Error("NeedsRelink still true after Relink")
	}
}

func TestNextSessionNumberNeverReusesANumber(t *testing.T) {
	st := State{Workflows: []Workflow{{Slug: "w", LastNumber: 5}}, Sessions: []Session{{Name: "cb-w-2"}}}
	if n := st.NextSessionNumber("w"); n != 6 {
		t.Errorf("NextSessionNumber = %d, want 6 (5 was used by a deleted session)", n)
	}
}

func TestSeenIsKeptAndForgottenWithItsSession(t *testing.T) {
	st := Open(t.TempDir())
	if err := st.Update(func(s *State) error {
		s.UpsertSession(Session{SessionID: "a", Name: "cb-w-1"})
		s.MarkSeen("a", 4)
		s.MarkSeen("other", 2)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s, _, _ := st.Load()
	if s.Seen["a"] != 4 || s.Seen["other"] != 2 {
		t.Fatalf("Seen = %v", s.Seen)
	}
	s.RemoveSession("a", "")
	if _, ok := s.Seen["a"]; ok {
		t.Error("a deleted session's read state should go with it")
	}
}

func movable() State {
	return State{
		Workflows: []Workflow{{Slug: "payments-v2", Name: "payments-v2"}, {Slug: "q4-search", Name: "Q4 search"}},
		Sessions: []Session{
			{SessionID: "id-1", ShortID: "a1", Name: "cb-payments-v2-1", Workflow: "payments-v2", Repo: "api", Dir: "/w/api", Worktree: "payments-v2-1"},
			{Name: "cb-payments-v2-2", Workflow: "payments-v2", Repo: "web", Dir: "/w/web"}, // ID not known yet
		},
	}
}

func TestMoveSessionChangesOnlyItsWorkflow(t *testing.T) {
	st := movable()
	before := st.Sessions[0]
	wf, err := st.MoveSession(Session{SessionID: "id-1", Name: "cb-payments-v2-1"}, "Q4 search", now)
	if err != nil || wf.Slug != "q4-search" {
		t.Fatalf("MoveSession = %+v, %v", wf, err)
	}
	after := st.Sessions[0]
	before.Workflow = "q4-search"
	if after != before {
		t.Errorf("recorded %+v, want %+v (name, worktree and the rest kept)", after, before)
	}
}

func TestMoveSessionIntoANewWorkflow(t *testing.T) {
	st := movable()
	wf, err := st.MoveSession(Session{SessionID: "id-1"}, "Hotfixes", now)
	if err != nil || wf.Slug != "hotfixes" || wf.Name != "Hotfixes" || len(st.Workflows) != 3 {
		t.Fatalf("MoveSession = %+v, %v; workflows %+v", wf, err, st.Workflows)
	}
	if st.Sessions[0].Workflow != "hotfixes" {
		t.Errorf("session in %q", st.Sessions[0].Workflow)
	}
}

func TestMoveSessionKnownOnlyByName(t *testing.T) {
	st := movable()
	if _, err := st.MoveSession(Session{Name: "cb-payments-v2-2"}, "q4-search", now); err != nil {
		t.Fatal(err)
	}
	if st.Sessions[1].Workflow != "q4-search" || st.Sessions[0].Workflow != "payments-v2" {
		t.Errorf("sessions %+v", st.Sessions)
	}
}

func TestMoveSessionIntoAnArchivedWorkflowUnarchivesIt(t *testing.T) {
	st := movable()
	st.Workflows[1].Archived = true
	wf, err := st.MoveSession(Session{SessionID: "id-1"}, "Q4 Search!", now)
	if err != nil || wf.Slug != "q4-search" || wf.Archived || st.Workflows[1].Archived {
		t.Fatalf("MoveSession = %+v, %v; workflows %+v", wf, err, st.Workflows)
	}
	if st.Sessions[0].Workflow != "q4-search" {
		t.Errorf("session in %q", st.Sessions[0].Workflow)
	}
}

func TestMoveSessionRefusals(t *testing.T) {
	st := movable()
	if _, err := st.MoveSession(Session{SessionID: "id-1"}, "Payments V2", now); err == nil || !strings.Contains(err.Error(), "already in") {
		t.Errorf("into its own workflow: %v", err)
	}
	if _, err := st.MoveSession(Session{SessionID: "gone", Name: "cb-payments-v2-1"}, "q4-search", now); err == nil {
		t.Error("a session the state doesn't have should be refused, not matched by name")
	}
	if _, err := st.MoveSession(Session{SessionID: "id-1"}, "!!!", now); err == nil {
		t.Error("a workflow name with no letters or digits should be refused")
	}
	if st.Sessions[0].Workflow != "payments-v2" || len(st.Workflows) != 2 {
		t.Errorf("a refused move changed the state: %+v", st)
	}
}

func TestRelinkKeepsAMovedSessionWhereItIs(t *testing.T) {
	st := movable()
	if _, err := st.MoveSession(Session{SessionID: "id-1"}, "q4-search", now); err != nil {
		t.Fatal(err)
	}
	st.Relink([]Listed{{SessionID: "id-1", ShortID: "a2", Name: "cb-payments-v2-1", Cwd: "/w/api"}}, now)
	if s := st.Session("id-1"); s.Workflow != "q4-search" || s.ShortID != "a2" {
		t.Errorf("after relink %+v", s)
	}
}

func TestSetLabel(t *testing.T) {
	st := movable()
	if err := st.SetLabel(Session{SessionID: "id-1"}, "  checkout button  "); err != nil {
		t.Fatal(err)
	}
	if got := st.Sessions[0].Label; got != "checkout button" {
		t.Errorf("Label = %q", got)
	}
	if err := st.SetLabel(Session{Name: "cb-payments-v2-2"}, "web bits"); err != nil || st.Sessions[1].Label != "web bits" {
		t.Errorf("by name: %v, %q", err, st.Sessions[1].Label)
	}
	if err := st.SetLabel(Session{SessionID: "id-1"}, " "); err != nil || st.Sessions[0].Label != "" {
		t.Errorf("clearing: %v, %q", err, st.Sessions[0].Label)
	}
	if err := st.SetLabel(Session{SessionID: "gone"}, "x"); err == nil {
		t.Error("a session the state doesn't have should be refused")
	}
}
