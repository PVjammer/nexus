// Package app wires the prototype core: the websocket hub for the shell, the
// agent loop, the tools the agent calls, and the plugin bridge. Everything an
// actor does goes through here so it can be logged and policed.
package app

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"

	"github.com/pvjammer/nexus/prototypes/ux/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/ux/internal/agent"
	"github.com/pvjammer/nexus/prototypes/ux/internal/broker"
	"github.com/pvjammer/nexus/prototypes/ux/internal/files"
	"github.com/pvjammer/nexus/prototypes/ux/internal/plugins"
	"github.com/pvjammer/nexus/prototypes/ux/internal/review"
	"github.com/pvjammer/nexus/prototypes/ux/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/ux/internal/store"
	"github.com/pvjammer/nexus/prototypes/ux/internal/term"
	"github.com/pvjammer/nexus/prototypes/ux/internal/vault"
)

type Config struct {
	DataDir string
	WebDir  string
	Token   string // shared secret for the agent tool endpoint
	TermDir string
}

type App struct {
	cfg     Config
	Store   *store.Store
	Log     *actionlog.Log
	Roots   *files.Roots
	Sheets  *sheets.Store
	Review  *review.Store
	Vault   *vault.Vault
	Broker  *broker.Broker
	Plugins *plugins.Store
	Terms   *term.Manager
	Agent   agent.Backend

	hubMu   sync.Mutex
	clients map[*client]bool

	chatMu  sync.Mutex
	chat    []ChatMsg
	busy    bool
	turnSeq int

	pendMu    sync.Mutex
	pending   map[string]chan bool // permission prompts
	pluginCtx map[string]string    // paneID -> context text published by plugin

	watcher *fsnotify.Watcher
	watched map[string]bool
}

type ChatMsg struct {
	ID     int    `json:"id"`
	Role   string `json:"role"` // you | agent | tool | system | thinking
	Text   string `json:"text"`
	Tool   string `json:"tool,omitempty"`
	CallID string `json:"callId,omitempty"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
	Error  bool   `json:"error,omitempty"`
	Done   bool   `json:"done,omitempty"`
	TS     int64  `json:"ts"`
}

type client struct {
	conn *websocket.Conn
	send chan []byte
}

func New(cfg Config, roots *files.Roots, v *vault.Vault) (*App, error) {
	st, err := store.Open(filepath.Join(cfg.DataDir, "state.json"))
	if err != nil {
		return nil, err
	}
	pl, err := plugins.Open(filepath.Join(cfg.DataDir, "plugins"))
	if err != nil {
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
		clients:   map[*client]bool{},
		pending:   map[string]chan bool{},
		pluginCtx: map[string]string{},
		watched:   map[string]bool{},
	}
	a.dropDeadPanes() // panes whose live resources don't survive a restart
	st.OnChange(a.broadcastState)
	a.Log.OnAdd(func(e actionlog.Entry) { a.broadcast(map[string]any{"t": "log", "entry": e}) })
	a.Terms.OnOut = func(id string, data []byte) {
		a.broadcast(map[string]any{"t": "termOut", "term": id, "data": string(data)})
	}
	a.Terms.OnExit = func(id string) { a.broadcast(map[string]any{"t": "termExit", "term": id}) }
	if w, err := fsnotify.NewWatcher(); err == nil {
		a.watcher = w
		go a.watchLoop()
	}
	return a, nil
}

// dropDeadPanes removes secure panes and drafts left over from a previous run:
// delivered values and handles live in memory only.
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
	c := &client{conn: conn, send: make(chan []byte, 512)}
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
	a.chatMu.Lock()
	hist := append([]ChatMsg(nil), a.chat...)
	busy := a.busy
	a.chatMu.Unlock()
	a.sendTo(c, map[string]any{"t": "chatHistory", "messages": hist, "busy": busy})
	a.sendTo(c, map[string]any{"t": "logHistory", "entries": a.Log.Recent(200)})
	// Re-deliver secure values to a reconnecting shell (memory only).
	for _, p := range a.Store.Panes("") {
		if p.Kind == "secure-view" {
			if v, ok := a.Vault.Delivered(p.ID); ok {
				a.sendTo(c, map[string]any{"t": "secureValue", "pane": p.ID, "value": v})
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
	depth, label := a.Store.UndoDepth(st.ActiveSpace)
	var pls []map[string]any
	for _, p := range a.Plugins.List() {
		pls = append(pls, map[string]any{
			"id": p.ID, "name": p.Name, "description": p.Description, "permissions": p.Permissions,
			"grants": p.Grants, "kept": p.Kept, "version": p.Version, "hash": p.Hash,
		})
	}
	name := "no agent"
	if a.Agent != nil {
		name = a.Agent.Name()
	}
	return map[string]any{
		"t": "state", "state": st, "undo": map[string]any{"depth": depth, "label": label},
		"plugins": pls, "roots": a.Roots.Names(), "agent": name,
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
			a.broadcast(map[string]any{"t": "fileChanged", "abs": name, "path": a.appPath(name)})
		})
		mu.Unlock()
	}
}

// appPath maps an absolute path back to "<root>/<rel>" (best effort).
func (a *App) appPath(abs string) string {
	for _, n := range a.Roots.Names() {
		dir, _ := a.Roots.Resolve(n)
		if strings.HasPrefix(abs, dir+string(filepath.Separator)) {
			rel, _ := filepath.Rel(dir, abs)
			return n + "/" + filepath.ToSlash(rel)
		}
	}
	return ""
}

// ---- HTTP ----

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", a.serveWS)
	mux.HandleFunc("/agent/tools", a.serveTools)
	mux.HandleFunc("/agent/call", a.serveCall)
	mux.HandleFunc("/raw/", a.serveRaw)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
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
	// Reviewed HTML is untrusted: default-deny CSP, sandbox, no scripts.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	http.ServeFile(w, r, abs)
}

// Start launches the agent backend and pumps its events into chat.
func (a *App) StartAgent(ctx context.Context, b agent.Backend) error {
	a.Agent = b
	if err := b.Start(ctx); err != nil {
		return err
	}
	go a.pumpAgent()
	a.broadcastState()
	return nil
}
