package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	st, recovered, err := Open(t.TempDir()).Load()
	if err != nil || recovered || st.Version != 1 || len(st.Sessions) != 0 {
		t.Fatalf("Load = %+v, %v, %v", st, recovered, err)
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	s := Open(t.TempDir())
	err := s.Update(func(st *State) error {
		st.UpsertSession(Session{SessionID: "id-1", Name: "cb-w-1", Workflow: "w"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, _, err := s.Load()
	if err != nil || st.Session("id-1") == nil {
		t.Fatalf("after Update, Load = %+v, %v", st, err)
	}
}

func TestUpdateKeepsFileWhenCallbackFails(t *testing.T) {
	home := t.TempDir()
	s := Open(home)
	if err := s.Update(func(*State) error { return errors.New("nope") }); err == nil {
		t.Fatal("Update should return the callback's error")
	}
	if _, err := os.Stat(filepath.Join(home, "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state.json written despite the error: %v", err)
	}
}

func TestLoadSetsCorruptFileAside(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, recovered, err := Open(home).Load()
	if err != nil || !recovered || len(st.Sessions) != 0 {
		t.Fatalf("Load = %+v, %v, %v", st, recovered, err)
	}
	if m, _ := filepath.Glob(filepath.Join(home, "state.json.corrupt-*")); len(m) != 1 {
		t.Errorf("corrupt file not set aside: %v", m)
	}
}

func TestUpdateIsSerialised(t *testing.T) {
	s := Open(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := s.Update(func(st *State) error {
				st.UpsertSession(Session{SessionID: fmt.Sprint("id-", i), Name: fmt.Sprint("cb-w-", i+1)})
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	st, _, _ := s.Load()
	if len(st.Sessions) != 20 {
		t.Errorf("got %d sessions after 20 concurrent updates, want 20", len(st.Sessions))
	}
}
