package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const zebra = "ZEBRA-PRIVATE"

// privateFixture: beta is private, with a work item, a task, a note, a
// file and a wiki page that all carry the canary.
func privateFixture(t *testing.T) (*App, string, string, string) {
	t.Helper()
	a, dir := newTestApp(t)
	a.cfg.SkillsDir = "../../skills"
	os.WriteFile(filepath.Join(dir, "beta", "zebra.md"), []byte("# "+zebra+" plans\n"), 0o644)
	w, err := a.workItemAdd("you", "beta", zebra+" taxes", zebra+" brief", true)
	if err != nil {
		t.Fatal(err)
	}
	tk, err := a.workTaskAdd("you", "", TaskEdit{Title: strp(zebra + " file the return"), Item: &w.Ref, Due: strp("2026-11-20")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, a, "p:beta", "wiki_write", map[string]any{"page": "index", "content": "# " + zebra + "\n", "summary": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, a, "p:beta", "item_add", map[string]any{"kind": "note", "title": zebra + " note"}); err != nil {
		t.Fatal(err)
	}
	if err := a.setPrivate("beta", true); err != nil {
		t.Fatal(err)
	}
	// something on alpha too, so outsiders have real work to see
	aw, _ := a.workItemAdd("you", "alpha", "Add CSV export", "", true)
	a.workTaskAdd("you", "", TaskEdit{Title: strp("Ask finance"), Item: &aw.Ref})
	return a, w.Ref, tk.ID, aw.Ref
}

// TestPrivateProjectWall calls every tool every outside agent has, with
// arguments aimed at the private project, and checks nothing of it comes
// back: not its titles, refs, ids, files or wiki. Its own agent still sees it.
func TestPrivateProjectWall(t *testing.T) {
	a, ref, taskID, alphaRef := privateFixture(t)
	secret := []string{zebra, ref, taskID}
	alphaTask := ""
	for _, x := range a.Desk.Snapshot().Tasks {
		_ = x
	}
	dt := a.Desk.AddTask(deskTask("alpha", "look at alpha", "brief", ""))
	alphaTask = "t:" + dt.ID
	variants := []map[string]any{
		{}, {"project": "beta"}, {"item": ref}, {"item": zebra + " taxes"}, {"id": taskID}, {"query": zebra}, {"query": "zebra"},
		{"path": "beta/zebra.md"}, {"path": "beta"}, {"page": "index", "project": "beta"}, {"all": true}, {"include_done": true},
		{"text": "x", "item": ref}, {"question": "x", "item": ref}, {"name": zebra},
	}
	a.cfg.PersonasDir = "../../personas"
	outsiders := []string{"home", customizerID, "p:alpha", workstreamID(alphaRef), alphaTask, "persona:critic:home"}
	for _, id := range outsiders {
		r, err := a.run(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, tl := range a.toolsFor(r) {
			if tl.Name == "project_add" { // it would add real folders (this test's own) as projects
				continue
			}
			for _, args := range variants {
				cp := map[string]any{"reason": "sweep"}
				for k, v := range args {
					cp[k] = v
				}
				out, err := a.CallTool(r, tl.Name, cp)
				r.mu.Lock()
				r.asked = ""
				r.mu.Unlock()
				text := out
				if err != nil {
					text += " " + err.Error()
				}
				for _, k := range cp { // echoing back what the caller sent is fine: it says nothing new
					if v, ok := k.(string); ok {
						text = strings.ReplaceAll(text, v, "<arg>")
					}
				}
				for _, s := range secret {
					if strings.Contains(text, s) {
						t.Fatalf("%s's %s %v leaked %q:\n%s", id, tl.Name, args, s, text)
					}
				}
			}
		}
		for _, view := range []string{"home", "beta", "w:" + ref, "project beta"} {
			sel := zebra + "-selected-inside-the-private-project" // must stay there
			if view == "home" {
				sel = "" // on the home screen a selection is the user's to share
			}
			ctx := a.contextFor(r, sel, view)
			for _, s := range secret {
				if strings.Contains(ctx, s) {
					t.Fatalf("%s's context (view %s) leaked %q:\n%s", id, view, s, ctx)
				}
			}
		}
	}
	// the outside agents see it's there: counts, a day, no titles
	home := a.contextFor(a.mustRun(t, "home"), "", "home")
	if !strings.Contains(home, "private project") || !strings.Contains(home, "due Fri") {
		t.Fatalf("home sees that private work exists:\n%s", home)
	}
	if strings.Contains(home, `"Beta"`) {
		t.Fatalf("not even its name:\n%s", home)
	}
	// its own agents see everything
	if ctx := a.contextFor(a.mustRun(t, "p:beta"), "", ""); !strings.Contains(ctx, zebra) {
		t.Fatalf("the private project's own agent sees its work:\n%s", ctx)
	}
	if out := mustCall(t, a, "p:beta", "work_tasks", map[string]any{}); !strings.Contains(out, zebra) {
		t.Fatalf("its own tools too: %s", out)
	}
}

func (a *App) mustRun(t *testing.T, id string) *Run {
	t.Helper()
	r, err := a.run(id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
