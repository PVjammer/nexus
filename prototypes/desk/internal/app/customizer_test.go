package app

import (
	"strings"
	"testing"
)

func TestCustomizerWorksOnADraft(t *testing.T) {
	a, _ := newTestApp(t)
	before := a.Desk.Layout("home")
	out := mustCall(t, a, customizerID, "home_add", map[string]any{"kind": "needs", "reason": "show what needs me"})
	if !strings.Contains(out, "on the draft") {
		t.Fatalf("the customizer should be told it's a draft: %s", out)
	}
	if len(a.Desk.Layout("home").Widgets) != len(before.Widgets) {
		t.Fatal("the live home screen must not change while it builds")
	}
	st := a.customizerState()
	if st["state"] != "ready" || len(st["added"].([]string)) != 1 {
		t.Fatalf("a draft with a new widget: %+v", st)
	}
	// a build waits for Keep at the end of the turn
	a.customizerSettle()
	if !a.Desk.HasLayout(homeDraft) {
		t.Fatal("a new widget waits for the user to try it")
	}
	if err := a.customizerKeep("test"); err != nil {
		t.Fatal(err)
	}
	if a.Desk.HasLayout(homeDraft) || len(a.Desk.Layout("home").Widgets) != len(before.Widgets)+1 {
		t.Fatal("Keep puts the draft live")
	}
	if n, label := a.Desk.UndoInfo("home"); n == 0 || !strings.Contains(label, CustomizerName) {
		t.Fatalf("Keep is one undoable change: %d %q", n, label)
	}

	// a small change goes live at the end of the turn
	id := a.Desk.Layout("home").Widgets[1].ID
	mustCall(t, a, customizerID, "home_change", map[string]any{"id": id, "shape": "compact", "reason": "smaller"})
	a.customizerSettle()
	if a.Desk.HasLayout(homeDraft) || a.Desk.Layout("home").Widgets[1].Shape != "compact" {
		t.Fatal("small changes apply straight away")
	}

	// roll back
	mustCall(t, a, customizerID, "home_grid", map[string]any{"cols": 2, "tiling": "tile", "reason": "tile it"})
	a.customizerSettle()
	if a.customizerState()["state"] != "ready" {
		t.Fatal("a new grid waits for Keep")
	}
	if err := a.customizerRollback(); err != nil {
		t.Fatal(err)
	}
	if l := a.Desk.Layout("home"); l.Tiling == "tile" || a.Desk.HasLayout(homeDraft) {
		t.Fatal("rolled back")
	}
	// the home agent still changes the live screen
	mustCall(t, a, "home", "home_grid", map[string]any{"cols": 2, "reason": "two"})
	if a.Desk.Layout("home").Cols != 2 {
		t.Fatal("home agent edits are live")
	}
}

func TestCustomizerSeesWhatYouPointAt(t *testing.T) {
	a, _ := newTestApp(t)
	a.homeReset("you", "dashboard", "")
	w := a.Desk.Layout("home").Widgets[1]
	ctx := a.customizerContext("home", w.ID)
	if !strings.Contains(ctx, "pointed at widget "+w.ID) {
		t.Fatalf("%s", ctx)
	}
	// the customizer only gets the layout tools (and ask_user)
	r, _ := a.run(customizerID)
	for _, tl := range a.toolsFor(r) {
		if !strings.HasPrefix(tl.Name, "home_") && tl.Name != "ask_user" {
			t.Fatalf("customizer shouldn't have %s", tl.Name)
		}
	}
}

func TestCustomizerDiffIgnoresShiftsFromAnInsert(t *testing.T) {
	a, _ := newTestApp(t)
	a.homeReset("you", "dashboard", "")
	mustCall(t, a, customizerID, "home_add", map[string]any{"kind": "board", "position": 2, "reason": "board under the prompt"})
	added, changed, removed, grid := a.draftDiff()
	if len(added) != 1 || len(changed) != 0 || len(removed) != 0 || grid {
		t.Fatalf("one widget added, nothing else: %v %v %v %v", added, changed, removed, grid)
	}
	mustCall(t, a, customizerID, "home_change", map[string]any{"id": "projects", "position": 2, "reason": "projects up top"})
	if _, changed, _, _ = a.draftDiff(); len(changed) == 0 {
		t.Fatal("a real move is a change")
	}
}
