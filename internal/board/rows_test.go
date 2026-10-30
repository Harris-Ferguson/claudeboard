package board

import (
	"testing"

	"github.com/Harris-Ferguson/claudeboard/internal/status"
)

func kinds(rows []Row) []RowKind {
	out := make([]RowKind, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func TestBuildRows(t *testing.T) {
	st, live, snaps := fixture()
	rows, counts := BuildRows(st, live, snaps, map[string]bool{}, nil, false)
	want := []RowKind{RowWorkflow, RowSession, RowSession, RowWorkflow, RowSession, RowWorkflow, RowSession, RowOtherHeader, RowOther}
	if got := kinds(rows); len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i, k := range want {
		if rows[i].Kind != k {
			t.Fatalf("row %d is %v, want %v", i, rows[i].Kind, k)
		}
	}
	wantStatus := map[int]status.Status{
		1: {Kind: status.Working},
		2: {Kind: status.NeedsYou, Detail: "permission prompt"},
		4: {Kind: status.Done},
		6: {Kind: status.New},
		8: {Kind: status.Working},
	}
	for i, s := range wantStatus {
		if rows[i].Status != s {
			t.Errorf("row %d status = %+v, want %+v", i, rows[i].Status, s)
		}
	}
	if counts != (Counts{Working: 1, NeedsYou: 1, Tokens: 65000}) {
		t.Errorf("counts = %+v (the Other session must not count)", counts)
	}
	if r := rows[0]; r.Tokens != 53000 || r.Sessions != 2 || r.Stats.NeedsYou != 1 {
		t.Errorf("payments-v2 row = %+v", r)
	}
	if rows[8].Name() != "arbybot-8d" || !rows[8].Interactive() || rows[2].ShortID() != "aaaa0002" {
		t.Error("row helpers")
	}
}

func TestBuildRowsCollapsedAndArchived(t *testing.T) {
	st, live, snaps := fixture()
	rows, counts := BuildRows(st, live, snaps, map[string]bool{"payments-v2": true}, nil, false)
	if len(rows) != 7 || !rows[0].Collapsed || counts.NeedsYou != 1 {
		t.Errorf("collapsed: %d rows, counts %+v", len(rows), counts)
	}
	st.Workflows[1].Archived = true
	rows, counts = BuildRows(st, live, snaps, map[string]bool{}, nil, false)
	for _, r := range rows {
		if r.Workflow.Slug == "q4-search" || (r.Kind == RowOther && r.Live.SessionID == "id-3") {
			t.Errorf("archived workflow still shown: %+v", r)
		}
	}
	if counts.Tokens != 53000 {
		t.Errorf("archived tokens still counted: %+v", counts)
	}
	rows, _ = BuildRows(st, live, snaps, map[string]bool{}, nil, true)
	if len(rows) != 9 {
		t.Errorf("showArchived: %d rows, want 9", len(rows))
	}
}

func TestBuildRowsUnlistedAndUnresolved(t *testing.T) {
	st, live, snaps := fixture()
	rows, _ := BuildRows(st, live[1:], snaps, map[string]bool{}, nil, false) // id-1 not listed
	if rows[1].Status.Kind != status.Stopped || rows[1].Live != nil {
		t.Errorf("unlisted session = %+v", rows[1])
	}
	st.Sessions[0].SessionID = "" // launched, ID not resolved yet: match by short ID
	rows, _ = BuildRows(st, live, snaps, map[string]bool{}, nil, false)
	if rows[1].Live == nil || rows[1].Status.Kind != status.Working || len(rows) != 9 {
		t.Errorf("short-ID match failed: %+v (%d rows)", rows[1], len(rows))
	}
}
