package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectsDir is where Claude Code keeps conversations: $CLAUDE_CONFIG_DIR/projects,
// or ~/.claude/projects.
func ProjectsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "projects")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// Reader follows one session's conversation files, reading only what was appended
// since the last Update.
type Reader struct {
	projectsDir string
	sessionID   string
	mainPath    string
	files       map[string]*tail
	acc         *accumulator          // the session's own conversation
	agents      map[string]*agentFile // its subagents', by file path
}

// agentFile is a subagent's conversation plus what Claude Code records about it in
// agent-<id>.meta.json.
type agentFile struct {
	acc     *accumulator
	meta    agentMeta
	metaMod time.Time // the meta file's modification time when last read
}

type agentMeta struct {
	AgentType    string `json:"agentType"`
	Description  string `json:"description"`
	ToolUseID    string `json:"toolUseId"`    // the call that started it
	RequestShape string `json:"requestShape"` // "background" or "foreground"
	// Finished is set once it ends, but not by every Claude Code version, so it only
	// counts when there is no other sign.
	Finished bool `json:"finished"`
}

// tail is how far a file has been read, plus an incomplete last line.
type tail struct {
	offset  int64
	partial []byte
}

// NewReader returns a Reader for sessionID. Nothing is read until Update.
func NewReader(projectsDir, sessionID string) *Reader {
	r := &Reader{projectsDir: projectsDir, sessionID: sessionID}
	r.reset()
	return r
}

func (r *Reader) reset() {
	r.files, r.acc, r.agents = map[string]*tail{}, newAccumulator(false), map[string]*agentFile{}
}

// Update reads new lines from the main file and any subagent files and returns the
// result. A session whose file doesn't exist yet gives a Snapshot with Found false.
func (r *Reader) Update() (Snapshot, error) {
	if r.mainPath == "" {
		matches, _ := filepath.Glob(filepath.Join(r.projectsDir, "*", r.sessionID+".jsonl"))
		if len(matches) == 0 {
			return Snapshot{}, nil
		}
		r.mainPath = matches[0]
	}
	subs, _ := filepath.Glob(filepath.Join(strings.TrimSuffix(r.mainPath, ".jsonl"), "subagents", "*.jsonl"))
	for attempt := 0; attempt < 2; attempt++ {
		shrunk, err := r.readAll(subs)
		if err != nil {
			return Snapshot{}, err
		}
		if !shrunk {
			break
		}
		// A file got shorter (rewritten or truncated): start over from scratch.
		r.reset()
	}
	s := r.acc.snapshot()
	s.Agents = r.agentList()
	for _, a := range s.Agents {
		s.Usage = s.Usage.plus(a.Usage)
	}
	return s, nil
}

func (r *Reader) readAll(subs []string) (shrunk bool, err error) {
	if shrunk, err := r.read(r.mainPath, r.acc); err != nil || shrunk {
		return shrunk, err
	}
	for _, p := range subs {
		f := r.agents[p]
		if f == nil {
			f = &agentFile{acc: newAccumulator(true)}
			r.agents[p] = f
		}
		f.readMeta(strings.TrimSuffix(p, ".jsonl") + ".meta.json")
		if shrunk, err := r.read(p, f.acc); err != nil || shrunk {
			return shrunk, err
		}
	}
	return false, nil
}

// readMeta reads the subagent's meta file if it changed since the last read. A missing
// or unreadable one leaves what was read before.
func (f *agentFile) readMeta(path string) {
	info, err := os.Stat(path)
	if err != nil || info.ModTime().Equal(f.metaMod) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m agentMeta
	if json.Unmarshal(data, &m) == nil {
		f.meta, f.metaMod = m, info.ModTime()
	}
}

// agentList builds the session's subagents, oldest first. A subagent's parent is the
// subagent whose file holds the call that started it.
func (r *Reader) agentList() []Agent {
	caller := map[string]string{} // call ID → the ID of the subagent that made it
	ids := map[*agentFile]string{}
	for p, f := range r.agents {
		id := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "agent-"), ".jsonl")
		ids[f] = id
		for call := range f.acc.agentCalls {
			caller[call] = id
		}
	}
	accs := []*accumulator{r.acc}
	for f := range ids {
		accs = append(accs, f.acc)
	}
	type started struct {
		a     Agent
		first time.Time
	}
	list := make([]started, 0, len(ids))
	for f, id := range ids {
		snap := f.acc.snapshot()
		a := Agent{ID: id, Parent: caller[f.meta.ToolUseID], Type: f.meta.AgentType, Description: f.meta.Description,
			Items: snap.Items, Pending: snap.Pending, Usage: snap.Usage, Last: f.acc.last}
		var end time.Time
		for _, acc := range accs {
			if t := acc.notified[id]; t.After(end) {
				end = t
			}
			if t := acc.results[f.meta.ToolUseID]; f.meta.RequestShape != "background" && t.After(end) {
				// A background subagent's call gets its result as soon as it starts.
				end = t
			}
		}
		a.Ended = (!end.IsZero() && !end.Before(a.Last)) || (end.IsZero() && f.meta.Finished)
		list = append(list, started{a, f.acc.first})
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].first.Equal(list[j].first) {
			return list[i].first.Before(list[j].first)
		}
		return list[i].a.ID < list[j].a.ID
	})
	out := make([]Agent, len(list))
	for i, s := range list {
		out[i] = s.a
	}
	return out
}

// read feeds the complete lines appended to path since the last call to acc. It
// reports whether the file is now shorter than what was already read.
func (r *Reader) read(path string, acc *accumulator) (shrunk bool, err error) {
	t := r.files[path]
	if t == nil {
		t = &tail{}
		r.files[path] = t
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false, err
	}
	switch {
	case info.Size() < t.offset:
		return true, nil
	case info.Size() == t.offset:
		return false, nil
	}
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return false, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return false, err
	}
	t.offset += int64(len(data))
	buf := append(t.partial, data...)
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		if line := bytes.TrimSpace(buf[:i]); len(line) > 0 {
			acc.add(line)
		}
		buf = buf[i+1:]
	}
	t.partial = append([]byte(nil), buf...)
	return false, nil
}
