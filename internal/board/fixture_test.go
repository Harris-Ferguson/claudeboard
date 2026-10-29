package board

import (
	"github.com/Harris-Ferguson/claudeboard/internal/claudecli"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
	"github.com/Harris-Ferguson/claudeboard/internal/transcript"
)

// fixture is three workflows and an interactive session started elsewhere, matching
// the mockup in the spec. Totals (cache reads left out): payments-v2 53k, q4-search 12k,
// everything 65k.
func fixture() (store.State, []claudecli.Session, map[string]transcript.Snapshot) {
	st := store.State{
		Version: 1,
		Workflows: []store.Workflow{
			{Slug: "payments-v2", Name: "payments-v2", LastRepo: "api"},
			{Slug: "q4-search", Name: "q4-search", LastRepo: "api"},
			{Slug: "quick", Name: "quick", LastRepo: "scratch"},
		},
		Sessions: []store.Session{
			{SessionID: "id-1", ShortID: "aaaa0001", Name: "cb-payments-v2-1", Workflow: "payments-v2", Repo: "api", Dir: "/work/api"},
			{SessionID: "id-2", ShortID: "aaaa0002", Name: "cb-payments-v2-2", Workflow: "payments-v2", Repo: "web", Dir: "/work/web"},
			{SessionID: "id-3", ShortID: "aaaa0003", Name: "cb-q4-search-1", Workflow: "q4-search", Repo: "api", Dir: "/work/api"},
			{SessionID: "id-4", ShortID: "aaaa0004", Name: "cb-quick-1", Workflow: "quick", Repo: "scratch", Dir: "/home/.claudeboard/scratch"},
		},
	}
	live := []claudecli.Session{
		{PID: 1, ShortID: "aaaa0001", SessionID: "id-1", Name: "cb-payments-v2-1", Kind: claudecli.KindBackground, Status: "busy", State: "working"},
		{PID: 2, ShortID: "aaaa0002", SessionID: "id-2", Name: "cb-payments-v2-2", Kind: claudecli.KindBackground, Status: "waiting", State: "blocked", WaitingFor: "permission prompt"},
		{PID: 3, ShortID: "aaaa0003", SessionID: "id-3", Name: "cb-q4-search-1", Kind: claudecli.KindBackground, Status: "idle", State: "done"},
		{PID: 4, ShortID: "aaaa0004", SessionID: "id-4", Name: "cb-quick-1", Kind: claudecli.KindBackground, Status: "idle", State: "blocked"},
		{PID: 5, SessionID: "id-other", Name: "arbybot-8d", Kind: claudecli.KindInteractive, Status: "busy"},
	}
	snaps := map[string]transcript.Snapshot{
		"id-1": {Found: true, FirstPrompt: "wire the new AfterPay button into checkout", Prompts: 1,
			Usage: transcript.Usage{Input: 1000, Output: 5000, CacheWrite: 35000, CacheRead: 400000}},
		"id-2": {Found: true, FirstPrompt: "hook PaymentForm into checkout", Prompts: 1, Pending: "Bash npm test",
			Usage: transcript.Usage{Input: 1200, Output: 8400, CacheRead: 2100, CacheWrite: 2400},
			Items: []transcript.Item{
				{Kind: transcript.ItemPrompt, Text: "hook PaymentForm into checkout"},
				{Kind: transcript.ItemTool, Tool: "Edit", Text: "CheckoutPage.vue"},
				{Kind: transcript.ItemTool, Tool: "Bash", Text: "npm test"},
				{Kind: transcript.ItemText, Text: "Wired it in. Running the tests to check checkout still works."},
			}},
		"id-3": {Found: true, FirstPrompt: "plan the search rewrite", Prompts: 1,
			Usage: transcript.Usage{Output: 2000, CacheWrite: 10000, CacheRead: 90000}},
		"id-4": {},
	}
	return st, live, snaps
}
