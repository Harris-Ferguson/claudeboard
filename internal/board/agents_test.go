package board

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// agentsFixture is what cb-payments-v2-1 (id-1, working) gets in the tests below: two
// running subagents, the first with a running subagent of its own, and two finished
// ones, the second started by the first.
func agentsFixture() []transcript.Agent {
	last := testNow.Add(-time.Minute)
	return []transcript.Agent{
		{ID: "a1", Type: "Explore", Description: "find payment code", Last: last, Usage: transcript.Usage{Output: 8000},
			Pending: "Grep AfterPay",
			Items: []transcript.Item{
				{Kind: transcript.ItemPrompt, Text: "Find where payments are handled"},
				{Kind: transcript.ItemTool, Tool: "Grep", Text: "AfterPay"},
			}},
		{ID: "f1", Description: "map the checkout", Ended: true, Last: last, Usage: transcript.Usage{Output: 5000}},
		{ID: "a2", Parent: "a1", Description: "look at the tests", Last: last, Usage: transcript.Usage{Output: 2000}},
		{ID: "f2", Parent: "f1", Description: "list the routes", Ended: true, Last: last, Usage: transcript.Usage{Output: 1000}},
		{ID: "a3", Type: "general-purpose", Description: "review the diff", Last: last, Usage: transcript.Usage{Output: 3000}},
	}
}

// withAgentsModel is testModel with agentsFixture under cb-payments-v2-1.
func withAgentsModel(t *testing.T, w, h int) (Model, *stubSource) {
	t.Helper()
	m, src := testModel(t, w, h)
	snap := m.snaps["id-1"]
	snap.Agents = agentsFixture()
	m.snaps["id-1"] = snap
	m.rebuild()
	return m, src
}

// treeOf is the left pane's agent rows as "tree icon label" lines.
func treeOf(rows []Row) []string {
	var out []string
	for _, r := range rows {
		switch r.Kind {
		case RowAgent:
			out = append(out, r.Tree+r.Status.Icon()+" "+r.Name())
		case RowAgentsFolded:
			out = append(out, r.Tree+ansi.Strip(agentsFoldedLabel(r)))
		}
	}
	return out
}

func TestRunningSubagentsShowAsATree(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 30)
	want := []string{
		"├ ● find payment code",
		"│ └ ● look at the tests",
		"├ ● review the diff",
		"└ ✓ 2 finished  [+]",
	}
	if got := treeOf(m.rows); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if m.rows[2].Kind != RowAgent || m.rows[2].Owner == nil || m.rows[2].Owner.Session.Name != "cb-payments-v2-1" {
		t.Errorf("the subagents don't follow their session: row 2 = %+v", m.rows[2])
	}
	if m.counts.Working != 1 {
		t.Errorf("Working = %d: subagents shouldn't count as sessions", m.counts.Working)
	}
}

func TestEnterOnFinishedLineShowsThem(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 30)
	m.cursor = 5 // "✓ 2 finished"
	m, _ = press(t, m, "enter")
	want := []string{
		"├ ● find payment code",
		"│ └ ● look at the tests",
		"├ ● review the diff",
		"└ ✓ 2 finished",
		"  └ ✓ map the checkout",
		"    └ ✓ list the routes",
	}
	if got := treeOf(m.rows); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r, _ := m.selected(); r.Kind != RowAgentsFolded {
		t.Errorf("cursor moved off the finished line to %+v", r)
	}
	m, _ = press(t, m, "enter")
	if got := treeOf(m.rows); len(got) != 4 {
		t.Errorf("second enter didn't fold them again: %q", got)
	}
}

func TestSubagentsOfAStoppedSessionAreFinished(t *testing.T) {
	owner := Row{Kind: RowSession, Snap: transcript.Snapshot{Agents: agentsFixture()}}
	rows := agentRows(owner, false)
	if len(rows) != 1 || rows[0].Kind != RowAgentsFolded || rows[0].Finished != 5 {
		t.Errorf("rows = %q, want only \"5 finished\"", treeOf(rows))
	}
}

func TestSubagentsFromBeforeARestartAreFinished(t *testing.T) {
	// Their work ended with the session's last run, whatever their files say.
	started := testNow.Add(-30 * time.Second)
	owner := Row{Kind: RowSession, Live: &claudecli.Session{State: "working", StartedAt: started.UnixMilli()},
		Snap: transcript.Snapshot{Agents: []transcript.Agent{
			{ID: "old", Description: "before", Last: started.Add(-time.Minute)},
			{ID: "new", Description: "after", Last: started.Add(time.Second)},
		}}}
	got := treeOf(agentRows(owner, true))
	want := []string{"├ ● after", "└ ✓ 1 finished", "  └ ✓ before"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("tree:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSessionsWithoutSubagentsGetNoRows(t *testing.T) {
	m, _ := testModel(t, 100, 30)
	for _, r := range m.rows {
		if r.Kind == RowAgent || r.Kind == RowAgentsFolded {
			t.Errorf("unexpected row %+v", r)
		}
	}
}

func TestEnterOnSubagentOpensItsSession(t *testing.T) {
	m, src := withAgentsModel(t, 100, 30)
	m.cursor = 3 // "look at the tests", under cb-payments-v2-1
	if _, cmd := press(t, m, "enter"); cmd == nil || len(src.attached) != 1 || src.attached[0] != "aaaa0001" {
		t.Errorf("cmd %v, attached %v, want cb-payments-v2-1 (aaaa0001)", cmd, src.attached)
	}
}

func TestMoveOnSubagentMovesItsSession(t *testing.T) {
	// Enter on a subagent of an interactive Other session says "press m"; m must then
	// act on that session.
	m, _ := testModel(t, 100, 30)
	snap := m.snaps["id-other"]
	snap.Found, snap.Agents = true, []transcript.Agent{{ID: "a1", Description: "dig", Last: testNow}}
	m.snaps["id-other"] = snap
	m.rebuild()
	m.cursor = len(m.rows) - 1
	if r, _ := m.selected(); r.Kind != RowAgent {
		t.Fatalf("last row is %+v, want arbybot-8d's subagent", r)
	}
	m, _ = press(t, m, "m")
	if m.mode != modePickWorkflow || m.flow.move == nil || m.flow.move.Name() != "arbybot-8d" {
		t.Errorf("mode %v, moving %+v, message %q", m.mode, m.flow.move, m.message)
	}
}

func TestStopAndDeleteIgnoreSubagentRows(t *testing.T) {
	m, src := withAgentsModel(t, 100, 30)
	m.cursor = 2
	for _, k := range []string{"x", "D"} {
		if m2, cmd := press(t, m, k); cmd != nil || m2.mode != modeList {
			t.Errorf("%s on a subagent row: cmd %v, mode %v", k, cmd, m2.mode)
		}
	}
	if len(src.stopped)+len(src.removed) != 0 {
		t.Errorf("stopped %v, removed %v", src.stopped, src.removed)
	}
}

func TestSubagentPreview(t *testing.T) {
	m, _ := withAgentsModel(t, 110, 20)
	m.cursor = 2 // "find payment code"
	plain := ansi.Strip(m.render())
	for _, want := range []string{
		"find payment code · subagent of cb-payments-v2-1 · 8.0k tokens",
		"Explore · in 0 · out 8.0k",
		"● working: Grep AfterPay · enter opens cb-payments-v2-1",
		"task: Find where payments are handled",
		"› Grep  AfterPay",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("preview is missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "you: Find") {
		t.Error("the subagent's task is labelled as yours")
	}
	golden(t, "agents", plain)
}

func TestFinishedLinePreviewListsThem(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 20)
	m.cursor = 5
	plain := ansi.Strip(m.render())
	for _, want := range []string{
		"cb-payments-v2-1 · 2 finished subagents · 6.0k tokens",
		"✓ map the checkout",
		"✓ list the routes",
		"enter shows them",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("preview is missing %q:\n%s", want, plain)
		}
	}
}

func TestLookingAtASubagentDoesNotMarkItsSessionSeen(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 30)
	m.state.Seen = map[string]int{"id-1": 0}
	snap := m.snaps["id-1"]
	snap.Activity = 4
	m.snaps["id-1"] = snap
	m.cursor = 2
	m.noteCursor()
	m.deps.Now = func() time.Time { return testNow.Add(time.Hour) }
	m.noteSeen("")
	if m.state.Seen["id-1"] != 0 {
		t.Errorf("seen = %d after resting on one of its subagents", m.state.Seen["id-1"])
	}
}

func TestSubagentRowStatus(t *testing.T) {
	m, _ := withAgentsModel(t, 100, 30)
	if r := m.rows[2]; r.Status.Kind != status.Working || r.Tokens != 8000 {
		t.Errorf("running subagent: status %v, tokens %d", r.Status, r.Tokens)
	}
}
