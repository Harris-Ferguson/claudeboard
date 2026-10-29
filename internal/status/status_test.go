package status

import (
	"testing"

	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
)

func bg(state, waitingFor string) *claudecli.Session {
	return &claudecli.Session{Kind: claudecli.KindBackground, State: state, WaitingFor: waitingFor}
}

func TestFrom(t *testing.T) {
	cases := []struct {
		name   string
		live   *claudecli.Session
		prompt bool
		want   Status
	}{
		{"not listed", nil, true, Status{Kind: Stopped}},
		{"working", bg("working", ""), true, Status{Kind: Working}},
		{"waiting with a reason", bg("blocked", "permission prompt"), true, Status{Kind: NeedsYou, Detail: "permission prompt"}},
		{"fresh session", bg("blocked", ""), false, Status{Kind: New}},
		{"blocked after a prompt", bg("blocked", ""), true, Status{Kind: NeedsYou}},
		{"done", bg("done", ""), true, Status{Kind: Done}},
		{"stopped", bg("stopped", ""), true, Status{Kind: Stopped}},
		{"failed", bg("failed", ""), false, Status{Kind: Unknown, Detail: "failed"}},
		{"interactive busy", &claudecli.Session{Kind: claudecli.KindInteractive, Status: "busy"}, true, Status{Kind: Working}},
		{"interactive idle", &claudecli.Session{Kind: claudecli.KindInteractive, Status: "idle"}, true, Status{Kind: Done}},
	}
	for _, c := range cases {
		if got := From(c.live, c.prompt); got != c.want {
			t.Errorf("%s: From = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestIconAndText(t *testing.T) {
	cases := []struct {
		s          Status
		icon, text string
	}{
		{Status{Kind: Working}, "●", "working"},
		{Status{Kind: NeedsYou, Detail: "permission prompt"}, "!", "needs you — permission prompt"},
		{Status{Kind: NeedsYou}, "!", "needs you"},
		{Status{Kind: Done}, "✓", "done"},
		{Status{Kind: New}, "○", "new"},
		{Status{Kind: Stopped}, "○", "stopped"},
		{Status{Kind: Unknown, Detail: "failed"}, "?", "failed"},
		{Status{Kind: Unknown}, "?", "unknown"},
	}
	for _, c := range cases {
		if c.s.Icon() != c.icon || c.s.Text() != c.text {
			t.Errorf("%+v: Icon %q Text %q, want %q %q", c.s, c.s.Icon(), c.s.Text(), c.icon, c.text)
		}
	}
}
