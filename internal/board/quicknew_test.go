package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

// names returns a name generator that hands out names in order.
func names(ns ...string) func() string {
	return func() string {
		n := ns[0]
		if len(ns) > 1 {
			ns = ns[1:]
		}
		return n
	}
}

func TestQuickNewSessionAsksNothingInAFreeFolder(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.deps.NewName = names("amber-otter")
	l := claudecli.Session{PID: 7, ShortID: "ffff0007", SessionID: "id-7", Name: "tools", Kind: claudecli.KindBackground, State: "done", Cwd: "/work/tools"}
	m.live, src.live = append(m.live, l), append(src.live, l)
	m.rebuild()
	m = cursorTo(t, m, "o:id-7")
	m, cmd := press(t, m, "N")
	if cmd == nil || m.mode != modeList {
		t.Fatalf("N: cmd %v, mode %v; it should start straight away", cmd, m.mode)
	}
	msg := cmd()
	if len(src.launched) != 1 {
		t.Fatalf("launched %+v", src.launched)
	}
	if o := src.launched[0]; o.Name != "cb-amber-otter-1" || o.Dir != "/work/tools" || o.Worktree != "" {
		t.Errorf("launch options = %+v", o)
	}
	m, _ = send(m, msg)
	if len(src.attached) != 1 || src.attached[0] != "new00001" {
		t.Errorf("attached %v, want the new session opened", src.attached)
	}
	if st, _, _ := m.deps.Store.Load(); st.Workflow("amber-otter") == nil {
		t.Error("workflow amber-otter not recorded")
	}
}

func TestQuickNewSessionInABusyFolderAsksAboutAWorktree(t *testing.T) {
	m, src := testModel(t, 100, 16)
	m.deps.NewName = names("payments-v2", "amber-otter") // the first is taken
	m.cursor = 2                                         // cb-payments-v2-2, running in /work/web
	m, _ = press(t, m, "N")
	if m.mode != modeAskWorktree || !strings.Contains(m.footer(100), "web already has a session running") {
		t.Fatalf("mode %v, footer %q", m.mode, m.footer(100))
	}
	_, cmd := press(t, m, "y")
	if cmd == nil {
		t.Fatal("y did nothing")
	}
	cmd()
	if o := src.launched[0]; o.Name != "cb-amber-otter-1" || o.Dir != "/work/web" || o.Worktree != "amber-otter-1" {
		t.Errorf("launch options = %+v", o)
	}
}

func TestQuickNewSessionFolder(t *testing.T) {
	m, _ := testModel(t, 100, 16)
	m.cursor = 3 // the q4-search workflow, last used in api
	if got := m.selectedDir(); got != "/work/api" {
		t.Errorf("workflow row: %q, want its last folder", got)
	}
	m.state, m.live = store.State{}, nil
	m.rebuild()
	if got := m.selectedDir(); got != "/work" {
		t.Errorf("empty board: %q, want the folder claudeboard started in", got)
	}
}

func TestRandomNamesAreSlugs(t *testing.T) {
	for i := 0; i < 200; i++ {
		n := randomName()
		if store.Slug(n) != n || strings.Count(n, "-") != 1 {
			t.Fatalf("randomName() = %q, want adjective-noun", n)
		}
	}
	m, _ := testModel(t, 100, 24)
	m.mode = modeHelp
	if !strings.Contains(ansi.Strip(m.render()), "n / N") {
		t.Error("help doesn't list N")
	}
}
