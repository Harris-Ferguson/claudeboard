package board

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/status"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

type tickMsg time.Time

// refreshedMsg carries one refresh's results back to Update.
type refreshedMsg struct {
	state    store.State
	live     []claudecli.Session
	listErr  error
	storeErr error
	snaps    map[string]transcript.Snapshot
	readers  map[string]*transcript.Reader
	at       time.Time
}

// applier is a message that knows how to apply itself, so action messages added
// later don't need new cases in Update.
type applier interface {
	apply(m Model) (Model, tea.Cmd)
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init starts the refresh cycle straight away.
func (m Model) Init() tea.Cmd {
	return func() tea.Msg { return tickMsg(time.Now()) }
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ensureVisible()
		return m, nil
	case tickMsg:
		cmd := m.startRefresh()
		return m, tea.Batch(cmd, tick())
	case refreshedMsg:
		cmd := m.applyRefresh(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case applier:
		return msg.apply(m)
	}
	if m.mode == modeNameWorkflow {
		var cmd tea.Cmd
		m.flow.input, cmd = m.flow.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// startRefresh begins a refresh unless one is running. The refresh owns the
// transcript readers until its refreshedMsg hands them back.
func (m *Model) startRefresh() tea.Cmd {
	if m.refreshing {
		return nil
	}
	m.refreshing = true
	d, readers := m.deps, m.readers
	m.readers = nil
	return func() tea.Msg { return doRefresh(d, readers) }
}

// doRefresh asks Claude for its sessions, re-reads the state (adopting cb- sessions
// it doesn't know), and reads new transcript lines. It runs off the UI goroutine.
func doRefresh(d Deps, readers map[string]*transcript.Reader) refreshedMsg {
	if readers == nil {
		readers = map[string]*transcript.Reader{}
	}
	msg := refreshedMsg{readers: readers, at: d.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), staleAfter)
	defer cancel()
	msg.live, msg.listErr = d.Src.List(ctx)

	st, _, err := d.Store.Load()
	if err != nil {
		msg.storeErr = err
		return msg
	}
	if msg.listErr == nil {
		listed := toListed(msg.live)
		if st.NeedsRelink(listed) {
			now := d.Now()
			if err := d.Store.Update(func(s *store.State) error { s.Relink(listed, now); return nil }); err == nil {
				if fresh, _, err := d.Store.Load(); err == nil {
					st = fresh
				}
			}
		}
	}
	msg.state = st

	ids := map[string]bool{}
	for _, s := range st.Sessions {
		if s.SessionID != "" {
			ids[s.SessionID] = true
		}
	}
	for _, l := range msg.live {
		if l.SessionID != "" {
			ids[l.SessionID] = true
		}
	}
	msg.snaps = make(map[string]transcript.Snapshot, len(ids))
	for id := range ids {
		r := readers[id]
		if r == nil {
			r = transcript.NewReader(d.ProjectsDir, id)
			readers[id] = r
		}
		snap, _ := r.Update()
		msg.snaps[id] = snap
	}
	for id := range readers {
		if !ids[id] {
			delete(readers, id)
		}
	}
	return msg
}

// toListed keeps the background sessions; interactive ones are never adopted.
func toListed(live []claudecli.Session) []store.Listed {
	var out []store.Listed
	for _, l := range live {
		if l.Kind == claudecli.KindBackground {
			out = append(out, store.Listed{SessionID: l.SessionID, ShortID: l.ShortID, Name: l.Name, Cwd: l.Cwd})
		}
	}
	return out
}

func (m *Model) applyRefresh(msg refreshedMsg) tea.Cmd {
	m.refreshing = false
	m.readers = msg.readers
	if msg.storeErr != nil {
		m.message = "reading state.json: " + msg.storeErr.Error()
	} else {
		m.state = msg.state
	}
	if msg.snaps != nil {
		m.snaps = msg.snaps
	}
	if msg.listErr != nil {
		m.listErr = msg.listErr
	} else {
		m.live, m.listErr, m.lastOK = msg.live, nil, msg.at
	}
	m.rebuild()
	cmd := m.noteSeen(m.seenAfterAttach)
	m.seenAfterAttach = ""
	return cmd
}

func (m Model) handleKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modeList:
		return m.listKey(k)
	case modeHelp:
		m.mode = modeList
		return m, nil
	}
	return m.flowKey(k)
}

func (m Model) listKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	page := max(1, m.bodyHeight()/2)
	switch k.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "tab":
		m.nextWaiting()
	case "A":
		m.showArchived = !m.showArchived
		m.rebuild()
	case "p":
		m.previewToggled = !m.previewToggled
		if m.previewVisible() && !m.previewFits() {
			m.message = "the screen is too narrow for the preview"
		}
	case "pgup":
		m.previewScroll += page
	case "pgdown":
		m.previewScroll = max(0, m.previewScroll-page)
	case "?":
		m.mode = modeHelp
	case "ctrl+l":
		return m, tea.ClearScreen
	default:
		return m.commandKey(k)
	}
	return m, nil
}

// nextWaiting moves to the next session (after the cursor, wrapping) that needs you.
func (m *Model) nextWaiting() {
	n := len(m.rows)
	for i := 1; i <= n; i++ {
		j := (m.cursor + i) % n
		if r := m.rows[j]; (r.Kind == RowSession || r.Kind == RowOther) && r.Status.Kind == status.NeedsYou {
			m.cursor, m.previewScroll = j, 0
			m.ensureVisible()
			m.noteCursor()
			return
		}
	}
	m.message = "nothing needs you right now"
}
