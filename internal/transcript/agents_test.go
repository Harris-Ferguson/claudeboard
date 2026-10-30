package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// at is 10:00:00 UTC on 2026-10-29 plus sec seconds, as Claude Code writes timestamps.
func at(sec int) string {
	return time.Date(2026, 10, 29, 10, 0, sec, 0, time.UTC).Format("2006-01-02T15:04:05.000Z")
}

// agentCall is Claude starting a subagent with the Agent tool.
func agentCall(sec int, toolUseID, description string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":"m-%s","content":[{"type":"tool_use","id":%q,"name":"Agent","input":{"description":%q,"prompt":"go"}}]}}`+"\n",
		at(sec), toolUseID, toolUseID, description)
}

// toolResult is the result of tool call toolUseID.
func toolResult(sec int, toolUseID, text string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"content":[{"type":"tool_result","tool_use_id":%q,"content":%q}]}}`+"\n",
		at(sec), toolUseID, text)
}

// notice is Claude Code queueing a "task finished" notification for agent agentID.
func notice(sec int, agentID, toolUseID string) string {
	content := fmt.Sprintf("<task-notification>\n<task-id>%s</task-id>\n<tool-use-id>%s</tool-use-id>\n<status>completed</status>\n</task-notification>", agentID, toolUseID)
	return fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","timestamp":%q,"content":%q}`+"\n", at(sec), content)
}

// subPrompt and subStep are lines of a subagent's own file, which marks them sidechain.
func subPrompt(sec int, text string) string {
	return fmt.Sprintf(`{"type":"user","isSidechain":true,"timestamp":%q,"message":{"content":%q}}`+"\n", at(sec), text)
}

func subStep(sec int, id, command string, out int) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":true,"timestamp":%q,"message":{"id":%q,"content":[{"type":"tool_use","id":"t-%s","name":"Bash","input":{"command":%q}}],"usage":{"output_tokens":%d}}}`+"\n",
		at(sec), id, id, command, out)
}

// writeAgent writes agent-<id>.jsonl and, if meta isn't empty, agent-<id>.meta.json
// next to the session file at mainPath. It returns the conversation file's path.
func writeAgent(t *testing.T, mainPath, id, content, meta string) string {
	t.Helper()
	dir := filepath.Join(strings.TrimSuffix(mainPath, ".jsonl"), "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent-"+id+".jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta != "" {
		if err := os.WriteFile(filepath.Join(dir, "agent-"+id+".meta.json"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func backgroundMeta(toolUseID, agentType, description string) string {
	return fmt.Sprintf(`{"agentType":%q,"description":%q,"toolUseId":%q,"spawnDepth":1,"requestShape":"background"}`, agentType, description, toolUseID)
}

func TestReaderListsSubagents(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", promptLine+agentCall(1, "tu1", "find payment code")+
		toolResult(2, "tu1", "Async agent launched successfully.\nagentId: a1"))
	writeAgent(t, main, "a1", subPrompt(1, "Find where payments are handled")+subStep(5, "sm1", "grep -r pay", 40),
		backgroundMeta("tu1", "Explore", "find payment code"))

	s := mustUpdate(t, NewReader(projects, "s1"))
	if len(s.Agents) != 1 {
		t.Fatalf("Agents = %+v, want one", s.Agents)
	}
	a := s.Agents[0]
	if a.ID != "a1" || a.Type != "Explore" || a.Description != "find payment code" || a.Parent != "" {
		t.Errorf("agent = %+v", a)
	}
	if a.Ended {
		t.Error("Ended before Claude recorded that it finished")
	}
	if a.Pending != "Bash grep -r pay" || a.Usage.Output != 40 || !a.Last.Equal(time.Date(2026, 10, 29, 10, 0, 5, 0, time.UTC)) {
		t.Errorf("Pending %q, output %d, Last %v", a.Pending, a.Usage.Output, a.Last)
	}
	if len(a.Items) != 2 || a.Items[0].Kind != ItemPrompt || a.Items[1].Tool != "Bash" {
		t.Errorf("Items = %+v, want its prompt and its tool call", a.Items)
	}
	if s.Usage.Output != 40 || len(s.Items) != 2 {
		t.Errorf("session: output %d (want the subagent's 40 included), %d items (want the main file's 2)", s.Usage.Output, len(s.Items))
	}
}

func TestSubagentEndsWithANoticeAfterItsLastStep(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "review")+toolResult(2, "tu1", "Async agent launched"))
	sub := writeAgent(t, main, "a1", subPrompt(1, "review it")+subStep(5, "sm1", "go test", 10), backgroundMeta("tu1", "general-purpose", "review"))
	r := NewReader(projects, "s1")
	if mustUpdate(t, r).Agents[0].Ended {
		t.Fatal("Ended without a notice")
	}

	appendTo(t, main, notice(6, "a1", "tu1"))
	if !mustUpdate(t, r).Agents[0].Ended {
		t.Error("not Ended after the notice")
	}

	// Sent another message, it works again: the old notice no longer counts. The new
	// notice names the SendMessage call, not the Agent one, so only the ID matches.
	appendTo(t, sub, subStep(9, "sm2", "go vet", 10))
	if mustUpdate(t, r).Agents[0].Ended {
		t.Error("Ended after working past its notice")
	}
	appendTo(t, main, notice(12, "a1", "tu-send"))
	if !mustUpdate(t, r).Agents[0].Ended {
		t.Error("not Ended after the second notice")
	}
}

func TestBackgroundSubagentDoesNotEndWithItsLaunchResult(t *testing.T) {
	// A background subagent's call gets its result at once, often before the subagent
	// has written anything after its prompt.
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "review")+toolResult(2, "tu1", "Async agent launched"))
	writeAgent(t, main, "a1", subPrompt(1, "review it"), backgroundMeta("tu1", "general-purpose", "review"))
	if mustUpdate(t, NewReader(projects, "s1")).Agents[0].Ended {
		t.Error("Ended by the result saying it was launched")
	}
}

func TestForegroundSubagentEndsWithItsResult(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "look around"))
	writeAgent(t, main, "a1", subPrompt(1, "look")+subStep(4, "sm1", "ls", 5),
		`{"agentType":"Explore","description":"look around","toolUseId":"tu1","requestShape":"foreground"}`)
	r := NewReader(projects, "s1")
	if mustUpdate(t, r).Agents[0].Ended {
		t.Fatal("Ended while its call has no result")
	}
	appendTo(t, main, toolResult(8, "tu1", "Here is what I found"))
	if !mustUpdate(t, r).Agents[0].Ended {
		t.Error("not Ended after its call got a result")
	}
}

func TestSubagentStartedByASubagentNamesItsParent(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "plan"))
	writeAgent(t, main, "a1", subPrompt(1, "plan it")+agentCall(3, "tu2", "dig into tests"), backgroundMeta("tu1", "Plan", "plan"))
	writeAgent(t, main, "a2", subPrompt(4, "dig"), backgroundMeta("tu2", "Explore", "dig into tests"))

	s := mustUpdate(t, NewReader(projects, "s1"))
	if len(s.Agents) != 2 || s.Agents[0].ID != "a1" || s.Agents[1].ID != "a2" {
		t.Fatalf("Agents = %+v, want a1 then a2, in the order they started", s.Agents)
	}
	if s.Agents[0].Parent != "" || s.Agents[1].Parent != "a1" {
		t.Errorf("parents %q and %q, want \"\" and a1", s.Agents[0].Parent, s.Agents[1].Parent)
	}
}

func TestSubagentMarkedFinishedWithoutANotice(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "x"))
	writeAgent(t, main, "a1", subPrompt(1, "x"),
		`{"agentType":"general-purpose","description":"x","toolUseId":"tu1","requestShape":"background","finished":true}`)
	if !mustUpdate(t, NewReader(projects, "s1")).Agents[0].Ended {
		t.Error("not Ended though Claude marked it finished")
	}
}

func TestSubagentWithoutMetaStillListed(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", promptLine)
	writeAgent(t, main, "a1", subPrompt(1, "x")+subStep(2, "sm1", "ls", 7), "")
	s := mustUpdate(t, NewReader(projects, "s1"))
	if len(s.Agents) != 1 || s.Agents[0].ID != "a1" || s.Agents[0].Usage.Output != 7 {
		t.Errorf("Agents = %+v", s.Agents)
	}
}

func TestSubagentMetaIsReread(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", agentCall(1, "tu1", "x"))
	writeAgent(t, main, "a1", subPrompt(1, "x"), backgroundMeta("tu1", "general-purpose", "x"))
	r := NewReader(projects, "s1")
	mustUpdate(t, r)
	metaPath := filepath.Join(strings.TrimSuffix(main, ".jsonl"), "subagents", "agent-a1.meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"description":"x","toolUseId":"tu1","requestShape":"background","finished":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(metaPath, later, later); err != nil {
		t.Fatal(err)
	}
	if !mustUpdate(t, r).Agents[0].Ended {
		t.Error("a changed meta file wasn't read again")
	}
}

// forkCall is a record of message msgID holding an Agent call that starts a fork. The
// fork's own file repeats it, sidechain, with the same message ID and usage.
func forkCall(sec int, msgID, toolUseID string, cacheWrite, out int, sidechain bool) string {
	return fmt.Sprintf(`{"type":"assistant","isSidechain":%v,"timestamp":%q,"message":{"id":%q,"content":[{"type":"tool_use","id":%q,"name":"Agent","input":{"description":"refactor","prompt":"go","subagent_type":"fork"}}],"usage":{"input_tokens":2,"output_tokens":%d,"cache_creation_input_tokens":%d}}}`+"\n",
		sidechain, at(sec), msgID, toolUseID, out, cacheWrite)
}

// subResult is the result of tool call toolUseID in a subagent's own file.
func subResult(sec int, toolUseID, text string) string {
	return strings.Replace(toolResult(sec, toolUseID, text), `"type":"user"`, `"type":"user","isSidechain":true`, 1)
}

func forkMeta(toolUseID string) string {
	return fmt.Sprintf(`{"agentType":"fork","isFork":true,"description":"refactor","toolUseId":%q,"spawnDepth":1,"requestShape":"background"}`, toolUseID)
}

func TestForkDoesNotDoubleCountItsParentsMessage(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", promptLine+forkCall(1, "m-parent", "tu1", 50000, 3000, false)+
		toolResult(2, "tu1", "Async agent launched"))
	writeAgent(t, main, "a1",
		`{"type":"fork-context-ref"}`+"\n"+
			forkCall(1, "m-parent", "tu1", 50000, 3000, true)+
			subResult(2, "tu1", "fork started")+
			subStep(5, "own1", "ls", 100),
		forkMeta("tu1"))
	s := mustUpdate(t, NewReader(projects, "s1"))
	if want := (Usage{Input: 2, Output: 3100, CacheWrite: 50000}); s.Usage != want {
		t.Errorf("session usage = %+v, want %+v", s.Usage, want)
	}
	if a := s.Agents[0]; a.Usage != (Usage{Output: 100}) || a.Parent != "" {
		t.Errorf("fork usage %+v, Parent %q; want only its own step, and the session as its parent", a.Usage, a.Parent)
	}
}

func TestForkOfASubagentDoesNotDoubleCountItsParentsMessage(t *testing.T) {
	projects := t.TempDir()
	main := writeSession(t, projects, "s1", promptLine+agentCall(1, "tu1", "plan")+toolResult(2, "tu1", "Async agent launched"))
	writeAgent(t, main, "a1",
		subPrompt(1, "plan it")+forkCall(3, "m-plan", "tu2", 40000, 2000, true)+subResult(4, "tu2", "Async agent launched"),
		backgroundMeta("tu1", "Plan", "plan"))
	writeAgent(t, main, "a2",
		`{"type":"fork-context-ref"}`+"\n"+
			forkCall(3, "m-plan", "tu2", 40000, 2000, true)+
			subResult(4, "tu2", "fork started")+
			subStep(6, "own2", "ls", 100),
		forkMeta("tu2"))
	for range 5 { // map order must not matter
		s := mustUpdate(t, NewReader(projects, "s1"))
		if want := (Usage{Input: 2, Output: 2100, CacheWrite: 40000}); s.Usage != want {
			t.Errorf("session usage = %+v, want %+v", s.Usage, want)
		}
		if len(s.Agents) != 2 || s.Agents[0].ID != "a1" {
			t.Fatalf("Agents = %+v", s.Agents)
		}
		plan, fork := s.Agents[0], s.Agents[1]
		if want := (Usage{Input: 2, Output: 2000, CacheWrite: 40000}); plan.Usage != want || plan.Parent != "" {
			t.Errorf("a1 usage %+v, Parent %q; want %+v and the session", plan.Usage, plan.Parent, want)
		}
		if fork.Usage != (Usage{Output: 100}) || fork.Parent != "a1" {
			t.Errorf("fork usage %+v, Parent %q; want only its own step, and a1 as its parent", fork.Usage, fork.Parent)
		}
	}
}
