package board

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// golden compares got with testdata/<name>.golden (rewriting it with -update).
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/board -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden file:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// assertBox checks the screen is exactly h lines of exactly w cells.
func assertBox(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("%d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("line %d is %d cells wide, want %d: %q", i, got, w, ansi.Strip(l))
		}
	}
}

func TestRenderWide(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 2 // cb-payments-v2-2, waiting on a permission prompt
	out := m.render()
	assertBox(t, out, 100, 16)
	plain := ansi.Strip(out)
	for _, want := range []string{
		"claudeboard · 1 working · 1 needs you · 65k tokens",
		"cb-payments-v2-2 · web · 12k tokens",
		"needs you — permission prompt: Bash npm test",
		"you: hook PaymentForm into checkout",
		"Other sessions",
	} {
		if !strings.Contains(plain, want) {
			t.Errorf("screen is missing %q:\n%s", want, plain)
		}
	}
	golden(t, "wide", plain)
}

func TestRenderNarrowHidesPreview(t *testing.T) {
	m, _ := testModel(t, 70, 16)
	out := m.render()
	assertBox(t, out, 70, 16)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "cache r") {
		t.Errorf("preview shown below %d columns:\n%s", narrowWidth, plain)
	}
	if !strings.Contains(plain, "q quit") {
		t.Error("footer help is cut off on a narrow screen")
	}
	golden(t, "narrow", plain)
	m.previewToggled = true
	m.cursor = 2 // a session, whose preview has the token breakdown
	assertBox(t, m.render(), 70, 16)
	if !strings.Contains(ansi.Strip(m.render()), "cache r") {
		t.Error("p should show the preview on a narrow screen")
	}
}

func TestRenderHelp(t *testing.T) {
	m, _ := testModel(t, 100, 20)
	m.mode = modeHelp
	out := m.render()
	assertBox(t, out, 100, 20)
	for _, line := range strings.Split(ansi.Strip(out), "\n") {
		if parts := strings.Split(line, "│"); len(parts) == 4 && strings.Contains(parts[2], "…") {
			t.Errorf("help line cut off: %q", parts[2])
		}
	}
	golden(t, "help", ansi.Strip(out))
}

func TestHeaderShowsStaleStatus(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.deps.Now = func() time.Time { return testNow.Add(12 * time.Second) }
	if !strings.Contains(ansi.Strip(m.render()), "status stale (12s)") {
		t.Error("stale status not shown")
	}
}

func TestEmptyBoard(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.state, m.live = store.State{}, nil
	m.rebuild()
	out := m.render()
	assertBox(t, out, 100, 16)
	if !strings.Contains(ansi.Strip(out), "No sessions yet") {
		t.Errorf("empty board:\n%s", ansi.Strip(out))
	}
}

func TestListScrollsToCursor(t *testing.T) {
	m, _ := testModel(t, 100, 10)
	st := store.State{Version: 1, Workflows: []store.Workflow{{Slug: "big", Name: "big"}}}
	snaps := map[string]transcript.Snapshot{}
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("big-%d", i)
		st.Sessions = append(st.Sessions, store.Session{SessionID: id, ShortID: id, Name: fmt.Sprintf("cb-big-%d", i), Workflow: "big", Repo: "api"})
		snaps[id] = transcript.Snapshot{Found: true, FirstPrompt: fmt.Sprintf("task %d", i), Prompts: 1}
	}
	m.state, m.live, m.snaps = st, nil, snaps
	m.rebuild()
	for i := 0; i < 40; i++ {
		m.move(1)
	}
	out := m.render()
	assertBox(t, out, 100, 10)
	if !strings.Contains(ansi.Strip(out), "task 30") || m.listTop == 0 {
		t.Errorf("cursor row not visible (listTop %d):\n%s", m.listTop, ansi.Strip(out))
	}
}

func TestWideCharactersKeepBoxAligned(t *testing.T) {
	for _, w := range []int{100, 70} {
		m, _ := testModel(t, w, 16)
		snap := m.snaps["id-2"]
		snap.FirstPrompt = "修复结账页面的错误 😀 then \x1b[31mred\x1b[0m\ttabs"
		snap.Items = append(snap.Items, transcript.Item{Kind: transcript.ItemText, Text: "完成了 ✅ 所有测试都通过了，结账页面现在可以正常工作。\ttab"})
		m.snaps["id-2"] = snap
		m.rebuild()
		m.cursor = 2
		assertBox(t, m.render(), w, 16)
	}
}
