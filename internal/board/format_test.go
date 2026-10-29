package board

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

func TestFormatTokens(t *testing.T) {
	cases := map[int64]string{0: "0", 950: "950", 1234: "1.2k", 9949: "9.9k", 9950: "10k", 41234: "41k",
		999_499: "999k", 999_500: "1.00M", 1_240_000: "1.24M", 46_230_000: "46.2M", 401_410_000: "401M", 2_500_000_000: "2.50B"}
	for n, want := range cases {
		if got := formatTokens(n); got != want {
			t.Errorf("formatTokens(%d) = %q, want %q", n, got, want)
		}
	}
	if tokensOrBlank(0) != "" || tokensOrBlank(5) != "5" {
		t.Error("tokensOrBlank")
	}
}

func TestFitAndPad(t *testing.T) {
	if got := fit("abc", "12k", 10); got != "abc    12k" {
		t.Errorf("fit short = %q", got)
	}
	if got := fit("abcdefghijkl", "12k", 10); got != "abcde… 12k" {
		t.Errorf("fit long = %q", got)
	}
	if got := fit("abc", "", 5); got != "abc  " {
		t.Errorf("fit no right = %q", got)
	}
	for _, s := range []string{"日本語のテキスト", "😀😀😀😀", "plain text here"} {
		if w := ansi.StringWidth(pad(s, 7)); w != 7 {
			t.Errorf("pad(%q, 7) is %d cells wide", s, w)
		}
	}
}

func TestClean(t *testing.T) {
	if got := clean("a\tb\r\n\x1b[31mred\x1b[0m"); got != "a    b\nred" {
		t.Errorf("clean = %q", got)
	}
}

func TestLabels(t *testing.T) {
	if got := sessionLabel(transcript.Snapshot{FirstPrompt: "  fix the bug\nmore detail"}); got != "fix the bug" {
		t.Errorf("sessionLabel = %q", got)
	}
	if got := sessionLabel(transcript.Snapshot{}); got != "new session" {
		t.Errorf("empty sessionLabel = %q", got)
	}
	if repoLabel("/work/api") != "api" || repoLabel("api") != "api" {
		t.Error("repoLabel")
	}
	if plural(1, "session") != "1 session" || plural(3, "session") != "3 sessions" {
		t.Error("plural")
	}
}
