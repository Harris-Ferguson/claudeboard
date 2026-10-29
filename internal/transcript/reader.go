package transcript

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	acc         *accumulator
}

// tail is how far a file has been read, plus an incomplete last line.
type tail struct {
	offset  int64
	partial []byte
}

// NewReader returns a Reader for sessionID. Nothing is read until Update.
func NewReader(projectsDir, sessionID string) *Reader {
	return &Reader{projectsDir: projectsDir, sessionID: sessionID, files: map[string]*tail{}, acc: newAccumulator()}
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
		r.files, r.acc = map[string]*tail{}, newAccumulator()
	}
	s := r.acc.snapshot()
	s.Subagents = len(subs)
	return s, nil
}

func (r *Reader) readAll(subs []string) (shrunk bool, err error) {
	for _, p := range append([]string{r.mainPath}, subs...) {
		shrunk, err := r.read(p, p == r.mainPath)
		if err != nil || shrunk {
			return shrunk, err
		}
	}
	return false, nil
}

// read feeds the complete lines appended to path since the last call to the
// accumulator. It reports whether the file is now shorter than what was already read.
func (r *Reader) read(path string, main bool) (shrunk bool, err error) {
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
			r.acc.add(line, main)
		}
		buf = buf[i+1:]
	}
	t.partial = append([]byte(nil), buf...)
	return false, nil
}
