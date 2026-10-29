// Package claudecli runs the claude command and parses what it prints. Everything
// claudeboard assumes about Claude Code's command line lives here; section 4 of the
// design spec lists those assumptions and how they were verified.
package claudecli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MinVersion is the Claude Code version claudeboard was verified against.
const MinVersion = "2.1.312"

// Session kinds reported by `claude agents --json`.
const (
	KindBackground  = "background"
	KindInteractive = "interactive"
)

// Session is one entry of `claude agents --json --all`. Interactive sessions have no
// ShortID or State; stopped ones have no PID or Status.
type Session struct {
	PID        int    `json:"pid"`
	ShortID    string `json:"id"`
	Cwd        string `json:"cwd"`
	Kind       string `json:"kind"`
	SessionID  string `json:"sessionId"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	State      string `json:"state"`
	WaitingFor string `json:"waitingFor"`
}

// ParseAgents parses the output of `claude agents --json --all`.
func ParseAgents(data []byte) ([]Session, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var out []Session
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing claude agents --json: %w", err)
	}
	return out, nil
}

var launchLine = regexp.MustCompile(`backgrounded · ([0-9a-f]+)(?: · (\S+))?`)

// ParseLaunch finds the "backgrounded · <shortId> · <name>" line that `claude --bg`
// prints. The name is missing when Claude started a copy of a running session.
func ParseLaunch(out string) (shortID, name string, err error) {
	m := launchLine.FindStringSubmatch(out)
	if m == nil {
		return "", "", fmt.Errorf("unexpected output from claude --bg: %q", firstLine(out))
	}
	return m[1], m[2], nil
}

// ParseVersion extracts "2.1.312" from "2.1.312 (Claude Code)".
func ParseVersion(out string) string {
	if f := strings.Fields(out); len(f) > 0 {
		return f[0]
	}
	return ""
}

// CompareVersions compares dotted version numbers, returning -1, 0 or 1. Missing or
// non-numeric parts count as 0.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < max(len(pa), len(pb)); i++ {
		x, y := versionPart(pa, i), versionPart(pb, i)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, _ := strconv.Atoi(parts[i])
	return n
}

// IsUntrusted reports whether err is Claude refusing to start in a folder whose trust
// prompt hasn't been accepted yet.
func IsUntrusted(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Workspace not trusted")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
