package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
)

func mustCall(t *testing.T, a *App, agent, tool string, args map[string]any) string {
	t.Helper()
	out, err := call(t, a, agent, tool, args)
	if err != nil {
		t.Fatalf("%s %v: %v", tool, args, err)
	}
	return out
}

func wantErr(t *testing.T, a *App, tool string, args map[string]any, hint string) {
	t.Helper()
	_, err := call(t, a, "home", tool, args)
	if err == nil || !strings.Contains(err.Error(), hint) {
		t.Fatalf("%s %v: want an error mentioning %q, got %v", tool, args, hint, err)
	}
}

func TestHomeStartsAsThePrompt(t *testing.T) {
	a, _ := newTestApp(t)
	l := a.Desk.Layout("home")
	if len(l.Widgets) != 1 || l.Widgets[0].Kind != "chat" || l.Widgets[0].Shape != "minimal" {
		t.Fatalf("home should be just the prompt: %s", a.describeHome())
	}
}

func TestHomeAgentEditsTheHomeScreen(t *testing.T) {
	a, _ := newTestApp(t)
	out := mustCall(t, a, "home", "home_add", map[string]any{"kind": "needs", "project": "alpha", "w": 2, "position": 2, "reason": "user asked"})
	if !strings.Contains(out, "needs (list), project alpha, 2 wide") {
		t.Fatalf("add result should show the layout: %s", out)
	}
	l := a.Desk.Layout("home")
	w := l.Widgets[1]
	if w.Kind != "needs" || w.Scope != "p:alpha" || w.By != "agent" || w.Why != "user asked" {
		t.Fatalf("bad widget: %+v", w)
	}
	mustCall(t, a, "home", "home_add", map[string]any{"kind": "running", "shape": "compact", "reason": "r"})
	mustCall(t, a, "home", "home_change", map[string]any{"id": w.ID, "shape": "compact", "project": "all", "h": 2, "position": 3, "reason": "r"})
	l = a.Desk.Layout("home")
	if l.Widgets[2].ID != w.ID || l.Widgets[2].Scope != "" || l.Widgets[2].H != 2 || l.Widgets[2].Shape != "compact" {
		t.Fatalf("change didn't apply: %s", a.describeHome())
	}
	mustCall(t, a, "home", "home_grid", map[string]any{"cols": 2, "tiling": "tile", "reason": "r"})
	if l = a.Desk.Layout("home"); l.Cols != 2 || l.Tiling != "tile" {
		t.Fatalf("grid: %+v", l)
	}
	mustCall(t, a, "home", "home_remove", map[string]any{"id": w.ID, "reason": "r"})
	if strings.Contains(a.describeHome(), w.ID) {
		t.Fatal("not removed")
	}
	mustCall(t, a, "home", "home_undo", nil)
	if !strings.Contains(a.describeHome(), w.ID) {
		t.Fatal("undo didn't bring it back")
	}
	mustCall(t, a, "home", "home_reset", map[string]any{"to": "dashboard", "reason": "r"})
	if l = a.Desk.Layout("home"); len(l.Widgets) != 6 || l.Widgets[0].Kind != "chat" {
		t.Fatalf("dashboard preset: %s", a.describeHome())
	}
	mustCall(t, a, "home", "home_reset", map[string]any{"to": "prompt", "reason": "r"})
	if l = a.Desk.Layout("home"); len(l.Widgets) != 1 {
		t.Fatalf("prompt preset: %s", a.describeHome())
	}
}

// Errors say how to fix the call: small models recover from these.
func TestHomeErrorsSayHowToFix(t *testing.T) {
	a, _ := newTestApp(t)
	wantErr(t, a, "home_add", map[string]any{"kind": "calendar", "reason": "r"}, "Kinds: chat, needs")
	wantErr(t, a, "home_add", map[string]any{"kind": "needs", "shape": "board", "reason": "r"}, "Use one of: list, compact")
	wantErr(t, a, "home_add", map[string]any{"kind": "needs", "project": "gamma", "reason": "r"}, "Project ids: alpha, beta")
	wantErr(t, a, "home_add", map[string]any{"kind": "projects", "project": "alpha", "reason": "r"}, "always shows everything")
	wantErr(t, a, "home_add", map[string]any{"kind": "needs", "w": 9, "reason": "r"}, "w must be 1 to 4")
	wantErr(t, a, "home_add", map[string]any{"kind": "needs"}, "reason is required")
	wantErr(t, a, "home_change", map[string]any{"id": "w999", "w": 2, "reason": "r"}, "Widgets now: chat chat")
	wantErr(t, a, "home_change", map[string]any{"id": "chat", "reason": "r"}, "nothing to change")
	wantErr(t, a, "home_grid", map[string]any{"tiling": "masonry", "reason": "r"}, "flow")
	wantErr(t, a, "home_reset", map[string]any{"to": "fancy", "reason": "r"}, "prompt, dashboard")
	if l := a.Desk.Layout("home"); len(l.Widgets) != 1 {
		t.Fatal("a failed call changed the layout")
	}
}

func TestHomeToolsAreTheHomeAgentsOnly(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := call(t, a, "p:alpha", "home_add", map[string]any{"kind": "needs", "reason": "r"}); err == nil {
		t.Fatal("a project agent changed the home screen")
	}
}

func TestYouAndTheAgentShareTheSamePath(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.handleHome("homeAdd", map[string]any{"kind": "tasks", "shape": "board", "scope": "p:beta", "w": float64(3)}); err != nil {
		t.Fatal(err)
	}
	w := a.Desk.Layout("home").Widgets[1]
	if w.By != "you" || w.Scope != "p:beta" || w.W != 3 || w.Shape != "board" {
		t.Fatalf("%+v", w)
	}
	if err := a.handleHome("homeChange", map[string]any{"id": w.ID, "shape": "grid"}); err == nil {
		t.Fatal("the shell bypassed validation")
	}
	if err := a.handleHome("homeUndo", nil); err != nil {
		t.Fatal(err)
	}
	if len(a.Desk.Layout("home").Widgets) != 1 {
		t.Fatal("undo from the shell didn't work")
	}
}

func TestLegacyWidgetsMoveOntoHome(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "desk.json")
	b, _ := json.Marshal(map[string]any{"widgets": []map[string]any{{"id": "w3", "plugin": "glance", "wide": true}}, "seq": 3})
	os.WriteFile(p, b, 0o600)
	s, err := desk.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	l := s.Layout("home")
	if len(l.Widgets) != 2 || l.Widgets[1].Plugin != "glance" || l.Widgets[1].W != 2 {
		t.Fatalf("legacy widget not migrated: %+v", l.Widgets)
	}
	if len(s.Snapshot().Widgets) != 0 {
		t.Fatal("legacy list should be emptied")
	}
}

func TestTranscriptsSurviveARestart(t *testing.T) {
	a, dir := newTestApp(t)
	r, _ := a.run("home")
	r.add(ChatMsg{Role: "you", Text: "remember the hotel"})
	r.persist()
	b, err := New(Config{DataDir: filepath.Join(dir, "data"), WebDir: dir, Token: "tok"}, a.Vault)
	if err != nil {
		t.Fatal(err)
	}
	r2 := b.existingRun("home")
	if r2 == nil {
		t.Fatal("saved conversation wasn't reloaded at startup")
	}
	msgs, _ := r2.history()
	if len(msgs) != 1 || msgs[0].Text != "remember the hotel" || !msgs[0].Done {
		t.Fatalf("transcript: %+v", msgs)
	}
	_ = time.Second
}
