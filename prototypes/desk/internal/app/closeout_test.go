package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoneIsTheUsersAndClosesOut(t *testing.T) {
	a, dir := newTestApp(t)
	a.cfg.SkillsDir = "../../skills"
	w, err := a.workItemAdd("you", "alpha", "Add CSV export", "Orders to CSV for finance.", true)
	if err != nil {
		t.Fatal(err)
	}
	ref := w.Ref
	mustCall(t, a, "p:alpha", "work_item_set", map[string]any{"item": ref, "done_when": []any{"Orders export", "Finance checked it"}, "reason": "shaped"})
	mustCall(t, a, "p:alpha", "work_task_add", map[string]any{"title": "Ask finance for columns", "item": ref, "reason": "next step"})

	// an agent can only claim it
	out := mustCall(t, a, "p:alpha", "work_item_set", map[string]any{"item": ref, "state": "done", "evidence": []any{"exports from the admin page", "Sam signed off"}, "reason": "all checks met"})
	if !strings.Contains(out, "confirm") || a.tracked(ref).State == "done" {
		t.Fatalf("a claim, not done: %s", out)
	}
	var claim *Need
	for _, n := range a.needs() {
		if n.Source == "done" && n.Ref == ref {
			claim = &n
		}
	}
	if claim == nil || len(claim.Checks) != 2 || claim.Checks[1].Evidence != "Sam signed off" {
		t.Fatalf("the claim is in Needs you with its evidence: %+v", claim)
	}
	if err := a.notDone(ref, "finance hasn't seen it"); err != nil {
		t.Fatal(err)
	}
	if a.tracked(ref).Claim != nil {
		t.Fatal("Not yet clears the claim")
	}

	// the user's Done is final: no confirm, a closeout starts
	if _, err := a.handleWork("workItemSet", map[string]any{"item": ref, "state": "done"}); err != nil {
		t.Fatal(err)
	}
	tr := a.tracked(ref)
	if tr.State != "done" || tr.PrevState != "in-progress" || tr.Closeout == "" {
		t.Fatalf("done, with a closeout: %+v", tr)
	}
	tk, _ := a.Desk.Task(tr.Closeout)
	for _, want := range []string{"Add CSV export", "Ask finance for columns", "Loose ends", "How to close out (from skills/closeout)"} {
		if !strings.Contains(tk.Brief, want) {
			t.Fatalf("closeout brief is missing %q:\n%s", want, tk.Brief)
		}
	}
	for _, n := range a.workNeeds() {
		if n.Title == "Ask finance for columns" {
			t.Fatal("a done item's tasks leave your lists")
		}
	}
	if _, err := call(t, a, "p:alpha", "work_item_set", map[string]any{"item": ref, "state": "in-progress", "reason": "x"}); err == nil {
		t.Fatal("agents can't restart done work; the user reopens it")
	}

	// Undo reopens it as it was
	if _, err := a.reopenWorkItem(ref); err != nil {
		t.Fatal(err)
	}
	if tr := a.tracked(ref); tr.State != "in-progress" || tr.Closeout != "" {
		t.Fatalf("reopened: %+v", tr)
	}
	found := false
	for _, n := range a.workNeeds() {
		found = found || n.Title == "Ask finance for columns"
	}
	if !found {
		t.Fatal("its tasks come back")
	}

	// a project's own closeout steps replace the skill
	os.MkdirAll(filepath.Join(dir, "data", "desk-files", "alpha"), 0o755)
	os.WriteFile(filepath.Join(dir, "data", "desk-files", "alpha", closeoutFile), []byte("1. Update CHANGELOG.md in the report.\n"), 0o644)
	a.closeWorkItem(ref)
	tk, _ = a.Desk.Task(a.tracked(ref).Closeout)
	if !strings.Contains(tk.Brief, "CHANGELOG") || !strings.Contains(tk.Brief, "desk/alpha/closeout.md") {
		t.Fatalf("project closeout steps:\n%s", tk.Brief)
	}
}

func TestCloseoutProposalsGoToTheProjectAgent(t *testing.T) {
	a, _ := newTestApp(t)
	w, _ := a.workItemAdd("you", "alpha", "Add CSV export", "", true)
	a.closeWorkItem(w.Ref)
	id := "t:" + a.tracked(w.Ref).Closeout
	mustCall(t, a, id, "work_propose", map[string]any{"proposals": []any{map[string]any{"title": "Add the customers export", "why": "left over"}}, "reason": "what's left"})
	for _, p := range a.openProposals() {
		if p.Title == "Add the customers export" && p.Agent != "p:alpha" {
			t.Fatalf("accepting it should tell the project agent, not the finished task: %s", p.Agent)
		}
	}
}
