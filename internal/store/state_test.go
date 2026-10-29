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
