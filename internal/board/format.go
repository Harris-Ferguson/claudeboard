package board

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// maxLabel is the most of a first prompt shown as a row label.
const maxLabel = 40

// formatTokens renders a token count in at most five characters: 950, 1.2k, 41k,
// 1.24M, 46.2M, 401M, 2.50B.
func formatTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 9950:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 999_500:
		return fmt.Sprintf("%dk", (n+500)/1000)
	case n < 9_995_000:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n < 99_950_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n < 999_500_000:
		return fmt.Sprintf("%dM", (n+500_000)/1_000_000)
	default:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	}
}

// tokensOrBlank is formatTokens, but empty for zero so idle rows stay quiet.
func tokensOrBlank(n int64) string {
	if n == 0 {
		return ""
	}
	return formatTokens(n)
}

// truncate shortens s to at most w terminal cells, ending in "…" when it cuts.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

// pad truncates or space-pads s to exactly w cells.
func pad(s string, w int) string {
	s = truncate(s, w)
	if d := w - ansi.StringWidth(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// fit lays out left and right on one line of exactly width cells, cutting left first.
func fit(left, right string, width int) string {
	if right == "" {
		return pad(left, width)
	}
	rw := ansi.StringWidth(right) + 1
	if width-rw < 1 {
		return pad(left, width)
	}
	return pad(left, width-rw) + " " + right
}

// clean makes transcript text safe to draw: no escape codes, tabs or carriage returns.
func clean(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(s, "\r", "")
	return strings.ReplaceAll(s, "\t", "    ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// repoLabel shows configured repos by name and other folders by their last element.
func repoLabel(repo string) string {
	if filepath.IsAbs(repo) {
		return filepath.Base(repo)
	}
	return repo
}

// sessionLabel is the start of the first prompt, or "new session".
func sessionLabel(s transcript.Snapshot) string {
	if l := firstLine(clean(s.FirstPrompt)); l != "" {
		return truncate(l, maxLabel)
	}
	return "new session"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }
