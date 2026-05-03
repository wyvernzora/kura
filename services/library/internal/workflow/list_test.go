package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wyvernzora/kura/internal/coord"
	"github.com/wyvernzora/kura/internal/domain/refs"
	"github.com/wyvernzora/kura/internal/progress"
	"github.com/wyvernzora/kura/internal/response"
	"github.com/wyvernzora/kura/internal/storage/indexfile"
	"github.com/wyvernzora/kura/internal/workflow"
)

func mustParseSeries(t *testing.T, name string) refs.Series {
	t.Helper()
	ref, err := refs.ParseSeries(name)
	if err != nil {
		t.Fatalf("ParseSeries(%q): %v", name, err)
	}
	return ref
}

// listFixture builds a workflow.Deps backed by an in-memory Index
// pre-populated with rows for each (title, status) pair. Order of
// upserts doesn't matter; the index sorts by lower-cased title.
func listFixture(t *testing.T, rows ...indexfile.Row) workflow.Deps {
	t.Helper()
	idx := indexfile.New(t.TempDir())
	for _, row := range rows {
		if err := idx.Upsert(row); err != nil {
			t.Fatalf("Upsert(%s): %v", row.Series, err)
		}
	}
	return workflow.Deps{
		LibRoot:     t.TempDir(),
		Index:       idx,
		Coordinator: coord.NewMCPCoordinator(),
		Now:         time.Now,
	}
}

func makeRow(t *testing.T, title string, status response.ListStatus) indexfile.Row {
	t.Helper()
	ref := mustParseSeries(t, title)
	return indexfile.Row{
		Series: ref,
		Title:  title,
		Status: status,
	}
}

func TestList_ReturnsAllRowsSortedByTitle(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "Zebra", response.ListStatusComplete),
		makeRow(t, "Apple", response.ListStatusComplete),
		makeRow(t, "Mango", response.ListStatusAiring),
	)
	result, err := workflow.List(context.Background(), deps, workflow.ListInput{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"Apple", "Mango", "Zebra"}
	if len(result.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(result.Rows))
	}
	for i, r := range result.Rows {
		if r.Title != want[i] {
			t.Fatalf("row[%d] title = %q, want %q", i, r.Title, want[i])
		}
	}
	if result.NextCursor != "" {
		t.Fatalf("NextCursor = %q, want empty (no MaxResults)", result.NextCursor)
	}
}

func TestList_EmitsProgressOnInboundCtx(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "Apple", response.ListStatusComplete),
		makeRow(t, "Mango", response.ListStatusAiring),
	)
	var events []progress.Event
	ctx := progress.With(context.Background(), func(_ context.Context, ev progress.Event) {
		events = append(events, ev)
	})
	if _, err := workflow.List(ctx, deps, workflow.ListInput{}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d (%+v), want 2", len(events), events)
	}
	if events[0].Status != progress.StartStatus || events[0].Stage != "list" {
		t.Errorf("events[0] = %+v, want start/list", events[0])
	}
	if events[1].Status != progress.SuccessStatus || events[1].Stage != "list" {
		t.Errorf("events[1] = %+v, want success/list", events[1])
	}
}

func TestList_FilterByStatus(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "A", response.ListStatusComplete),
		makeRow(t, "B", response.ListStatusUntracked),
		makeRow(t, "C", response.ListStatusComplete),
	)
	result, err := workflow.List(context.Background(), deps, workflow.ListInput{
		Statuses: []response.ListStatus{response.ListStatusUntracked},
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Title != "B" {
		t.Fatalf("rows = %+v, want only B", result.Rows)
	}
}

func TestList_PaginationStableNoChange(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "A", response.ListStatusComplete),
		makeRow(t, "B", response.ListStatusComplete),
		makeRow(t, "C", response.ListStatusComplete),
		makeRow(t, "D", response.ListStatusComplete),
	)

	page1, err := workflow.List(context.Background(), deps, workflow.ListInput{MaxResults: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Rows) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %+v", page1)
	}
	if page1.DataChanged {
		t.Fatal("page1 DataChanged = true, want false on first page")
	}

	page2, err := workflow.List(context.Background(), deps, workflow.ListInput{
		MaxResults: 2,
		Cursor:     page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Rows) != 2 {
		t.Fatalf("page2 rows = %d, want 2", len(page2.Rows))
	}
	if page2.Rows[0].Title != "C" || page2.Rows[1].Title != "D" {
		t.Fatalf("page2 = %+v, want [C D]", page2.Rows)
	}
	if page2.DataChanged {
		t.Fatal("page2 DataChanged = true, want false (no mutation between pages)")
	}
	if page2.NextCursor != "" {
		t.Fatalf("page2 NextCursor = %q, want empty (last page)", page2.NextCursor)
	}
}

func TestList_PaginationDataChangedWhenIndexMutates(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "A", response.ListStatusComplete),
		makeRow(t, "B", response.ListStatusComplete),
		makeRow(t, "C", response.ListStatusComplete),
	)

	page1, err := workflow.List(context.Background(), deps, workflow.ListInput{MaxResults: 1})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Rows) != 1 || page1.Rows[0].Title != "A" {
		t.Fatalf("page1 = %+v, want only A", page1.Rows)
	}

	// Mutate the index between pages.
	if err := deps.Index.Upsert(makeRow(t, "BB", response.ListStatusComplete)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	page2, err := workflow.List(context.Background(), deps, workflow.ListInput{
		MaxResults: 1,
		Cursor:     page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if !page2.DataChanged {
		t.Fatal("page2 DataChanged = false, want true after index mutation")
	}
	// Anchor "A" still present; resume after it.
	if len(page2.Rows) != 1 || page2.Rows[0].Title != "B" {
		t.Fatalf("page2 = %+v, want [B] (resume after anchor A)", page2.Rows)
	}
}

func TestList_PaginationAnchorRemovedRestarts(t *testing.T) {
	deps := listFixture(t,
		makeRow(t, "A", response.ListStatusComplete),
		makeRow(t, "B", response.ListStatusComplete),
		makeRow(t, "C", response.ListStatusComplete),
	)

	page1, err := workflow.List(context.Background(), deps, workflow.ListInput{MaxResults: 1})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}

	// Remove the anchor row.
	deps.Index.Remove(mustParseSeries(t, "A"))

	page2, err := workflow.List(context.Background(), deps, workflow.ListInput{
		MaxResults: 1,
		Cursor:     page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if !page2.DataChanged {
		t.Fatal("page2 DataChanged = false, want true after anchor removal")
	}
	if len(page2.Rows) != 1 || page2.Rows[0].Title != "B" {
		t.Fatalf("page2 = %+v, want [B] (restart from page 1)", page2.Rows)
	}
}

func TestList_PaginationInvalidCursor(t *testing.T) {
	deps := listFixture(t, makeRow(t, "A", response.ListStatusComplete))

	_, err := workflow.List(context.Background(), deps, workflow.ListInput{
		MaxResults: 10,
		Cursor:     "not-a-valid-cursor",
	})
	var bad *workflow.InvalidCursorError
	if !errors.As(err, &bad) {
		t.Fatalf("err = %v, want InvalidCursorError", err)
	}
}

func TestList_PaginationEmptyResultNoCursor(t *testing.T) {
	deps := listFixture(t)
	result, err := workflow.List(context.Background(), deps, workflow.ListInput{MaxResults: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(result.Rows))
	}
	if result.NextCursor != "" {
		t.Fatalf("NextCursor = %q, want empty", result.NextCursor)
	}
}
