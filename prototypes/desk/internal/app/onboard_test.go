package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pvjammer/nexus/prototypes/desk/internal/scan"
)

// a small repo with every kind of sign of work
func workRepo(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "gamma")
	for _, d := range []string{"src", "docs/design", "notes", "vendored/.git", "node_modules/x"} {
		os.MkdirAll(filepath.Join(p, d), 0o755)
	}
	w := func(rel, s string) { os.WriteFile(filepath.Join(p, rel), []byte(s), 0o644) }
	w("README.md", "# Gamma\n\nGamma exports orders.\n\n## Roadmap\n\n- Add CSV export\n- ~~Old idea~~\n- [x] Shipped thing\n\n## Usage\n\n- not a plan item\n")
	w("src/main.go", "package main\n\n// TODO: handle retries on timeout\nfunc main() {\n\ts := \"TODO in a string\"\n\t_ = s\n}\n")
	w("docs/design/export.md", "# Export design\n\n## Open questions\n\n1. Which columns does finance need?\n\n```\n- [ ] not a real checkbox\n```\n")
	w("notes/todo.md", "# Todo\n\n- [ ] Write the release notes\n- [x] done already\n")
	w(".env", "API_KEY="+canary+"\n- [ ] secret checkbox\n")
	w("vendored/notes.md", "- [ ] belongs to another repo\n")
	w("node_modules/x/readme.md", "- [ ] dependency noise\n")
	g := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", p, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	g("init", "-q", "-b", "main")
	g("add", "README.md", "src", "docs", "notes")
	g("commit", "-qm", "init")
	g("checkout", "-qb", "auth-v2")
	w("src/auth.go", "package main\n")
	g("add", "src/auth.go")
	g("commit", "-qm", "auth: new tokens")
	g("checkout", "-q", "main")
	w("src/wip.go", "package main\n")
	return p
}

func TestScanFindsSignsOfWork(t *testing.T) {
	r := scan.Project(workRepo(t, t.TempDir()))
	has := func(fs []scan.Finding, text, where string) bool {
		for _, f := range fs {
			if strings.Contains(f.Text, text) && strings.HasPrefix(f.Where, where) {
				return true
			}
		}
		return false
	}
	if !has(r.Todos, "TODO: handle retries", "src/main.go:3") || len(r.Todos) != 1 {
		t.Fatalf("todo comments only (not strings): %+v", r.Todos)
	}
	if !has(r.Checks, "Write the release notes", "notes/todo.md:3") || len(r.Checks) != 1 {
		t.Fatalf("open checkboxes only, not in fences, secrets, nested repos or deps: %+v", r.Checks)
	}
	if !has(r.Plans, "Add CSV export", "README.md") || !has(r.Plans, "Which columns", "docs/design/export.md") {
		t.Fatalf("plan sections: %+v", r.Plans)
	}
	if has(r.Plans, "Old idea", "") || has(r.Plans, "Shipped thing", "") || has(r.Plans, "not a plan item", "") {
		t.Fatalf("struck-out, done and non-plan bullets are skipped: %+v", r.Plans)
	}
	if !has(r.Docs, "Export design", "docs/design/export.md") {
		t.Fatalf("docs: %+v", r.Docs)
	}
	if len(r.Branches) != 1 || !strings.Contains(r.Branches[0].Text, "auth-v2: auth: new tokens (1 commits not on main)") {
		t.Fatalf("branches: %+v", r.Branches)
	}
	if r.Changes == nil || !strings.Contains(r.Changes.Text, "wip.go") {
		t.Fatalf("uncommitted: %+v", r.Changes)
	}
}

func TestOnboardingProposeAcceptShape(t *testing.T) {
	a, dir := newTestApp(t)
	if _, err := a.AddProject("Gamma", workRepo(t, dir)); err != nil {
		t.Fatal(err)
	}
	out := mustCall(t, a, "p:gamma", "project_scan", nil)
	for _, want := range []string{"branch auth-v2", "notes/todo.md:3 · Write the release notes", "src/main.go:3", "docs/design/export.md"} {
		if !strings.Contains(out, want) {
			t.Fatalf("scan text missing %q:\n%s", want, out)
		}
	}
	leakCheck(t, dir, out)
	if _, ok := a.Desk.Onboarding("gamma"); !ok {
		t.Fatal("scan should be recorded")
	}

	// propose: the user gets a checklist; Needs you shows it
	mustCall(t, a, "p:gamma", "work_propose", map[string]any{"reason": "setup", "proposals": []any{
		map[string]any{"title": "Finish the auth refactor", "why": "branch", "evidence": []any{"branch auth-v2"}},
		map[string]any{"title": "Add CSV export"},
		map[string]any{"title": "Write the release notes"},
	}})
	ps := a.openProposals()
	if len(ps) != 3 || ps[0].Agent != "p:gamma" || ps[0].Evidence[0] != "branch auth-v2" {
		t.Fatalf("proposals: %+v", ps)
	}
	found := false
	for _, n := range a.needs() {
		found = found || (n.Source == "proposals" && n.Count == 3 && n.Project == "gamma")
	}
	if !found {
		t.Fatalf("Needs you should show the batch: %+v", a.needs())
	}
	// the user accepts two (starting them) and dismisses one
	if _, err := a.handleOnboard("proposalsResolve", map[string]any{"accept": []any{ps[0].ID, ps[1].ID}, "dismiss": []any{ps[2].ID}, "start": true}); err != nil {
		t.Fatal(err)
	}
	w := itemByRefApp(a, "Finish the auth refactor")
	if w2, err := a.checkItemRef("Finish the auth refactor"); err != nil || w2.State != "in-progress" || !strings.Contains(w2.Detail, "branch auth-v2") {
		t.Fatalf("accepted + started, evidence kept: %+v %v (%+v)", w2, err, w)
	} else if !w2.Unshaped {
		t.Fatal("no done-when or tasks yet: needs shaping")
	}
	if len(a.openProposals()) != 0 {
		t.Fatal("all handled")
	}
	// a rescan doesn't offer the same things again
	out, _ = call(t, a, "p:gamma", "work_propose", map[string]any{"reason": "again", "proposals": []any{
		map[string]any{"title": "write the release notes"}, map[string]any{"title": "Finish the auth refactor"}}})
	if !strings.Contains(out, "Skipped") || len(a.openProposals()) != 0 {
		t.Fatalf("dismissed and existing items aren't proposed again: %s", out)
	}

	// shaping: brief + done-when, then tasks proposed and accepted
	mustCall(t, a, "p:gamma", "work_item_set", map[string]any{"item": "Finish the auth refactor", "brief": "Move to short-lived tokens.", "done_when": []any{"Merged", "Old tokens revoked"}, "reason": "answers"})
	mustCall(t, a, "p:gamma", "work_propose", map[string]any{"item": "Finish the auth refactor", "reason": "next", "proposals": []any{
		map[string]any{"title": "Rebase auth-v2 on main", "assignee": "you"}, map[string]any{"title": "Draft the migration note", "assignee": "agent"}}})
	tp := a.openProposals()
	if len(tp) != 2 || tp[0].Item == "" {
		t.Fatalf("task proposals: %+v", tp)
	}
	if _, err := a.resolveProposals([]string{tp[0].ID, tp[1].ID}, nil, false); err != nil {
		t.Fatal(err)
	}
	w2, _ := a.checkItemRef("Finish the auth refactor")
	if w2.Open != 2 || w2.Unshaped || w2.Brief != "Move to short-lived tokens." || w2.Move != "you" {
		t.Fatalf("shaped: %+v", w2)
	}
	if out := mustCall(t, a, "p:gamma", "work_tasks", map[string]any{"item": w2.Ref}); !strings.Contains(out, "Draft the migration note · assignee=agent") && !strings.Contains(out, "assignee=agent") {
		t.Fatalf("tasks: %s", out)
	}
}

func TestOnboardingToolsCheckTheirInput(t *testing.T) {
	a, _ := newTestApp(t)
	wantErrAs(t, a, "p:alpha", "ask_user", map[string]any{"question": "When?", "options": []any{"Now"}}, "2-5 options")
	wantErrAs(t, a, "p:alpha", "work_propose", map[string]any{"reason": "r", "proposals": []any{}}, "give 1 to 8")
	wantErrAs(t, a, "p:alpha", "work_propose", map[string]any{"reason": "r", "proposals": []any{map[string]any{"why": "no title"}}}, "needs a title")
	wantErrAs(t, a, "p:beta", "work_propose", map[string]any{"item": "ws:alpha/ws-one", "reason": "r", "proposals": []any{map[string]any{"title": "x"}}}, "not yours")
	if out := mustCall(t, a, "p:alpha", "ask_user", map[string]any{"question": "When?", "options": []any{"Now", "Later"}}); !strings.Contains(out, "end your turn") {
		t.Fatal(out)
	}
	mustCall(t, a, "p:alpha", "work_item_add", map[string]any{"title": "Add dark mode", "start": true, "brief": "theme", "reason": "r"})
	wantErrAs(t, a, "p:alpha", "work_item_add", map[string]any{"title": "add dark mode", "reason": "r"}, "already exists")
	if w, err := a.checkItemRef("Add dark mode"); err != nil || w.State != "in-progress" || w.Brief != "theme" {
		t.Fatalf("%+v %v", w, err)
	}
}

func TestForemanIsOffUnlessSwitchedOn(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.Foreman = false
	a.refreshDetails()
	d, _ := a.detail("alpha")
	if !d.Foreman || len(d.Workstreams)+len(d.Backlog)+len(d.Inbox) != 0 {
		t.Fatalf("with the add-on off, a ledger is only noted: %+v", d)
	}
	for _, w := range a.workItems() {
		if strings.HasPrefix(w.Ref, "ws:") || strings.HasPrefix(w.Ref, "bl:") {
			t.Fatalf("no foreman work items when off: %+v", w)
		}
	}
	if len(a.needs()) != 0 {
		t.Fatalf("no foreman needs when off: %+v", a.needs())
	}
	if err := a.setAddon("alpha", "foreman", true); err != nil {
		t.Fatal(err)
	}
	a.refreshDetails()
	if d, _ := a.detail("alpha"); len(d.Workstreams) != 1 {
		t.Fatal("a project can switch it on")
	}
}

func wantErrAs(t *testing.T, a *App, agent, tool string, args map[string]any, want string) {
	t.Helper()
	_, err := call(t, a, agent, tool, args)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: want error containing %q, got %v", tool, want, err)
	}
}
