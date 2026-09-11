package boardrepo_test

import (
	"context"
	"testing"

	"agile-suite/tam/internal/backend"
)

func oneColumn() []backend.BoardColumn {
	return []backend.BoardColumn{{Name: "To Do", StatusIDs: []string{"1"}}}
}

func twoColumns() []backend.BoardColumn {
	return []backend.BoardColumn{
		{Name: "To Do", StatusIDs: []string{"1"}},
		{Name: "Done", StatusIDs: []string{"5"}},
	}
}

// TestReplaceBoardLandsColumnsAndMembershipTogether writes two boards while a
// reader shaped like the Boards view runs beside it, over and over. One column
// per card is the invariant the seed and the replacement both hold, so a reader
// that ever sees a different count has caught a board with its columns replaced
// and its membership still old.
//
// The reader goes through Shape, which is what the view uses, because the
// guarantee takes both halves: ReplaceBoard writes the two in one transaction,
// and Shape reads them in one snapshot. Two separate reads would tear against a
// perfectly atomic write, which is what this test caught.
func TestReplaceBoardLandsColumnsAndMembershipTogether(t *testing.T) {
	r, db := newRepo(t)
	ctx := context.Background()

	before := map[string][]string{"": {"PLAT-1"}}
	for _, b := range sampleBoards() {
		if err := r.ReplaceBoard(ctx, "p1", b, oneColumn(), nil, before); err != nil {
			t.Fatalf("seed board %d: %v", b.ID, err)
		}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	torn := make(chan [2]int, 1)
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			shape, err := r.Shape(ctx, "p1", 1, "")
			if err != nil {
				continue
			}
			if len(shape.Columns) != len(shape.Keys) {
				select {
				case torn <- [2]int{len(shape.Columns), len(shape.Keys)}:
				default:
				}
				return
			}
		}
	}()

	after := map[string][]string{"": {"PLAT-1", "PLAT-2"}}
	for _, b := range sampleBoards() {
		if err := r.ReplaceBoard(ctx, "p1", b, twoColumns(), nil, after); err != nil {
			t.Fatalf("replace board %d: %v", b.ID, err)
		}
	}
	close(stop)
	<-done
	select {
	case got := <-torn:
		t.Fatalf("a read saw %d columns against %d cards, want the two to change together", got[0], got[1])
	default:
	}

	for _, id := range []int{1, 2} {
		cols, err := r.Columns(ctx, "p1", id)
		if err != nil || len(cols) != 2 {
			t.Fatalf("board %d columns = %+v, %v, want both new columns", id, cols, err)
		}
		if got := boardKeys(t, db, "p1", id, ""); len(got) != 2 || got[1] != "PLAT-2" {
			t.Errorf("board %d membership = %v, want the new pair", id, got)
		}
	}
}

// TestReplaceBoardForgetsTheMembershipOfASprintItNoLongerCarries covers the
// scope the four Upsert calls could never reach: a sprint that stopped
// being active, or was deleted in Jira, is no longer among the scopes the
// sync writes, so only clearing the whole board first takes its cards away.
func TestReplaceBoardForgetsTheMembershipOfASprintItNoLongerCarries(t *testing.T) {
	r, db := newRepo(t)
	ctx := context.Background()
	board := backend.Board{ID: 1, Name: "PLAT Scrum", Type: backend.BoardTypeScrum}

	sprints := []backend.Sprint{
		{ID: 12, BoardID: 1, Name: "Sprint 12", State: "active"},
		{ID: 13, BoardID: 1, Name: "Sprint 13", State: "future"},
	}
	keys := map[string][]string{
		"":   {"PLAT-1", "PLAT-2"},
		"12": {"PLAT-1"},
		"13": {"PLAT-2"},
	}
	if err := r.ReplaceBoard(ctx, "p1", board, oneColumn(), sprints, keys); err != nil {
		t.Fatalf("first replace: %v", err)
	}

	// Sprint 12 closed and sprint 13 was deleted: this run reads neither.
	next := []backend.Sprint{{ID: 12, BoardID: 1, Name: "Sprint 12", State: "closed"}}
	if err := r.ReplaceBoard(ctx, "p1", board, oneColumn(), next, map[string][]string{"": {"PLAT-1"}}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	if got := boardKeys(t, db, "p1", 1, "13"); len(got) != 0 {
		t.Errorf("sprint 13 membership = %v, want it gone with the sprint", got)
	}
	if got := boardKeys(t, db, "p1", 1, "12"); len(got) != 0 {
		t.Errorf("sprint 12 membership = %v, want it gone now that the sprint is closed", got)
	}
	if got := boardKeys(t, db, "p1", 1, ""); len(got) != 1 || got[0] != "PLAT-1" {
		t.Errorf("board membership = %v, want this run's list", got)
	}
	sp, err := r.ListSprints(ctx, "p1", 1)
	if err != nil || len(sp) != 1 || sp[0].ID != 12 {
		t.Fatalf("sprints = %+v, %v, want only sprint 12 left", sp, err)
	}
}

// TestReplaceBoardKeepsEachBoardsCopyOfASharedSprint covers the ordinary
// configuration that used to take the whole boards pass down: two scrum
// boards over one project filter, so Jira hands both of them sprint 12.
// Sprints are cleared and written a board at a time, so the second board's
// insert collided with the first while the key was (profile_id, id).
func TestReplaceBoardKeepsEachBoardsCopyOfASharedSprint(t *testing.T) {
	r, _ := newRepo(t)
	ctx := context.Background()
	shared := backend.Sprint{ID: 12, Name: "Sprint 12", State: "active", StartDate: "2026-08-18T09:00:00Z"}

	for _, b := range sampleBoards() {
		sprints := []backend.Sprint{{ID: shared.ID, BoardID: b.ID, Name: shared.Name, State: shared.State, StartDate: shared.StartDate}}
		if err := r.ReplaceBoard(ctx, "p1", b, oneColumn(), sprints, nil); err != nil {
			t.Fatalf("replace board %d: %v", b.ID, err)
		}
	}

	for _, b := range sampleBoards() {
		got, err := r.ListSprints(ctx, "p1", b.ID)
		if err != nil {
			t.Fatalf("board %d sprints: %v", b.ID, err)
		}
		if len(got) != 1 || got[0].ID != 12 || got[0].BoardID != b.ID {
			t.Errorf("board %d sprints = %+v, want its own copy of sprint 12", b.ID, got)
		}
	}
}

// TestReplaceBoardWritesADuplicatedKeyOnce covers the other duplicate the
// key made fatal: a startAt walk over a board whose ranks move mid-walk
// hands the same issue back on two pages, and one collision used to abort
// the board's whole write.
func TestReplaceBoardWritesADuplicatedKeyOnce(t *testing.T) {
	r, db := newRepo(t)
	ctx := context.Background()
	board := backend.Board{ID: 1, Name: "PLAT Scrum", Type: backend.BoardTypeScrum}
	keys := map[string][]string{"": {"PLAT-1", "PLAT-2", "PLAT-1", "PLAT-3"}}

	if err := r.ReplaceBoard(ctx, "p1", board, oneColumn(), nil, keys); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got := boardKeys(t, db, "p1", 1, "")
	want := []string{"PLAT-1", "PLAT-2", "PLAT-3"}
	if len(got) != len(want) {
		t.Fatalf("membership = %v, want the repeat written once", got)
	}
	for i, key := range want {
		if got[i] != key {
			t.Errorf("membership = %v, want %v in the order the pages arrived", got, want)
		}
	}
}
