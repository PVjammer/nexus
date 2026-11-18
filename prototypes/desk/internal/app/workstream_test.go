package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func startedItem(t *testing.T, a *App, title string) WorkItem {
	t.Helper()
	w, err := a.workItemAdd("you", "alpha", title, "Orders to CSV for finance.", true)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWorkstreamAgentIsAProjectAgentForOneItem(t *testing.T) {
	a, _ := newTestApp(t)
	w := startedItem(t, a, "Add CSV export")
	r, err := a.run(workstreamID(w.Ref))
	if err != nil || r.Kind != KindWorkstream || r.Project != "alpha" || !strings.Contains(r.label(), "Add CSV export") {
		t.Fatalf("a workstream run: %+v %v", r, err)
	}
	has := map[string]bool{}
	for _, tl := range a.toolsFor(r) {
		has[tl.Name] = true
	}
	for _, want := range []string{"work_task_add", "work_item_set", "pane_open", "ws_transcript"} {
		if !has[want] {
			t.Fatalf("a workstream agent has the project agent's tools; missing %s", want)
		}
	}
	ctx := a.contextFor(r, "", "")
	if !strings.Contains(ctx, "<workstream") || !strings.Contains(ctx, "Orders to CSV for finance.") {
		t.Fatalf("its context has its work item:\n%s", ctx)
	}
	if !strings.Contains(a.projectContext("alpha", ""), "(agent w:"+w.Ref+")") {
		t.Fatal("the orchestrator sees each workstream's line")
	}
	if !strings.Contains(a.instructionsFor(KindWorkstream), "You are a workstream agent") {
		t.Fatal("its instructions say what it is")
	}
}

func TestOrchestratorReadsAndMessagesAWorkstream(t *testing.T) {
	a, _ := newTestApp(t)
	w := startedItem(t, a, "Add CSV export")
	r, _ := a.run(workstreamID(w.Ref))
	for i := 1; i <= 30; i++ {
		r.add(ChatMsg{Role: "you", Text: fmt.Sprintf("message %d", i)})
	}
	r.add(ChatMsg{Role: "agent", Text: "We decided on semicolons for the separator."})
	out := mustCall(t, a, "p:alpha", "ws_transcript", map[string]any{"item": "Add CSV export"})
	if !strings.Contains(out, "semicolons") || !strings.Contains(out, "of 31") || !strings.Contains(out, "from=") || strings.Contains(out, "message 1\n") {
		t.Fatalf("the newest window, and how to get more:\n%s", out)
	}
	out = mustCall(t, a, "p:alpha", "ws_transcript", map[string]any{"item": "Add CSV export", "query": "decided"})
	if !strings.Contains(out, "#31") || strings.Contains(out, "message 5") {
		t.Fatalf("search finds the decision:\n%s", out)
	}
	out = mustCall(t, a, "p:alpha", "ws_transcript", map[string]any{"item": "Add CSV export", "from": 1, "count": 5})
	if !strings.Contains(out, "message 1") || !strings.Contains(out, "from=6") {
		t.Fatalf("a window from the start:\n%s", out)
	}

	// a message waits for the workstream's turn to end
	r.mu.Lock()
	r.busy = true
	r.mu.Unlock()
	out = mustCall(t, a, "p:alpha", "ws_message", map[string]any{"item": w.Ref, "text": "use semicolons"})
	if !strings.Contains(out, "Queued") || len(a.queues()[r.ID]) != 1 || a.queues()[r.ID][0].From != "project agent (alpha)" {
		t.Fatalf("queued while busy: %s %+v", out, a.queues())
	}
	r.mu.Lock()
	r.busy = false
	r.mu.Unlock()
	r.turnEnded()
	if len(a.queues()[r.ID]) != 0 {
		t.Fatal("delivered when the turn ends")
	}
	if _, err := call(t, a, "p:alpha", "ws_message", map[string]any{"item": "nope", "text": "x"}); err == nil {
		t.Fatal("unknown work items are refused with refs")
	}
}

func TestSinceYourLastTurn(t *testing.T) {
	a, dir := newTestApp(t)
	alpha := filepath.Join(dir, "alpha")
	gitT := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", alpha, "-c", "user.name=Sam", "-c", "user.email=sam@x"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	gitT("init", "-q")
	gitT("add", "README.md", "docs")
	gitT("commit", "-qm", "init")
	w := startedItem(t, a, "Add CSV export")
	r, _ := a.run(workstreamID(w.Ref))
	a.markTurn(r)
	if a.sinceLastTurn(r, 12) != "" {
		t.Fatal("nothing changed yet")
	}
	// the user, and another agent, change things; the user edits the repo
	if _, err := a.handleWork("workTaskAdd", map[string]any{"title": "Ask finance for columns", "item": w.Ref}); err != nil {
		t.Fatal(err)
	}
	mustCall(t, a, "p:alpha", "work_task_add", map[string]any{"title": "Write the CSV writer", "item": w.Ref, "assignee": "agent", "reason": "next"})
	os.WriteFile(filepath.Join(alpha, "README.md"), []byte("# Alpha\n\nchanged\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, "new.go"), []byte("package x\n"), 0o644)
	os.Remove(filepath.Join(alpha, "docs", "plan.md"))
	note := a.sinceLastTurn(r, 12)
	for _, want := range []string{`task "Ask finance for columns" added (todo) · by the user`, `task "Write the CSV writer" added (todo) · by project agent (alpha)`, "README.md", "new.go", "deleted: docs/plan.md", "outside Nexus"} {
		if !strings.Contains(note, want) {
			t.Fatalf("the note is missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, canary) {
		t.Fatal("file contents never go in the note")
	}
	// a commit says who made it
	gitT("add", "-A")
	gitT("commit", "-qm", "csv: first pass", "-m", "Co-Authored-By: Claude <noreply@anthropic.com>")
	note = a.sinceLastTurn(r, 12)
	if !strings.Contains(note, `commit`) || !strings.Contains(note, "by Sam with Claude") {
		t.Fatalf("commits are attributed:\n%s", note)
	}
	// its own changes aren't news to it
	a.markTurn(r)
	mustCall(t, a, workstreamID(w.Ref), "work_task_add", map[string]any{"title": "Test it", "item": w.Ref, "reason": "next"})
	if note := a.sinceLastTurn(r, 12); strings.Contains(note, "Test it") {
		t.Fatalf("its own change isn't in its note:\n%s", note)
	}
}

func TestHandOffGoesToTheWorkstreamAndEndsInReview(t *testing.T) {
	a, _ := newTestApp(t)
	w := startedItem(t, a, "Add CSV export")
	tk, err := a.workTaskAdd("you", "", TaskEdit{Title: strp("Draft the column list"), Item: &w.Ref})
	if err != nil {
		t.Fatal(err)
	}
	nt, err := a.handOff(tk.ID)
	if err != nil || nt.Assignee != "agent" || nt.Status != "doing" {
		t.Fatalf("handed off: %+v %v", nt, err)
	}
	if ag, ws := a.agentFor(nt); ag != workstreamID(w.Ref) || !ws {
		t.Fatalf("its workstream does it: %s", ag)
	}
	mustCall(t, a, workstreamID(w.Ref), "work_task_update", map[string]any{"id": tk.ID, "status": "done", "note": "columns in desk/alpha/columns.md", "reason": "did it"})
	got, _ := a.findTask(tk.ID)
	if got.Status != "review" || got.Reviewer != "you" {
		t.Fatalf("an agent's finished task goes to your review: %+v", got)
	}
	// done closes the workstream (its conversation is kept)
	r, _ := a.run(workstreamID(w.Ref))
	r.add(ChatMsg{Role: "agent", Text: "kept"})
	a.closeWorkItem(w.Ref)
	if !r.closed || len(a.chatOf(workstreamID(w.Ref))) == 0 {
		t.Fatal("done closes the run and keeps the conversation")
	}
}
