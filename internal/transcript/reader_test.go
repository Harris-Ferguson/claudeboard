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
	if len(s.Agents) != 1 || s.Usage.Output != 110 || len(s.Items) != 2 {
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
