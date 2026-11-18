// Package app wires the desk prototype: the websocket hub for the shell,
// the agents (home, one per project, and dispatched task agents), the tools
// they call, the plugin bridge, and the read-only view of your projects.
// Everything an actor does goes through here so it can be logged and policed.
package app

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/broker"
	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
	"github.com/pvjammer/nexus/prototypes/desk/internal/plugins"
	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
	"github.com/pvjammer/nexus/prototypes/desk/internal/review"
	"github.com/pvjammer/nexus/prototypes/desk/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/desk/internal/store"
	"github.com/pvjammer/nexus/prototypes/desk/internal/term"
	"github.com/pvjammer/nexus/prototypes/desk/internal/vault"
	"github.com/pvjammer/nexus/prototypes/desk/internal/wiki"
)

type Config struct {
	DataDir   string
	WebDir    string
	SkillsDir string   // agent skills (default "skills")
	Token     string   // the shell's websocket token
	ScanRoots []string // where onboarding looks for projects
	Agents    AgentFactory
	TestHooks bool // e2e only: lets a test act as an agent without a model
	// Foreman: read foreman ledgers (workstreams, backlog, inbox) by default
	// in projects that have one. Off by default: Nexus doesn't assume any
	// particular tool; a project can still switch the add-on on.
	Foreman bool
}

type App struct {
	cfg      Config
	Store    *store.Store
	Log      *actionlog.Log
	Roots    *files.Roots
	Sheets   *sheets.Store
	Review   *review.Store
	Vault    *vault.Vault
	Broker   *broker.Broker
	Plugins  *plugins.Store
	Terms    *term.Manager
	Projects *projects.Registry
	Desk     *desk.Store
	Wiki     *wiki.Store
	cg       *codeGraph

	hubMu   sync.Mutex
	clients map[*client]bool

	runMu   sync.Mutex
	runs    map[string]*Run // by agent id: "home", "p:<project>", "t:<task>"
	byToken map[string]*Run

	// deskBcast serializes desk broadcasts. Each store change broadcasts
	// from its own goroutine; without this an older snapshot could reach the
	// shell after a newer one and leave it showing stale state.
	deskBcast sync.Mutex

	detMu   sync.Mutex
	details map[string]projects.Detail
	detHash string

	pendMu    sync.Mutex
	pending   map[string]chan bool // permission prompts
	pluginCtx map[string]string    // paneID -> context text published by plugin

	watcher *fsnotify.Watcher
	watched map[string]bool
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

func New(cfg Config, v *vault.Vault) (*App, error) {
	st, err := store.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	pl, err := plugins.Open(filepath.Join(cfg.DataDir, "plugins"))
	if err != nil {
		return nil, err
	}
	reg, err := projects.OpenRegistry(filepath.Join(cfg.DataDir, "projects.json"))
	if err != nil {
		return nil, err
	}
	dk, err := desk.Open(filepath.Join(cfg.DataDir, "desk.json"))
	if err != nil {
		return nil, err
	}
	roots := files.New(filepath.Join(cfg.DataDir, "meta"))
	if err := roots.Add("desk", filepath.Join(cfg.DataDir, "desk-files"), false); err != nil {
		return nil, err
	}
	wk := wiki.New(filepath.Join(cfg.DataDir, "wiki"))
	// Agents change the wiki only through wiki tools (versioned, proposals).
	if err := roots.Add("wiki", wk.Root(), true); err != nil {
		return nil, err
	}
	a := &App{
		cfg:       cfg,
		Store:     st,
		Log:       actionlog.Open(filepath.Join(cfg.DataDir, "actions.jsonl")),
		Roots:     roots,
		Sheets:    sheets.New(roots),
		Review:    review.New(roots),
		Vault:     v,
		Broker:    broker.New(),
		Plugins:   pl,
		Terms:     term.NewManager(),
		Projects:  reg,
		Desk:      dk,
		Wiki:      wk,
		cg:        newCodeGraph(),
		clients:   map[*client]bool{},
		runs:      map[string]*Run{},
		byToken:   map[string]*Run{},
		details:   map[string]projects.Detail{},
		pending:   map[string]chan bool{},
		pluginCtx: map[string]string{},
		watched:   map[string]bool{},
	}
	for _, p := range reg.List() {
		a.mountProject(p)
	}
	a.dropDeadPanes()
	st.OnChange(a.broadcastState)
	dk.OnChange(a.broadcastDesk)
	a.Log.OnAdd(func(e actionlog.Entry) { a.broadcast(map[string]any{"t": "log", "entry": e}) })
	a.Terms.OnOut = func(id string, data []byte) {
		a.broadcast(map[string]any{"t": "termOut", "term": id, "data": string(data)})
	}
	a.Terms.OnExit = func(id string) { a.broadcast(map[string]any{"t": "termExit", "term": id}) }
	if w, err := fsnotify.NewWatcher(); err == nil {
		a.watcher = w
		go a.watchLoop()
	}
	a.loadChats()
	a.refreshDetails()
	go a.pollProjects()
	go a.codegraphLoop()
	return a, nil
}

// mountProject makes a project's folder a file root (read-only for agents)
// and makes sure it has a layout space.
func (a *App) mountProject(p projects.Project) {
	if err := a.Roots.Add(p.ID, p.Path, true); err != nil {
		log.Printf("project %s: %v", p.ID, err)
	}
	if !a.Store.HasSpace(p.Space) {
		sp := a.Store.NewSpace(p.Name)
		a.Projects.Update(p.ID, func(q *projects.Project) { q.Space = sp })
	}
}

// dropDeadPanes removes panes whose live resources don't survive a restart.
func (a *App) dropDeadPanes() {
	snap := a.Store.Snapshot()
	for id, p := range snap.Panes {
		switch p.Kind {
		case "secure-view", "secret-input":
			a.Store.ClosePane(id, store.ActorUser)
		case "plugin":
			if _, ok := a.Plugins.Get(str(p.Props["plugin"])); !ok {
				a.Store.ClosePane(id, store.ActorUser)
			}
		case "task":
			if _, ok := a.Desk.Task(str(p.Props["task"])); !ok {
				a.Store.ClosePane(id, store.ActorUser)
			}
		case "terminal":
			a.Store.UpdatePane(id, store.ActorUser, func(p *store.Pane) error { delete(p.Props, "term"); return nil })
		}
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// projectBySpace maps a layout space back to its project.
func (a *App) projectBySpace(space string) (projects.Project, bool) {
	for _, p := range a.Projects.List() {
		if p.Space == space {
			return p, true
		}
	}
	return projects.Project{}, false
}

// ---- project details (polled; your folders are only read) ----

func (a *App) pollProjects() {
	t := time.NewTicker(8 * time.Second)
	for range t.C {
		a.refreshDetails()
	}
}

func (a *App) refreshDetails() {
	list := a.Projects.List()
	next := map[string]projects.Detail{}
	for _, p := range list {
		d := projects.Read(p)
		if !a.foremanOn(p.Addons, d.Foreman) {
			// Ledger view off: keep only that a ledger exists (for the add-on list).
			d.Workstreams, d.Backlog, d.Inbox, d.Events, d.Archived = nil, nil, nil, nil, 0
			d.LastActivity = time.Time{}
			if d.Git != nil && len(d.Git.Commits) > 0 {
				d.LastActivity = d.Git.Commits[0].When
			}
		}
		next[p.ID] = d
	}
	b, _ := json.Marshal(next)
	h := string(b)
	a.detMu.Lock()
	changed := h != a.detHash
	a.details, a.detHash = next, h
	a.detMu.Unlock()
	if changed {
		a.broadcastDesk()
	}
}

// foremanOn: the project's own switch if set, else the core's default.
func (a *App) foremanOn(addons map[string]bool, has bool) bool {
	if v, set := addons["foreman"]; set {
		return v && has
	}
	return a.cfg.Foreman && has
}

func (a *App) detail(id string) (projects.Detail, bool) {
	a.detMu.Lock()
	defer a.detMu.Unlock()
	d, ok := a.details[id]
	return d, ok
}

func (a *App) allDetails() []projects.Detail {
	a.detMu.Lock()
	defer a.detMu.Unlock()
	var out []projects.Detail
	for _, p := range a.Projects.List() {
		if d, ok := a.details[p.ID]; ok {
			out = append(out, d)
		}
	}
	return out
}

// ---- hub ----

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
	o := r.Header.Get("Origin")
	return o == "" || strings.HasPrefix(o, "http://127.0.0.1") || strings.HasPrefix(o, "http://localhost") || strings.HasPrefix(o, "wails://") || o == "null"
}}

func (a *App) serveWS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != a.cfg.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 1024)}
	a.hubMu.Lock()
	a.clients[c] = true
	a.hubMu.Unlock()
	go func() {
		for b := range c.send {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if conn.WriteMessage(websocket.TextMessage, b) != nil {
				break
			}
		}
		conn.Close()
	}()
	a.sendTo(c, a.stateMsg())
	a.sendTo(c, a.deskMsg())
	for _, run := range a.allRuns() {
		msgs, busy := run.history()
		a.sendTo(c, map[string]any{"t": "chatHistory", "agent": run.ID, "messages": msgs, "busy": busy})
	}
	a.sendTo(c, map[string]any{"t": "logHistory", "entries": a.Log.Recent(300)})
	// Re-deliver secure values to a reconnecting shell (memory only).
	for id, p := range a.Store.Snapshot().Panes {
		if p.Kind == "secure-view" {
			if v, ok := a.Vault.Delivered(id); ok {
				a.sendTo(c, map[string]any{"t": "secureValue", "pane": id, "value": v})
			}
		}
	}
	conn.SetReadLimit(4 << 20)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		a.handleClient(c, m, data)
	}
	a.hubMu.Lock()
	delete(a.clients, c)
	a.hubMu.Unlock()
	close(c.send)
}

func (a *App) sendTo(c *client, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	select {
	case c.send <- b:
	default:
		log.Printf("hub: client send buffer full, dropping")
	}
}

func (a *App) broadcast(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	for c := range a.clients {
		select {
		case c.send <- b:
		default:
		}
	}
}

func (a *App) stateMsg() map[string]any {
	st := a.Store.Snapshot()
	undo := map[string]any{}
	for _, sp := range st.Spaces {
		if d, l := a.Store.UndoDepth(sp.ID); d > 0 {
			undo[sp.ID] = map[string]any{"depth": d, "label": l}
		}
	}
	var pls []map[string]any
	for _, p := range a.Plugins.List() {
		pls = append(pls, map[string]any{
			"id": p.ID, "name": p.Name, "description": p.Description, "permissions": p.Permissions,
			"grants": p.Grants, "kept": p.Kept, "version": p.Version, "hash": p.Hash,
		})
	}
	return map[string]any{
		"t": "state", "state": st, "undo": undo, "plugins": pls, "roots": a.Roots.Names(),
		"agent": a.agentName(),
	}
}

func (a *App) broadcastState() { a.broadcast(a.stateMsg()) }

func (a *App) toast(text, level string) {
	a.broadcast(map[string]any{"t": "toast", "text": text, "level": level})
}

// ---- file watching (external edits refresh open panes) ----

func (a *App) watchFile(path string) {
	abs, err := a.Roots.Resolve(path)
	if err != nil || a.watcher == nil {
		return
	}
	dir := filepath.Dir(abs)
	a.hubMu.Lock()
	defer a.hubMu.Unlock()
	if !a.watched[dir] {
		if a.watcher.Add(dir) == nil {
			a.watched[dir] = true
		}
	}
}

func (a *App) watchLoop() {
	var mu sync.Mutex
	timers := map[string]*time.Timer{}
	for ev := range a.watcher.Events {
		if strings.Contains(ev.Name, ".nexus-tmp") {
			continue
		}
		name := ev.Name
		mu.Lock()
		if t := timers[name]; t != nil {
			t.Stop()
		}
		timers[name] = time.AfterFunc(150*time.Millisecond, func() {
			for _, p := range a.appPaths(name) {
				a.broadcast(map[string]any{"t": "fileChanged", "abs": name, "path": p})
			}
		})
		mu.Unlock()
	}
}

// appPaths maps an absolute path back to "<root>/<rel>" (several roots can
// contain the same file, e.g. a repo nested in another).
func (a *App) appPaths(abs string) []string {
	var out []string
	for _, n := range a.Roots.Names() {
		dir, _ := a.Roots.Dir(n)
		if strings.HasPrefix(abs, dir+string(filepath.Separator)) {
			rel, _ := filepath.Rel(dir, abs)
			out = append(out, n+"/"+filepath.ToSlash(rel))
		}
	}
	return out
}

// ---- HTTP ----

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", a.serveWS)
	mux.HandleFunc("/agent/tools", a.serveTools)
	mux.HandleFunc("/agent/call", a.serveCall)
	mux.HandleFunc("/raw/", a.serveRaw)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	if a.cfg.TestHooks {
		// Hands a test the token of an agent (home, p:<id>, t:<id>) so it can
		// call tools as that agent. Requires the shell token. Off by default.
		mux.HandleFunc("/test/agent-token", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("token") != a.cfg.Token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			run, err := a.run(r.URL.Query().Get("id"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Write([]byte(run.Token))
		})
	}
	web := http.FileServer(http.Dir(a.cfg.WebDir))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		web.ServeHTTP(w, r)
	}))
	return mux
}

// serveRaw serves a file for sandboxed HTML review (token required).
func (a *App) serveRaw(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != a.cfg.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/raw/")
	abs, err := a.Roots.Resolve(p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if files.LooksSecret(abs) {
		http.Error(w, "looks like a secret file", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	http.ServeFile(w, r, abs)
}

func (a *App) agentName() string {
	if a.cfg.Agents == nil {
		return "no agent"
	}
	return a.cfg.Agents.Name()
}
