package store

import "testing"

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Payments V2":    "payments-v2",
		"  q4 search!! ": "q4-search",
		"!!!":            "",
		"a very long workflow name that keeps going": "a-very-long-workflow-nam",
		"abcdefghijklmnopqrstuvw xyz":                "abcdefghijklmnopqrstuvw", // cut lands on "-", which is trimmed
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	if got := SessionName("payments-v2", 3); got != "cb-payments-v2-3" {
		t.Errorf("SessionName = %q", got)
	}
	if got := WorktreeName("payments-v2", 3); got != "payments-v2-3" {
		t.Errorf("WorktreeName = %q", got)
	}
}

func TestParseSessionName(t *testing.T) {
	good := map[string]struct {
		slug string
		n    int
	}{
		"cb-payments-v2-3": {"payments-v2", 3},
		"cb-quick-12":      {"quick", 12},
	}
	for name, want := range good {
		slug, n, ok := ParseSessionName(name)
		if !ok || slug != want.slug || n != want.n {
			t.Errorf("ParseSessionName(%q) = %q, %d, %v", name, slug, n, ok)
		}
	}
	for _, name := range []string{"arbybot-8d", "cb--1", "cb-x-0", "cb-x-y", "cb-x", ""} {
		if _, _, ok := ParseSessionName(name); ok {
			t.Errorf("ParseSessionName(%q) should fail", name)
		}
	}
}
