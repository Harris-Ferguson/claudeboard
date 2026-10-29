package store

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// QuickSlug is the workflow quick one-offs go into.
const QuickSlug = "quick"

const maxSlugLen = 24

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a workflow name into its slug: lowercase, every run of other characters
// replaced by "-", trimmed to 24 characters. It is empty when the name has no letter
// or digit.
func Slug(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	return s
}

// SessionName is the name claudeboard gives the n-th session of a workflow.
func SessionName(slug string, n int) string { return fmt.Sprintf("cb-%s-%d", slug, n) }

// WorktreeName is the git worktree name for the n-th session of a workflow.
func WorktreeName(slug string, n int) string { return fmt.Sprintf("%s-%d", slug, n) }

// ParseSessionName splits "cb-<slug>-<n>" into its slug and number.
func ParseSessionName(name string) (slug string, n int, ok bool) {
	rest, found := strings.CutPrefix(name, "cb-")
	if !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(rest, '-')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 1 {
		return "", 0, false
	}
	return rest[:i], n, true
}
