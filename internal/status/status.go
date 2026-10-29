// Package status turns what `claude agents --json` reports about a session into what
// the board shows (section 4 of the design spec, "Status mapping").
package status

import "github.com/Harris-Ferguson/claudeboard/internal/claudecli"

// Kind is a session's state as the board shows it.
type Kind int

const (
	Unknown Kind = iota
	New
	Working
	NeedsYou
	Done
	Stopped
)

// Status is a Kind plus detail: what a session is waiting for, or the raw state Claude
// reported when it isn't one claudeboard knows.
type Status struct {
	Kind   Kind
	Detail string
}

// From maps a session's listing (nil when Claude doesn't list it) and whether a prompt
// has been sent to it to a Status.
func From(live *claudecli.Session, promptSent bool) Status {
	if live == nil {
		return Status{Kind: Stopped}
	}
	if live.Kind == claudecli.KindInteractive && live.State == "" {
		if live.Status == "busy" {
			return Status{Kind: Working}
		}
		return Status{Kind: Done}
	}
	switch live.State {
	case "working":
		return Status{Kind: Working}
	case "done":
		return Status{Kind: Done}
	case "stopped":
		return Status{Kind: Stopped}
	case "blocked":
		switch {
		case live.WaitingFor != "":
			return Status{Kind: NeedsYou, Detail: live.WaitingFor}
		case !promptSent:
			return Status{Kind: New}
		default:
			return Status{Kind: NeedsYou}
		}
	}
	return Status{Kind: Unknown, Detail: live.State}
}

// Icon is the one-character marker shown before a session.
func (s Status) Icon() string {
	switch s.Kind {
	case Working:
		return "●"
	case NeedsYou:
		return "!"
	case Done:
		return "✓"
	case New, Stopped:
		return "○"
	}
	return "?"
}

// Text describes the status in a few words.
func (s Status) Text() string {
	switch s.Kind {
	case Working:
		return "working"
	case NeedsYou:
		if s.Detail != "" {
			return "needs you — " + s.Detail
		}
		return "needs you"
	case Done:
		return "done"
	case New:
		return "new"
	case Stopped:
		return "stopped"
	}
	if s.Detail != "" {
		return s.Detail
	}
	return "unknown"
}
