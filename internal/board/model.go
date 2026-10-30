// Package board is claudeboard's terminal UI: workflows and their sessions on the
// left, a live preview of the selected one on the right.
package board

import (
	"context"
	"os/exec"
	"time"

	"charm.land/bubbles/v2/textinput"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/config"
	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

const (
	refreshEvery = 1500 * time.Millisecond
	staleAfter   = 5 * time.Second
	narrowWidth  = 90 // below this the preview is hidden unless toggled with p
	// lookTime is how long the cursor rests on a session, with the preview showing,
	// before the session counts as seen.
	lookTime = time.Second
)

// Source is everything the board needs from Claude Code. claudecli.CLI implements it;
// tests use a stub.
type Source interface {
	List(ctx context.Context) ([]claudecli.Session, error)
	Launch(ctx context.Context, o claudecli.LaunchOpts) (string, error)
	Stop(ctx context.Context, shortID string) (string, error)
	Remove(ctx context.Context, shortID string) (string, error)
	Resume(ctx context.Context, sessionID, dir string) (string, error)
	AttachCmd(shortID string) *exec.Cmd
	TrustCmd(dir string) *exec.Cmd
}

var _ Source = claudecli.CLI{}

// Deps are the board's connections to the outside world.
type Deps struct {
	Src         Source
	Store       *store.Store
	Cfg         config.Config
	ProjectsDir string // where Claude Code keeps transcripts
	Cwd         string // the folder claudeboard was started in
	Now         func() time.Time
	NewName     func() string // a random workflow name for N; randomName when nil
}

type mode int

const (
	modeList mode = iota
	modeHelp
	modePickWorkflow
	modeNameWorkflow
	modePickRepo
	modeAskWorktree
	modeAskTrust
	modeConfirmDelete
)

type choice struct {
	label string
	value string
}

// newFlow holds the answers so far while starting a session.
type newFlow struct {
	choices  []choice
	cursor   int
	input    textinput.Model
	req      launch.Request
	trusting bool // the user went to accept the trust prompt; don't ask again
	move     *Row // the Other session being moved into a workflow; nil when starting one
}

// Model is the board's whole state.
type Model struct {
	deps Deps

	width, height int

	state   store.State
	live    []claudecli.Session
	snaps   map[string]transcript.Snapshot
	readers map[string]*transcript.Reader // nil while a refresh owns them

	rows      []Row
	counts    Counts
	cursor    int
	listTop   int // first row shown in the left pane
	collapsed map[string]bool
	// openAgents is the sessions (by row Key) whose finished subagents are shown.
	openAgents map[string]bool

	showArchived   bool
	previewToggled bool // p flips the default: shown when wide, hidden when narrow
	previewScroll  int  // lines scrolled back from the newest preview line

	refreshing bool
	lastOK     time.Time // last successful `claude agents --json`
	listErr    error
	message    string // one-line note in the footer

	// seen is what this board has marked seen (by session ID). It's laid over the
	// state's own record, so a refresh that read state.json before the save landed
	// doesn't make a session unread again.
	seen map[string]int
	// seenAfterAttach is the short ID of the session the user just came back from; the
	// next refresh marks it seen.
	seenAfterAttach string
	// selKey is the row the cursor is on, and selSince when it got there.
	selKey   string
	selSince time.Time

	mode       mode
	flow       newFlow
	pending    Row    // the row a y/N question is about
	selectName string // a session to move the cursor to once it appears in the rows
}

// New returns a board. warning, if not empty, is shown in the footer until the first
// key press.
func New(d Deps, warning string) Model {
	if d.Now == nil {
		d.Now = time.Now
	}
	return Model{
		deps:       d,
		snaps:      map[string]transcript.Snapshot{},
		readers:    map[string]*transcript.Reader{},
		collapsed:  map[string]bool{},
		openAgents: map[string]bool{},
		message:    warning,
	}
}

func (m Model) selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// rebuild recomputes the rows, keeping the cursor on the same row when it still
// exists (see cursorKeys) and otherwise at the same position.
func (m *Model) rebuild() {
	var keys []string
	if r, ok := m.selected(); ok {
		keys = cursorKeys(r)
	}
	for id, n := range m.seen {
		if cur, ok := m.state.Seen[id]; !ok || cur < n {
			m.state.MarkSeen(id, n)
		}
	}
	m.rows, m.counts = BuildRows(m.state, m.live, m.snaps, m.collapsed, m.openAgents, m.showArchived)
	m.cursor = m.findCursor(keys)
	m.skipHeader(1)
	m.ensureVisible()
	m.noteCursor()
}

// cursorKeys are the rows the cursor goes to after a rebuild, best first: the selected
// row itself, then for a subagent row, its session's finished line and its session.
// Subagent rows move and fold away as subagents finish or their session stops, so
// keeping the position there would put the cursor on an unrelated row, such as
// another session that enter would open and x would stop.
func cursorKeys(r Row) []string {
	keys := []string{r.Key()}
	if r.Kind == RowAgent || r.Kind == RowAgentsFolded {
		keys = append(keys, "f:"+r.Owner.Key(), r.Owner.Key())
	}
	return keys
}

// findCursor is the new rows' index for the cursor: the session waiting to be selected
// (selectName) once it appears, else the first of keys that is still there, else the
// same position.
func (m *Model) findCursor(keys []string) int {
	if m.selectName != "" {
		for i, r := range m.rows {
			if r.Kind == RowSession && r.Session.Name == m.selectName {
				m.selectName = ""
				return i
			}
		}
	}
	for _, k := range keys {
		for i, r := range m.rows {
			if r.Key() == k {
				return i
			}
		}
	}
	return clamp(m.cursor, 0, len(m.rows)-1)
}

// skipHeader moves the cursor off the "Other sessions" heading, in direction dir if
// possible.
func (m *Model) skipHeader(dir int) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].Kind != RowOtherHeader {
		return
	}
	if next := m.cursor + dir; next >= 0 && next < len(m.rows) {
		m.cursor = next
	} else {
		m.cursor -= dir
	}
}

func (m *Model) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = clamp(m.cursor+d, 0, len(m.rows)-1)
	m.skipHeader(d)
	m.previewScroll = 0
	m.ensureVisible()
	m.noteCursor()
}

// ensureVisible scrolls the left pane so the cursor's row is on screen.
func (m *Model) ensureVisible() {
	h := m.bodyHeight()
	if m.cursor < m.listTop {
		m.listTop = m.cursor
	}
	if m.cursor >= m.listTop+h {
		m.listTop = m.cursor - h + 1
	}
	m.listTop = clamp(m.listTop, 0, max(0, len(m.rows)-h))
}

// bodyHeight is the number of lines between the header and the footer.
func (m Model) bodyHeight() int { return max(1, m.height-4) }

func (m Model) previewVisible() bool { return (m.width >= narrowWidth) != m.previewToggled }
