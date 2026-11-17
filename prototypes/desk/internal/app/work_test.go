package app

import (
	"strings"
	"testing"
	"time"
)

func TestWorkItemsComeFromTheirSources(t *testing.T) {
	a, _ := newTestApp(t)
	items := a.workItems()
	var ws, bl *WorkItem
	for i := range items {
		switch items[i].Source {
		case "foreman workstream":
			ws = &items[i]
		case "foreman backlog":
			bl = &items[i]
		}
	}
	if ws == nil || ws.Ref != "ws:alpha/ws-one" || ws.State != "in-progress" || ws.Report != "alpha/.foreman/ws/ws-one/report.md" {
		t.Fatalf("open workstream should be an in-progress work item: %+v", ws)
	}
	if bl == nil || bl.State != "not-started" || bl.Title != "first thing" {
		t.Fatalf("open backlog line should be a not-started work item: %+v", bl)
	}
	if ws.Move != "none" || ws.MoveNote != "no next action yet" {
		t.Fatalf("no tasks yet: %+v", ws)
	}
}

func TestTasksBoardAndWhoseMove(t *testing.T) {
	a, _ := newTestApp(t)
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Grant API access", "assignee": "IT", "status": "doing", "reason": "r"})
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Test live", "blocked_by": []any{"Grant API access"}, "reason": "r"})
	w := itemByRef(a, "ws:alpha/ws-one")
	if w.Move != "waiting" || !strings.Contains(w.MoveNote, "IT") || w.Open != 2 {
		t.Fatalf("waiting on IT: %+v", w)
	}
	if len(a.workNeeds()) != 0 {
		t.Fatal("nothing needs the user yet")
	}
	out := mustCall(t, a, "home", "work_tasks", map[string]any{"item": "ws:alpha/ws-one"})
	if !strings.Contains(out, "[blocked] Test live") || !strings.Contains(out, "blocked_by=k") {
		t.Fatalf("listing: %s", out)
	}
	mustCall(t, a, "home", "work_task_update", map[string]any{"id": "Grant API access", "status": "done", "reason": "IT granted it"})
	if w = itemByRef(a, "ws:alpha/ws-one"); w.Move != "you" || w.Done != 1 {
		t.Fatalf("unblocked task should be the user's move: %+v", w)
	}
	needs := a.workNeeds()
	if len(needs) != 1 || needs[0].Title != "Test live" || needs[0].Source != "work" {
		t.Fatalf("needs: %+v", needs)
	}
	found := false
	for _, n := range a.needs() {
		found = found || n.ID == needs[0].ID
	}
	if !found {
		t.Fatal("task needs should be in the needs-you list")
	}
}

func TestParkingHidesTasks(t *testing.T) {
	a, _ := newTestApp(t)
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Decide the date", "reason": "r"})
	if len(a.workNeeds()) != 1 {
		t.Fatal("should need the user")
	}
	mustCall(t, a, "home", "work_item_set", map[string]any{"item": "ws:alpha/ws-one", "state": "parked", "park_note": "until Q1", "reason": "r"})
	if len(a.workNeeds()) != 0 {
		t.Fatal("a parked work item's tasks stay out of the way")
	}
	if w := itemByRef(a, "ws:alpha/ws-one"); w.State != "parked" || w.ParkNote != "until Q1" {
		t.Fatalf("%+v", w)
	}
	if _, err := a.handleWork("workItemSet", map[string]any{"item": "ws:alpha/ws-one", "state": "in-progress"}); err != nil {
		t.Fatal(err)
	}
	if len(a.workNeeds()) != 1 {
		t.Fatal("bringing it back shows the task again")
	}
}

func TestWorkErrorsAndScopes(t *testing.T) {
	a, _ := newTestApp(t)
	wantErr(t, a, "work_task_add", map[string]any{"item": "ws:alpha/nope", "title": "x", "reason": "r"}, "Call work_items for refs")
	wantErr(t, a, "work_task_add", map[string]any{"title": "x", "status": "wip", "reason": "r"}, "status must be one of")
	wantErr(t, a, "work_task_add", map[string]any{"title": "x", "due": "Friday", "reason": "r"}, "2026-11-20")
	wantErr(t, a, "work_task_update", map[string]any{"id": "k999", "status": "done", "reason": "r"}, "work_tasks lists the ids")
	wantErr(t, a, "work_item_set", map[string]any{"item": "ws:alpha/ws-one", "state": "paused", "reason": "r"}, "in-progress, parked, not-started")
	// a project agent works only in its own project
	if _, err := call(t, a, "p:beta", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "x", "reason": "r"}); err == nil {
		t.Fatal("beta's agent added a task to alpha")
	}
	if _, err := call(t, a, "p:alpha", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Mine", "reason": "r"}); err != nil {
		t.Fatal(err)
	}
	// a todo
	tk, err := a.handleWork("workTaskAdd", map[string]any{"title": "Book the hotel", "due": "2026-11-20"})
	if err != nil {
		t.Fatal(err)
	}
	if out := mustCall(t, a, "home", "work_tasks", map[string]any{"needs_me": true}); !strings.Contains(out, "Book the hotel") || !strings.Contains(out, "todo") {
		t.Fatalf("todo should need the user: %s (%+v)", out, tk)
	}
}

func itemByRef(a *App, ref string) WorkItem {
	for _, w := range a.workItems() {
		if w.Ref == ref {
			return w
		}
	}
	return WorkItem{}
}

func TestNeedsYouToolMatchesTheScreen(t *testing.T) {
	a, _ := newTestApp(t)
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Pick the date", "reason": "r"})
	out := mustCall(t, a, "home", "needs_you", nil)
	if !strings.Contains(out, "[foreman question] ws-one") || !strings.Contains(out, "[task (task)] Pick the date") {
		t.Fatalf("needs_you should list foreman questions and tasks: %s", out)
	}
	if out := mustCall(t, a, "p:beta", "needs_you", nil); !strings.Contains(out, "nothing is waiting") {
		t.Fatalf("a project agent sees its own project only: %s", out)
	}
}

func TestInProgressLimitIsConfigurable(t *testing.T) {
	a, _ := newTestApp(t)
	if a.Desk.WipLimit() != 4 {
		t.Fatal("default limit is 4")
	}
	mustCall(t, a, "home", "work_limit", map[string]any{"limit": 1, "reason": "r"})
	var bl string
	for _, w := range a.workItems() {
		if w.Source == "foreman backlog" {
			bl = w.Ref
		}
	}
	out := mustCall(t, a, "home", "work_item_set", map[string]any{"item": bl, "state": "in-progress", "reason": "r"})
	if !strings.Contains(out, "2 work items in progress; their limit is 1") || !strings.Contains(out, "ws:alpha/ws-one") {
		t.Fatalf("over the limit, the agent should be told and given candidates: %s", out)
	}
	if _, err := a.handleWork("workLimit", map[string]any{"limit": float64(0)}); err != nil {
		t.Fatal(err)
	}
	if out := mustCall(t, a, "home", "work_item_set", map[string]any{"item": bl, "state": "in-progress", "reason": "r"}); strings.Contains(out, "limit") {
		t.Fatalf("0 means never: %s", out)
	}
	wantErr(t, a, "work_limit", map[string]any{"limit": 99, "reason": "r"}, "0 to 20")
}

// Regression: setting blocked_by on an existing task resolved names under
// the store's lock and froze the whole store.
func TestUpdatingBlockersDoesNotDeadlock(t *testing.T) {
	a, _ := newTestApp(t)
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Design it", "reason": "r"})
	mustCall(t, a, "home", "work_task_add", map[string]any{"item": "ws:alpha/ws-one", "title": "Build it", "reason": "r"})
	done := make(chan string, 1)
	go func() {
		out, _ := call(t, a, "home", "work_task_update", map[string]any{"id": "Build it", "blocked_by": []any{"Design it"}, "reason": "order"})
		done <- out
	}()
	select {
	case out := <-done:
		if !strings.Contains(out, "[blocked]") && !strings.Contains(out, "Build it") {
			t.Fatalf("unexpected: %s", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("work_task_update with blocked_by hung")
	}
	wantErr(t, a, "work_task_update", map[string]any{"id": "Build it", "blocked_by": []any{"Build it"}, "reason": "r"}, "itself")
	if _, err := a.handleWork("workTaskUpdate", map[string]any{"id": "Design it", "blockedBy": []any{"Build it"}}); err != nil {
		t.Fatal(err) // the task panel's "waits on" goes the same way
	}
}
