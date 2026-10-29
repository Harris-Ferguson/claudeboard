//go:build realclaude

package claudecli

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestRealClaudeLifecycle starts an idle background session in this repo (no prompt,
// so no tokens are used), checks claude lists it, stops it, and deletes it.
//
//	go test -tags realclaude -v ./internal/claudecli
//
// The repo folder must be trusted: run `claude` in it once first.
func TestRealClaudeLifecycle(t *testing.T) {
	dir, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	c, ctx := New(), context.Background()

	start := time.Now()
	id, err := c.Launch(ctx, LaunchOpts{Dir: dir, Name: "cb-realcheck-1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("launched %s in %s", id, time.Since(start).Round(time.Millisecond))
	defer func() {
		out, err := c.Remove(ctx, id)
		if err != nil {
			t.Errorf("rm: %v", err)
		}
		t.Log(out)
	}()

	find := func() *Session {
		start := time.Now()
		live, err := c.List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("claude agents --json took %s", time.Since(start).Round(time.Millisecond))
		for i := range live {
			if live[i].ShortID == id {
				return &live[i]
			}
		}
		return nil
	}
	var s *Session
	for i := 0; i < 20 && s == nil; i++ {
		if s = find(); s == nil {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if s == nil {
		t.Fatalf("session %s never appeared in claude agents", id)
	}
	if s.Name != "cb-realcheck-1" || s.Kind != KindBackground {
		t.Errorf("listed as %+v", *s)
	}
	if _, err := c.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if s = find(); s != nil && s.State == "stopped" {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Errorf("after stop, listed as %+v", s)
}
