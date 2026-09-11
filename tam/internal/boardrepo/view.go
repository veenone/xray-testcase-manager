package boardrepo

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agile-suite/tam/internal/backend"
)

// The swimlanes the Boards view offers.
const (
	SwimlaneNone     = "none"
	SwimlaneAssignee = "assignee"
	SwimlaneEpic     = "epic"
)

// The labels the empty value gets in each swimlane, so a lane is never
// drawn with a blank heading.
const (
	laneAll        = "All issues"
	laneUnassigned = "Unassigned"
	laneNoEpic     = "No epic"
)

// MaxCardsPerCell is how many cards one lane's cell renders. A Done column
// of nine hundred issues would otherwise decide how the whole view performs;
// the rest are counted in LaneView.Overflow and still counted in the
// column's totals.
const MaxCardsPerCell = 200

// MaxCardsPerView bounds the whole payload. Twenty assignee lanes across six
// columns would multiply the per-cell cap into twenty-four thousand cards,
// so once this many have been rendered the remaining cards only count and
// BoardView.Capped says so.
const MaxCardsPerView = 2000

// BoardView is what the Boards view draws: the columns in order, the lanes
// the swimlane asked for with a cell per column, and the counts that explain
// what is not drawn.
type BoardView struct {
	BoardID  int    `json:"boardId"`
	SprintID string `json:"sprintId"`
	Swimlane string `json:"swimlane"`

	Columns []ColumnView `json:"columns"`
	Lanes   []LaneView   `json:"lanes"`

	// DonePoints is the story points of the mapped cards whose status is
	// done, summed here where every card is in hand rather than over the
	// cells the view drew. A Done column of nine hundred cards renders two
	// hundred of them, so a summary line that walked the cells would read
	// "200 of 900 points done" against column heads that say otherwise.
	// The definition of done is backend.IsDone, the same one the grid's
	// chip and the Epics tree count by, because a board's rightmost column
	// is as often Blocked or Won't Do as it is Done.
	DonePoints float64 `json:"donePoints"`

	// Unmapped counts the cards whose status no column collects, and
	// UnmappedStatuses names those statuses, deduplicated and sorted, so
	// the view can say which ones rather than print a bare number.
	Unmapped         int      `json:"unmapped"`
	UnmappedStatuses []string `json:"unmappedStatuses"`
	// NotSynced counts the board's keys the issue cache does not hold.
	// Board filters routinely reach outside the profile's project, and a
	// silent absence is how a board quietly lies.
	NotSynced int `json:"notSynced"`
	// Capped is true when MaxCardsPerView stopped the view short.
	Capped bool `json:"capped"`
	// NeedsStatusSync is true when every cached card carries an empty
	// status id, which is the state right after the version 4 migration and
	// before the sync that fills the column. Without it the board would
	// draw nothing and blame the user for it.
	NeedsStatusSync bool `json:"needsStatusSync"`
}

// ColumnView is one column heading with the totals of everything that
// landed in it, capped cards included.
type ColumnView struct {
	Name   string  `json:"name"`
	Total  int     `json:"total"`
	Points float64 `json:"points"`
}

// LaneView is one swimlane: a cell per column, in the same order as
// BoardView.Columns, with the cards the cell renders.
type LaneView struct {
	// ID is the raw grouping value (the assignee, the epic key, empty for
	// the catch-all lane); Label is what the view prints.
	ID    string `json:"id"`
	Label string `json:"label"`
	// Count is every card in the lane, whether rendered or not.
	Count int `json:"count"`
	// Cells holds one card list per column. Overflow is parallel to it and
	// carries how many cards that cell holds beyond what it rendered.
	Cells    [][]backend.Issue `json:"cells"`
	Overflow []int             `json:"overflow"`
}

// Board composes what the Boards view draws: the columns in order, the
// cards bucketed into them, and the lanes the swimlane asked for. Cards
// come from the issue cache, the board's own keys plus the profile's
// drafts; a status no column covers is counted in Unmapped so nothing
// disappears silently.
func (r *Repository) Board(ctx context.Context, issues IssueSource, profileID string, boardID int, sprintID, swimlane string) (BoardView, error) {
	lane, err := normalizeSwimlane(swimlane)
	if err != nil {
		return BoardView{}, err
	}
	view := BoardView{
		BoardID:          boardID,
		SprintID:         sprintID,
		Swimlane:         lane,
		Columns:          []ColumnView{},
		Lanes:            []LaneView{},
		UnmappedStatuses: []string{},
	}
	// Columns and membership come from one snapshot, since ReplaceBoard writes
	// them together and the sync replaces every board on every pass. Read as
	// two statements the view could draw this sync's columns against the last
	// one's cards.
	shape, err := r.Shape(ctx, profileID, boardID, sprintID)
	if err != nil {
		return BoardView{}, err
	}
	cols := shape.Columns
	if len(cols) == 0 {
		// The board's row and its columns are written in one transaction,
		// so no columns is what Jira's configuration answered with, not a
		// board caught half written. There is no shape to draw either way.
		return view, nil
	}
	for _, c := range cols {
		view.Columns = append(view.Columns, ColumnView{Name: c.Name})
	}

	keys := shape.Keys
	cards, err := issues.IssuesByKeys(ctx, profileID, keys)
	if err != nil {
		return BoardView{}, err
	}
	view.NotSynced = len(keys) - len(cards)
	view.NeedsStatusSync = needsStatusSync(cards)

	// The drafts come from the cache rather than from the board's key list,
	// which is Jira's and can never name one. They are project-level, so
	// every board and every sprint of the profile draws the same ones and
	// the Draft chip on the card is what says so.
	drafts, err := issues.DraftIssues(ctx, profileID)
	if err != nil {
		return BoardView{}, err
	}
	all := make([]backend.Issue, 0, len(cards)+len(drafts))
	all = append(all, cards...)
	all = append(all, drafts...)

	byStatus, draftColumn := columnIndex(cols)
	lanes := newLaneSet(lane, len(cols))
	unmapped := map[string]bool{}
	rendered := 0

	for _, card := range all {
		col, ok := placeCard(card, byStatus, draftColumn)
		if !ok {
			view.Unmapped++
			if name := unmappedName(card); name != "" {
				unmapped[name] = true
			}
			continue
		}
		view.Columns[col].Total++
		if card.StoryPoints != nil {
			view.Columns[col].Points += *card.StoryPoints
			if backend.IsDone(card.Status) {
				view.DonePoints += *card.StoryPoints
			}
		}
		l := lanes.get(card, lane)
		l.Count++
		switch {
		case len(l.Cells[col]) >= MaxCardsPerCell:
			l.Overflow[col]++
		case rendered >= MaxCardsPerView:
			view.Capped = true
			l.Overflow[col]++
		default:
			l.Cells[col] = append(l.Cells[col], card)
			rendered++
		}
	}
	view.Lanes = lanes.sorted()
	for name := range unmapped {
		view.UnmappedStatuses = append(view.UnmappedStatuses, name)
	}
	sort.Strings(view.UnmappedStatuses)
	return view, nil
}

// normalizeSwimlane accepts the three the view offers, treating an empty
// value as the default rather than as a typo, and names all three in the
// error so the caller is told what it may send.
func normalizeSwimlane(swimlane string) (string, error) {
	switch s := strings.ToLower(strings.TrimSpace(swimlane)); s {
	case "", SwimlaneNone:
		return SwimlaneNone, nil
	case SwimlaneAssignee, SwimlaneEpic:
		return s, nil
	default:
		return "", fmt.Errorf("boardrepo: unknown swimlane %q, want none, assignee, or epic", swimlane)
	}
}

// columnIndex maps a status id to the first column that collects it, and
// names the first column that collects any status at all. That second one
// is where a draft goes: a board's first column is often a Backlog column
// with no statuses mapped to it, and dropping every draft into a column
// Jira itself never fills would be its own kind of wrong.
func columnIndex(cols []backend.BoardColumn) (byStatus map[string]int, draftColumn int) {
	byStatus = make(map[string]int, len(cols))
	draftColumn = -1
	for i, c := range cols {
		if len(c.StatusIDs) > 0 && draftColumn < 0 {
			draftColumn = i
		}
		for _, id := range c.StatusIDs {
			if _, seen := byStatus[id]; !seen {
				byStatus[id] = i
			}
		}
	}
	return byStatus, draftColumn
}

// placeCard picks the column a card belongs in. A draft is the one card
// Jira has never seen, so no column names its status; it goes in the first
// column that collects anything, and a board with no such column has
// nowhere to put it. The Draft flag is what says so, not an empty status
// id: right after the version 4 migration every cached row carries an empty
// status id, and reading those as drafts would pile a whole backlog into
// the first column. They go through the status id like every other card,
// which no column collects, so they count as unmapped until the next sync
// fills the column in.
func placeCard(card backend.Issue, byStatus map[string]int, draftColumn int) (int, bool) {
	if card.Draft {
		if draftColumn < 0 {
			return 0, false
		}
		return draftColumn, true
	}
	col, ok := byStatus[card.StatusID]
	return col, ok
}

// unmappedName is what UnmappedStatuses lists for a card no column took:
// the status name when there is one, otherwise the raw id, so the message
// still names something the user can look up. A card with neither, which is
// what a row cached before the version 4 migration looks like, names
// nothing and the caller leaves it out of the list.
func unmappedName(card backend.Issue) string {
	if name := strings.TrimSpace(card.Status); name != "" {
		return name
	}
	return card.StatusID
}

// needsStatusSync is true when there are cards and not one of them carries a
// status id.
func needsStatusSync(cards []backend.Issue) bool {
	if len(cards) == 0 {
		return false
	}
	for _, c := range cards {
		if c.StatusID != "" {
			return false
		}
	}
	return true
}

// laneSet collects the lanes as the cards are walked, so a lane exists only
// once a card asks for it.
type laneSet struct {
	columns int
	byID    map[string]*LaneView
	order   []*LaneView
}

func newLaneSet(swimlane string, columns int) *laneSet {
	s := &laneSet{columns: columns, byID: map[string]*LaneView{}}
	if swimlane == SwimlaneNone {
		// One lane, always present, so a board with no cards still draws
		// its columns rather than an empty grid.
		s.add("", laneAll)
	}
	return s
}

func (s *laneSet) add(id, label string) *LaneView {
	l := &LaneView{ID: id, Label: label, Cells: make([][]backend.Issue, s.columns), Overflow: make([]int, s.columns)}
	for i := range l.Cells {
		l.Cells[i] = []backend.Issue{}
	}
	s.byID[id] = l
	s.order = append(s.order, l)
	return l
}

// get returns the lane a card belongs to, creating it on first sight.
func (s *laneSet) get(card backend.Issue, swimlane string) *LaneView {
	id, label := laneOf(card, swimlane)
	if l, ok := s.byID[id]; ok {
		return l
	}
	return s.add(id, label)
}

// laneOf is the grouping value and its label for one card.
func laneOf(card backend.Issue, swimlane string) (id, label string) {
	switch swimlane {
	case SwimlaneAssignee:
		if id = strings.TrimSpace(card.Assignee); id == "" {
			return "", laneUnassigned
		}
		return id, id
	case SwimlaneEpic:
		if id = strings.TrimSpace(card.ParentKey); id == "" {
			return "", laneNoEpic
		}
		return id, id
	default:
		return "", laneAll
	}
}

// sorted returns the lanes by label, with the empty value last: an
// Unassigned or No epic lane is a bucket, not a name, and it reads wrong
// among the names.
func (s *laneSet) sorted() []LaneView {
	out := make([]LaneView, 0, len(s.order))
	sort.SliceStable(s.order, func(i, j int) bool {
		a, b := s.order[i], s.order[j]
		if (a.ID == "") != (b.ID == "") {
			return b.ID == ""
		}
		return a.Label < b.Label
	})
	for _, l := range s.order {
		out = append(out, *l)
	}
	return out
}
