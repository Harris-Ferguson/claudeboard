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

	showArchived   bool
	previewToggled bool // p flips the default: shown when wide, hidden when narrow
	previewScroll  int  // lines scrolled back from the newest preview line

	refreshing bool
	lastOK     time.Time // last successful `claude agents --json`
	listErr    error
	message    string // one-line note in the footer

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
		deps:      d,
		snaps:     map[string]transcript.Snapshot{},
		readers:   map[string]*transcript.Reader{},
		collapsed: map[string]bool{},
		message:   warning,
	}
}

func (m Model) selected() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Row{}, false
	}
	return m.rows[m.cursor], true
}

// rebuild recomputes the rows, keeping the cursor on the same row when it still
// exists and otherwise at the same position.
func (m *Model) rebuild() {
	key := ""
	if r, ok := m.selected(); ok {
		key = r.Key()
	}
	m.rows, m.counts = BuildRows(m.state, m.live, m.snaps, m.collapsed, m.showArchived)
	found := false
	for i, r := range m.rows {
		if m.selectName != "" && r.Kind == RowSession && r.Session.Name == m.selectName {
			m.cursor, found, m.selectName = i, true, ""
			break
		}
		if r.Key() == key {
			m.cursor, found = i, true
		}
	}
	if !found {
		m.cursor = clamp(m.cursor, 0, len(m.rows)-1)
	}
	m.skipHeader(1)
	m.ensureVisible()
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
