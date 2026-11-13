package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/store"
	"github.com/pvjammer/nexus/prototypes/desk/internal/vault"
)

const canary = "CANARY-TEST-91ab"

// newTestApp makes an app with two real-looking projects: "alpha" (with a
// foreman ledger, a doc and a .env) and "beta".
func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	alpha := filepath.Join(dir, "alpha")
	beta := filepath.Join(dir, "beta")
	for _, d := range []string{alpha + "/.foreman/ws/ws-one", alpha + "/docs", beta} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(filepath.Join(alpha, "README.md"), []byte("# Alpha\n\nAlpha is a test project about widgets.\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, "docs", "plan.md"), []byte("# Plan\n\nThe quick brown fox jumps over the lazy dog.\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".env"), []byte("API_KEY="+canary+"\n"), 0o600)
	os.WriteFile(filepath.Join(alpha, ".foreman", "config.toml"), []byte("name='alpha'\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".foreman", "backlog.md"), []byte("# Backlog\n\n- [ ] first thing\n- [x] done thing\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".foreman", "inbox.jsonl"), []byte(
		`{"id":"i1","time":"2026-11-01T10:00:00Z","ws":"ws-one","kind":"question","summary":"needs a decision`+"\x01"+`"}`+"\n"+
			`{"id":"i2","time":"2026-11-02T10:00:00Z","ws":"ws-two","kind":"done","summary":"acked already"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".foreman", "acks.jsonl"), []byte(`{"id":"i2"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(alpha, ".foreman", "ws", "ws-one", "status.json"), []byte(`{"agent_status":"working","report_status":"needs-input","summary":"halfway","updated":"2026-11-01T10:00:00Z"}`), 0o644)
	os.WriteFile(filepath.Join(alpha, ".foreman", "ws", "ws-one", "report.md"), []byte("---\nstatus: needs-input\n---\n## Summary\nhalfway\n"), 0o644)
	os.WriteFile(filepath.Join(beta, "notes.md"), []byte("beta notes\n"), 0o644)

	vp := filepath.Join(dir, "vault.json")
	os.WriteFile(vp, []byte(`[{"name":"Home Wi-Fi","kind":"password","value":"`+canary+`"}]`), 0o600)
	v, err := vault.Open(vp)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{DataDir: filepath.Join(dir, "data"), WebDir: dir, Token: "tok"}, v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddProject("Alpha", alpha); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddProject("Beta", beta); err != nil {
		t.Fatal(err)
	}
	return a, dir
}

func call(t *testing.T, a *App, agent, tool string, args map[string]any) (string, error) {
	t.Helper()
	r, err := a.run(agent)
	if err != nil {
		t.Fatal(err)
	}
	return a.CallTool(r, tool, args)
}

// leakCheck greps everything Nexus persisted plus everything the agent saw.
func leakCheck(t *testing.T, dir string, agentSaw ...string) {
	t.Helper()
	for _, s := range agentSaw {
		if strings.Contains(s, canary) {
			t.Fatalf("canary reached the agent: %q", s)
		}
	}
	filepath.Walk(filepath.Join(dir, "data"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), canary) {
			t.Fatalf("canary leaked into %s", p)
		}
		return nil
	})
}

func TestProjectsReadFromForemanLedger(t *testing.T) {
	a, _ := newTestApp(t)
	d, ok := a.detail("alpha")
	if !ok || !d.Foreman {
		t.Fatalf("alpha not read as a foreman project: %+v", d)
	}
	if len(d.Backlog) != 2 || d.Backlog[0].Done || !d.Backlog[1].Done {
		t.Fatalf("backlog: %+v", d.Backlog)
	}
	if len(d.Inbox) != 1 || d.Inbox[0].ID != "i1" {
		t.Fatalf("inbox should hold only the unacked item (and survive a raw control char): %+v", d.Inbox)
	}
	if len(d.Workstreams) != 1 || d.Workstreams[0].AgentStatus != "working" || d.Workstreams[0].Report == "" {
		t.Fatalf("workstreams: %+v", d.Workstreams)
	}
	if !strings.Contains(d.Readme, "widgets") {
		t.Fatalf("readme intro: %q", d.Readme)
	}
	needs := a.needs()
	if len(needs) != 1 || needs[0].Report != "alpha/.foreman/ws/ws-one/report.md" {
		t.Fatalf("needs: %+v", needs)
	}
	if run := a.running(); len(run) != 1 || run[0].Title != "ws-one" {
		t.Fatalf("running: %+v", run)
	}
	a.Desk.Dismiss("i1")
	if len(a.needs()) != 0 {
		t.Fatal("dismissed need still listed")
	}
}

func TestReadOnlyAndScopes(t *testing.T) {
	a, dir := newTestApp(t)
	// project agents read their own project, including bare relative paths
	if out, err := call(t, a, "p:alpha", "file_read", map[string]any{"path": "docs/plan.md"}); err != nil || !strings.Contains(out, "quick brown fox") {
		t.Fatalf("own project read: %v %q", err, out)
	}
	// …not another project
	if _, err := call(t, a, "p:alpha", "file_read", map[string]any{"path": "beta/notes.md"}); err == nil || !isDenial(err) {
		t.Fatalf("cross-project read should be denied, got %v", err)
	}
	// the home agent reads everything, even by absolute path
	if out, err := call(t, a, "home", "file_read", map[string]any{"path": filepath.Join(dir, "beta", "notes.md")}); err != nil || !strings.Contains(out, "beta notes") {
		t.Fatalf("home read: %v %q", err, out)
	}
	// nobody reads secret-looking files
	for _, ag := range []string{"home", "p:alpha"} {
		out, err := call(t, a, ag, "file_read", map[string]any{"path": "alpha/.env"})
		if err == nil || !isDenial(err) {
			t.Fatalf("%s read .env: %v %q", ag, err, out)
		}
	}
	out, _ := call(t, a, "home", "search", map[string]any{"project": "alpha", "query": "API_KEY"})
	// your repos are read-only for agents; desk/<project>/ is theirs
	if _, err := call(t, a, "p:alpha", "sheet_create", map[string]any{"path": "alpha/x.csv", "headers": []any{"a"}}); err == nil || !isDenial(err) {
		t.Fatalf("writing into the repo should be denied: %v", err)
	}
	if _, err := call(t, a, "p:alpha", "sheet_create", map[string]any{"path": "desk/alpha/x.csv", "headers": []any{"a"}}); err != nil {
		t.Fatalf("desk write: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha", "x.csv")); err == nil {
		t.Fatal("a file appeared in the project folder")
	}
	// tools are per kind: the home agent has no layout tools; task agents can't draft tasks
	if _, err := call(t, a, "home", "pane_open", map[string]any{"kind": "markdown"}); err == nil {
		t.Fatal("home agent should not have pane_open")
	}
	leakCheck(t, dir, out)
}

func TestSecureToolNeverReturnsValue(t *testing.T) {
	a, dir := newTestApp(t)
	var seen []string
	for _, ag := range []string{"home", "p:alpha"} {
		out, err := call(t, a, ag, "vault_reveal", map[string]any{"name": "Home Wi-Fi", "reason": "you asked"})
		if err != nil || !strings.HasPrefix(out, "DATA_DELIVERED_OUT_OF_BAND") {
			t.Fatalf("%s: %v %q", ag, err, out)
		}
		seen = append(seen, out)
	}
	p, _ := a.Projects.Get("alpha")
	if c := a.projectContext("alpha", ""); strings.Contains(c, "Home Wi-Fi") {
		t.Fatalf("secure pane leaked into the project context: %s", c)
	}
	for _, pn := range a.Store.Panes(p.Space) {
		if pn.Kind == "secure-view" && pn.Visibility != store.VisNone {
			t.Fatal("secure pane must be private")
		}
	}
	seen = append(seen, a.projectContext("alpha", ""), a.homeContext(""))
	leakCheck(t, dir, seen...)
}

func TestAgentNeverStealsFocusAndUndo(t *testing.T) {
	a, _ := newTestApp(t)
	p, _ := a.Projects.Get("alpha")
	mine, _ := a.openPane(p.Space, store.Pane{Kind: "markdown", Title: "mine"}, "", store.ActorUser)
	if _, err := call(t, a, "p:alpha", "pane_open", map[string]any{"kind": "markdown", "title": "agent's", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	if fp, _ := a.Store.FocusedPaneIn(p.Space); fp.ID != mine.ID {
		t.Fatalf("focus moved to %s", fp.Title)
	}
	if _, err := a.Store.UndoAgent(p.Space); err != nil {
		t.Fatal(err)
	}
	if n := len(a.Store.Panes(p.Space)); n != 1 {
		t.Fatalf("undo left %d panes", n)
	}
	// a project agent can't touch another project's panes
	b, _ := a.Projects.Get("beta")
	other, _ := a.openPane(b.Space, store.Pane{Kind: "markdown", Title: "beta's"}, "", store.ActorUser)
	if _, err := call(t, a, "p:alpha", "pane_close", map[string]any{"pane_id": other.ID}); err == nil {
		t.Fatal("closed another project's pane")
	}
}

func TestCaptureProposeFile(t *testing.T) {
	a, _ := newTestApp(t)
	caps := a.Desk.AddCaptures("call the plumber\n\nwidget idea: blue ones")
	if len(caps) != 2 {
		t.Fatalf("captures: %+v", caps)
	}
	if _, err := call(t, a, "home", "capture_propose", map[string]any{"capture_id": caps[1].ID, "project": "alpha", "kind": "note", "title": "Blue widgets", "reason": "widgets"}); err != nil {
		t.Fatal(err)
	}
	if n := a.needs(); len(n) < 1 || n[0].Source != "filing" && n[len(n)-1].Source != "filing" {
		t.Fatalf("filing need missing: %+v", n)
	}
	it, err := a.Desk.File(caps[1].ID, "alpha", "note", "Blue widgets")
	if err != nil || it.Project != "alpha" || it.Body != "widget idea: blue ones" {
		t.Fatalf("file: %v %+v", err, it)
	}
	if _, err := call(t, a, "home", "capture_propose", map[string]any{"capture_id": caps[1].ID, "project": "beta", "kind": "note"}); err == nil {
		t.Fatal("re-proposing a filed capture should fail")
	}
}

func TestTaskLifecycle(t *testing.T) {
	a, dir := newTestApp(t)
	out, err := call(t, a, "p:alpha", "task_draft", map[string]any{"title": "Summarize the plan", "brief": "Read docs/plan.md and summarize."})
	if err != nil {
		t.Fatal(err)
	}
	tk := *a.Desk.Snapshot().Tasks[0]
	if tk.Status != "draft" || !strings.Contains(out, tk.ID) {
		t.Fatalf("draft: %+v %q", tk, out)
	}
	// the project agent can't start it, or write a report
	if _, err := call(t, a, "p:alpha", "report_write", map[string]any{"content": "x"}); err == nil {
		t.Fatal("project agent has report_write")
	}
	// act as the task agent (no model: drive its tools directly)
	a.Desk.UpdateTask(tk.ID, func(t *deskTaskT) error { t.Status = "running"; return nil })
	if out, err := call(t, a, "t:"+tk.ID, "file_read", map[string]any{"path": "docs/plan.md"}); err != nil || !strings.Contains(out, "fox") {
		t.Fatalf("task read: %v", err)
	}
	if _, err := call(t, a, "t:"+tk.ID, "file_read", map[string]any{"path": "beta/notes.md"}); err == nil {
		t.Fatal("task agent read another project")
	}
	if _, err := call(t, a, "t:"+tk.ID, "report_write", map[string]any{"content": "# Summary\nA fox."}); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, a, "t:"+tk.ID, "task_finish", map[string]any{"status": "done", "summary": "summarized"}); err != nil {
		t.Fatal(err)
	}
	tk, _ = a.Desk.Task(tk.ID)
	if tk.Status != "done" || !strings.HasPrefix(tk.Report, "desk/alpha/tasks/") {
		t.Fatalf("finished task: %+v", tk)
	}
	b, err := os.ReadFile(filepath.Join(dir, "data", "desk-files", strings.TrimPrefix(tk.Report, "desk/")))
	if err != nil || !strings.Contains(string(b), "A fox") {
		t.Fatalf("report on disk: %v", err)
	}
	// A follow-up that writes again must never overwrite the report you read.
	if !strings.Contains(a.taskContext(tk), "already finished") {
		t.Fatal("follow-ups to a finished task must carry the task context")
	}
	if _, err := call(t, a, "t:"+tk.ID, "report_write", map[string]any{"content": "# Something else"}); err != nil {
		t.Fatal(err)
	}
	tk2, _ := a.Desk.Task(tk.ID)
	if tk2.Report == tk.Report || len(tk2.Reports) != 1 || tk2.Reports[0] != tk.Report {
		t.Fatalf("report not versioned: %+v", tk2)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "data", "desk-files", strings.TrimPrefix(tk.Report, "desk/"))); !strings.Contains(string(b), "A fox") {
		t.Fatal("first report was overwritten")
	}
	found := false
	for _, n := range a.needs() {
		if n.Task == tk.ID && n.Report == tk2.Report {
			found = true
		}
	}
	if !found {
		t.Fatal("finished task not in needs-you")
	}
}

func TestPluginWriteValidation(t *testing.T) {
	a, _ := newTestApp(t)
	if _, err := call(t, a, "home", "widget_write", map[string]any{"id": "Bad ID", "name": "x", "html": "<p>x</p>"}); err == nil {
		t.Fatal("bad id accepted")
	}
	if _, err := call(t, a, "home", "widget_write", map[string]any{"id": "w", "name": "x", "html": "<p>x</p>", "permissions": []any{"tool:vault_reveal"}}); err == nil {
		t.Fatal("non-plugin tool permission accepted")
	}
	if _, err := call(t, a, "home", "widget_write", map[string]any{"id": "glance", "name": "Glance", "html": "<p>x</p>", "permissions": []any{"tool:projects_summary"}}); err != nil {
		t.Fatal(err)
	}
	if w := a.Desk.Snapshot().Widgets; len(w) != 1 || w[0].Plugin != "glance" {
		t.Fatalf("widget not on the dashboard: %+v", w)
	}
}

func TestWikiWriteProposeAndBootstrap(t *testing.T) {
	a, dir := newTestApp(t)
	// empty wiki: the bootstrap says so
	if !strings.Contains(a.wikiBootstrap("alpha"), "EMPTY") {
		t.Fatal("empty wiki not flagged")
	}
	// the project agent writes; previous versions are kept
	for _, body := range []string{"# Alpha\n\nWhat this is: widgets.\n", "# Alpha\n\nWhat this is: blue widgets.\n"} {
		if _, err := call(t, a, "p:alpha", "wiki_write", map[string]any{"page": "index", "content": body, "summary": "front page", "sources": []any{"alpha/README.md"}}); err != nil {
			t.Fatal(err)
		}
	}
	if pg := a.Wiki.List("alpha"); len(pg) != 1 || len(pg[0].Meta.Sources) != 1 || pg[0].Meta.Sources[0] != "alpha/README.md" {
		t.Fatalf("sources not normalized: %+v", pg)
	}
	call(t, a, "p:alpha", "wiki_write", map[string]any{"page": "index", "content": "# Alpha\n\nWhat this is: blue widgets.\n", "summary": "front page", "sources": []any{"README.md"}})
	if pg := a.Wiki.List("alpha"); pg[0].Meta.Sources[0] != "alpha/README.md" {
		t.Fatalf("bare source path not prefixed: %+v", pg[0].Meta.Sources)
	}
	os.Chtimes(filepath.Join(dir, "alpha", "README.md"), time.Now().Add(2*time.Hour), time.Now().Add(2*time.Hour))
	if pg := a.wikiPages("alpha"); len(pg[0].Stale) != 1 {
		t.Fatalf("changed source not flagged stale: %+v", pg[0])
	}
	if b := a.wikiBootstrap("alpha"); !strings.Contains(b, "blue widgets") {
		t.Fatalf("bootstrap missing index: %s", b)
	}
	if !strings.Contains(a.projectContext("alpha", ""), "blue widgets") {
		t.Fatal("project agents don't start with the wiki index")
	}
	hist, _ := os.ReadDir(filepath.Join(dir, "data", "wiki", "alpha", ".history"))
	if len(hist) != 2 {
		t.Fatalf("expected 2 history versions, got %d", len(hist))
	}
	// agents can't write wiki files directly, and other projects' wikis are off limits
	if _, err := call(t, a, "p:alpha", "sheet_create", map[string]any{"path": "wiki/alpha/x.csv", "headers": []any{"a"}}); err == nil {
		t.Fatal("wiki root must not be writable by agents except through wiki tools")
	}
	if _, err := call(t, a, "p:beta", "wiki_read", map[string]any{"page": "index", "project": "alpha"}); err == nil {
		t.Fatal("beta's agent read alpha's wiki")
	}
	// a task agent can only propose; the proposal is a need until you resolve it
	tk := a.Desk.AddTask(deskTask("alpha", "learn", "learn things", ""))
	if _, err := call(t, a, "t:"+tk.ID, "wiki_write", map[string]any{"page": "x", "content": "# X", "summary": "x"}); err == nil {
		t.Fatal("task agent can write the wiki directly")
	}
	if _, err := call(t, a, "t:"+tk.ID, "wiki_propose", map[string]any{"page": "gotchas", "content": "# Gotchas\n\n- widgets are blue\n", "summary": "gotchas", "reason": "learned in task"}); err != nil {
		t.Fatal(err)
	}
	props := a.pendingProposals("alpha")
	found := false
	for _, n := range a.needs() {
		found = found || n.Source == "wiki"
	}
	if len(props) != 1 || !found {
		t.Fatalf("proposal not pending or not in needs: %+v", props)
	}
	if _, err := a.Wiki.Resolve("alpha", props[0].ID, true); err != nil {
		t.Fatal(err)
	}
	if c, err := a.Wiki.Read("alpha", "gotchas"); err != nil || !strings.Contains(c, "widgets are blue") {
		t.Fatalf("accepted proposal not written: %v", err)
	}
	// plan
	if _, err := call(t, a, "t:"+tk.ID, "plan_set", map[string]any{"steps": []any{"scope", "read", "write"}}); err != nil {
		t.Fatal(err)
	}
	call(t, a, "t:"+tk.ID, "plan_update", map[string]any{"step": float64(2), "status": "done"})
	if got, _ := a.Desk.Task(tk.ID); len(got.Plan) != 3 || got.Plan[1].Status != "done" {
		t.Fatalf("plan: %+v", got.Plan)
	}
}

func TestAddonGatesTools(t *testing.T) {
	a, _ := newTestApp(t)
	r, _ := a.run("p:beta") // beta isn't a git repo: code graph defaults off
	has := func() bool {
		for _, tl := range a.toolsFor(r) {
			if tl.Name == "code_explore" {
				return true
			}
		}
		return false
	}
	if has() {
		t.Fatal("code tools offered with the add-on off")
	}
	if codegraphBin() == "" {
		t.Skip("codegraph not installed")
	}
	a.setAddon("beta", "codegraph", true)
	r, _ = a.run("p:beta")
	if !has() {
		t.Fatal("code tools missing with the add-on on")
	}
}
