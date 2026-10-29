package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Store reads and writes <home>/state.json.
type Store struct {
	dir string
}

// Open returns the store kept in home. Nothing is read until Load or Update.
func Open(home string) *Store { return &Store{dir: home} }

func (s *Store) path() string { return filepath.Join(s.dir, "state.json") }

// Load reads the state without locking; writers replace the file atomically, so a
// reader always sees a complete version. A missing file is an empty state. A file that
// can't be parsed is renamed to state.json.corrupt-<unix-time>, and an empty state is
// returned with recovered set.
func (s *Store) Load() (st State, recovered bool, err error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: 1}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		bad := fmt.Sprintf("%s.corrupt-%d", s.path(), time.Now().Unix())
		if rerr := os.Rename(s.path(), bad); rerr != nil {
			return State{}, false, rerr
		}
		return State{Version: 1}, true, nil
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return st, false, nil
}

// Update loads the state, lets fn change it, and saves the result, all while holding
// an exclusive lock on <home>/state.lock so concurrent writers (the board and
// `claudeboard new`) take turns. If fn returns an error nothing is saved.
func (s *Store) Update(fn func(*State) error) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, "state.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	st, _, err := s.Load()
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return s.save(st)
}

// save writes st to a temporary file and renames it over state.json.
func (s *Store) save(st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "state-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}
