package claudecli

import (
	"slices"
	"strings"
)

// MarkerVars are set by Claude Code in the shells it runs. A claude started with them
// believes it is a child of that session and doesn't save its transcript, so they are
// removed before claudeboard runs claude. Everything else, including company
// CLAUDE_CODE_* and ANTHROPIC_* settings, passes through.
var MarkerVars = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN",
	"CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_EFFORT",
	"CLAUDE_PID",
}

// CleanEnv returns env without MarkerVars.
func CleanEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(MarkerVars, name) {
			out = append(out, kv)
		}
	}
	return out
}
