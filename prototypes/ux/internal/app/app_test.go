package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pvjammer/nexus/prototypes/ux/internal/files"
	"github.com/pvjammer/nexus/prototypes/ux/internal/store"
	"github.com/pvjammer/nexus/prototypes/ux/internal/vault"
)

const canary = "CANARY-TEST-91ab"

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	r := files.New()
	ws := filepath.Join(dir, "workspace")
	if err := r.Add("workspace", ws); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(ws, "pantry.csv"), []byte("Item,Qty\nRice,2\nOats,1\n"), 0o644)
	os.WriteFile(filepath.Join(ws, "doc.md"), []byte("# Title\n\nThe quick brown fox jumps over the lazy dog.\n"), 0o644)
	vp := filepath.Join(dir, "vault.json")
	os.WriteFile(vp, []byte(`[{"name":"Home Wi-Fi","kind":"password","value":"`+canary+`"}]`), 0o600)
	v, err := vault.Open(vp)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DataDir: dir, WebDir: dir, Token: "tok", TermDir: ws}, r, v)
	if err != nil {
		t.Fatal(err)
	}
	return a, dir
}

// leakCheck greps everything persisted plus everything the agent was sent.
func leakCheck(t *testing.T, dir string, agentSaw ...string) {
	t.Helper()
	for _, s := range agentSaw {
		if strings.Contains(s, canary) {
			t.Fatalf("canary reached the agent: %q", s)
		}
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Base(p) == "vault.json" {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), canary) {
			t.Fatalf("canary leaked into %s", p)
		}
		return nil
	})
}

func TestSecureToolNeverReturnsValue(t *testing.T) {
	a, dir := newTestApp(t)
	out, err := a.CallTool("vault_reveal", map[string]any{"name": "Home Wi-Fi", "reason": "you asked"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "DATA_DELIVERED_OUT_OF_BAND") {
		t.Fatalf("want ack, got %q", out)
	}
	list, _ := a.CallTool("vault_list", nil)
	ctx := a.TurnContext("")
	layout, _ := a.CallTool("layout_get", nil)
	leakCheck(t, dir, out, list, ctx, layout)

	// The pane exists, is private, and the value is held for the shell only.
	var pane store.Pane
	for _, p := range a.Store.Panes("") {
		if p.Kind == "secure-view" {
			pane = p
		}
	}
	if pane.ID == "" || pane.Visibility != store.VisNone || !pane.VisLocked {
		t.Fatalf("secure pane wrong: %+v", pane)
	}
	if v, _ := a.Vault.Delivered(pane.ID); v != canary {
		t.Fatal("value not delivered to the pane")
	}
	if strings.Contains(layout, pane.ID) || strings.Contains(ctx, pane.ID) {
		t.Fatal("private pane visible to the agent")
	}

	// Only an explicit user change puts it in context, and that's logged.
	a.Store.UpdatePane(pane.ID, store.ActorUser, func(p *store.Pane) error { p.Visibility = store.VisFull; return nil })
	a.Store.Activate(pane.ID)
	if !strings.Contains(a.TurnContext(""), canary) {
		t.Fatal("after you shared it, the value should be in context")
	}
	found := false
	for _, e := range a.Log.Recent(50) {
		if strings.Contains(e.Action, "secure value entered agent context") {
			found = true
		}
	}
	if !found {
		t.Fatal("sharing a secure value must be logged")
	}
}

func TestSecretHandleBrokerCall(t *testing.T) {
	a, dir := newTestApp(t)
	out, err := a.CallTool("secret_request", map[string]any{"label": "Homelab API token"})
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out, "secret:")
	handle := out[i : i+len("secret:")+8]
	if _, err := a.CallTool("broker_call", map[string]any{"connection": "homelab-mock", "path": "/whoami", "auth_handle": handle}); err == nil {
		t.Fatal("handle should not be usable before you enter it")
	}
	if err := a.Vault.Fill(handle, canary); err != nil {
		t.Fatal(err)
	}
	res, err := a.CallTool("broker_call", map[string]any{"connection": "homelab-mock", "path": "/whoami", "auth_handle": handle})
	if err != nil || !strings.Contains(res, "auth ok") {
		t.Fatalf("broker call: %v %s", err, res)
	}
	leakCheck(t, dir, out, res, a.TurnContext(""))
	if _, err := a.CallTool("broker_call", map[string]any{"connection": "homelab-mock", "path": "/admin"}); err == nil {
		t.Fatal("non-allowlisted path must be refused")
	}
}

func TestAgentNeverStealsFocusAndUndo(t *testing.T) {
	a, _ := newTestApp(t)
	mine, _ := a.Store.OpenPane("", store.Pane{Kind: "markdown", Title: "mine"}, "", store.ActorUser)
	before, _ := a.Store.FocusedPane()
	if _, err := a.CallTool("pane_open", map[string]any{"kind": "sheet", "path": "workspace/pantry.csv"}); err != nil {
		t.Fatal(err)
	}
	a.CallTool("pane_open", map[string]any{"kind": "markdown", "title": "notes", "content": "x", "placement": "focused"})
	after, _ := a.Store.FocusedPane()
	if after.ID != before.ID || after.ID != mine.ID {
		t.Fatalf("agent moved focus from %s to %s", before.ID, after.ID)
	}
	n := len(a.Store.Panes(""))
	if _, err := a.Store.UndoAgent(""); err != nil {
		t.Fatal(err)
	}
	if len(a.Store.Panes("")) != n-1 {
		t.Fatal("undo should revert exactly the last agent op")
	}
}

func TestPinnedNeedsAsking(t *testing.T) {
	a, _ := newTestApp(t)
	p, _ := a.Store.OpenPane("", store.Pane{Kind: "markdown", Title: "keep"}, "", store.ActorUser)
	a.Store.UpdatePane(p.ID, store.ActorUser, func(p *store.Pane) error { p.Pinned = true; return nil })
	if _, err := a.CallTool("pane_close", map[string]any{"pane_id": p.ID}); err == nil {
		t.Fatal("agent closed a pinned pane")
	}
	if _, err := a.CallTool("layout_preset", map[string]any{"name": "grid"}); err == nil {
		t.Fatal("agent rearranged around a pinned pane without asking")
	}
	last := a.Log.Recent(1)[0]
	if !last.Denied {
		t.Fatal("refusal should be logged as denied")
	}
}

func TestVisibilityRefusesReads(t *testing.T) {
	a, _ := newTestApp(t)
	p, _ := a.Store.OpenPane("", store.Pane{Kind: "sheet", Title: "pantry.csv", Props: map[string]any{"path": "workspace/pantry.csv"}}, "", store.ActorUser)
	if out, err := a.CallTool("sheet_read", map[string]any{"path": "workspace/pantry.csv"}); err != nil || !strings.Contains(out, "Rice") {
		t.Fatalf("visible sheet should be readable: %v", err)
	}
	a.Store.UpdatePane(p.ID, store.ActorUser, func(p *store.Pane) error { p.Visibility = store.VisTitle; return nil })
	if _, err := a.CallTool("sheet_read", map[string]any{"path": "workspace/pantry.csv"}); err == nil {
		t.Fatal("hidden sheet was readable")
	}
	if strings.Contains(a.TurnContext(""), "Rice") {
		t.Fatal("hidden sheet content leaked into turn context")
	}
}

func TestReviewSuggestAccept(t *testing.T) {
	a, _ := newTestApp(t)
	out, err := a.CallTool("review_suggest", map[string]any{"path": "workspace/doc.md", "find": "quick brown fox", "replace": "fast red fox", "note": "punchier"})
	if err != nil {
		t.Fatal(err, out)
	}
	src, _ := a.Review.Content("workspace/doc.md")
	if strings.Contains(src, "fast red") {
		t.Fatal("suggestion must not edit the file before you accept")
	}
	d, _ := a.Review.Get("workspace/doc.md")
	th := d.Threads[0]
	if _, err := a.Review.ResolveSuggestion("workspace/doc.md", th.ID, th.Suggestions[0].ID, true); err != nil {
		t.Fatal(err)
	}
	src, _ = a.Review.Content("workspace/doc.md")
	if !strings.Contains(src, "fast red fox") {
		t.Fatal("accepted suggestion not applied")
	}
}

func TestPluginWriteValidation(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := a.CallTool("plugin_write", map[string]any{"id": "disk-bars", "name": "Disks", "permissions": []any{"tool:vault_reveal"}, "html": "<p>x</p>"}); err == nil {
		t.Fatal("plugins must not be able to declare vault tools")
	}
	if _, err := a.CallTool("plugin_write", map[string]any{"id": "disk-bars", "name": "Disks", "permissions": []any{"connect:homelab-mock"}, "html": "<p>x</p>"}); err != nil {
		t.Fatal(err)
	}
	pl, _ := a.Plugins.Get("disk-bars")
	if d, g := pl.Check("connect:homelab-mock"); !d || g != "" {
		t.Fatal("declared permission should start ungranted")
	}
	if d, _ := pl.Check("tool:sheet_read"); d {
		t.Fatal("undeclared permission reported as declared")
	}
}
