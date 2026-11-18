package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/agent"
)

// Agent kinds. Each kind gets its own tool set and instructions.
const (
	KindHome    = "home"    // global: organizes, files, retrieves; not how you work in a project
	KindProject = "project" // pairs with you inside one project
	KindTask    = "task"    // ephemeral: works one brief, returns a report
	// KindCustomizer is the customizer (customizer.go): changes how Nexus
	// looks, on a draft you try before keeping.
)

// RunSpec is what an agent factory needs to start one agent loop.
type RunSpec struct {
	ID      string // "home", "p:<project>", "t:<task>"
	Kind    string
	Token   string // this agent's own token for the tool endpoint
	WorkDir string
	// Instructions is extra system-prompt text for this agent: the skills
	// for its kind, rendered with text generated from the code.
	Instructions string
	// Fresh: start a new model session instead of resuming the last one
	// (a new episode). The factory keeps the old one on disk.
	Fresh bool
}

// AgentFactory starts agent loops. pi is the prototype implementation; a
// custom loop or an external harness would be another factory.
type AgentFactory interface {
	Start(ctx context.Context, spec RunSpec) (agent.Backend, error)
	Name() string
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

// Run is one agent: its loop, its transcript and its identity.
type Run struct {
	ID      string
	Kind    string
	Project string // project id (project and task agents)
	Task    string // task id (task agents)
	Item    string // work item ref (workstream agents)
	Title   string // the work item's title (workstream agents)
	Token   string

	a       *App
	startMu sync.Mutex
	be      agent.Backend

	mu     sync.Mutex
	chat   []ChatMsg
	busy   bool
	seq    int
	closed bool

	finishing bool // task agent called task_finish; stop it when the turn ends

	// asked: the agent called ask_user this turn. One question per turn: the
	// user answers it before anything else happens (see CallTool).
	asked string
	// batches: proposal batches this turn, by project and item, so several
	// work_propose calls in one turn make one checklist.
	batches map[string]string

	draftChars int       // size of the tool call the model is still writing
	draftSent  time.Time // last progress broadcast

	saveT *time.Timer // debounced transcript save

	// mark: what was true when its last turn ended (work and the repo), for
	// the "since your last turn" note (changes.go).
	mark *turnMark
	// queue: messages from other agents (or hand-offs) that wait for this
	// agent's turn to end; the user can interrupt instead.
	queue []Queued
	// waiters are closed when the current turn ends (ws_ask waits on one).
	waiters []chan struct{}
	// wantEnd: the agent said this episode is done (episode_close); fresh:
	// the next start begins a new model session (episodes.go).
	wantEnd string
	fresh   bool
}

func newToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// run returns (creating if needed) the agent with this id. It does not
// start the loop; that happens on the first prompt.
func (a *App) run(id string) (*Run, error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if r := a.runs[id]; r != nil && !r.closed {
		return r, nil
	}
	r := &Run{ID: id, Token: newToken(), a: a}
	switch {
	case id == "home":
		r.Kind = KindHome
	case id == customizerID:
		r.Kind = KindCustomizer
	case strings.HasPrefix(id, "p:"):
		r.Kind, r.Project = KindProject, id[2:]
		if _, ok := a.Projects.Get(r.Project); !ok {
			return nil, fmt.Errorf("no project %q", r.Project)
		}
	case strings.HasPrefix(id, "w:"):
		w, err := a.checkItemRef(id[2:])
		if err != nil || w.Ref != id[2:] {
			return nil, fmt.Errorf("no work item %q", id[2:])
		}
		r.Kind, r.Project, r.Item, r.Title = KindWorkstream, w.Project, w.Ref, w.Title
	case strings.HasPrefix(id, "ask:"):
		r.Kind = KindAsk
	case strings.HasPrefix(id, "t:"):
		t, ok := a.Desk.Task(id[2:])
		if !ok {
			return nil, fmt.Errorf("no task %q", id[2:])
		}
		r.Kind, r.Task, r.Project = KindTask, t.ID, t.Project
	default:
		return nil, fmt.Errorf("bad agent id %q", id)
	}
	if old := a.runs[id]; old != nil {
		delete(a.byToken, old.Token)
		old.mu.Lock()
		r.chat, r.seq = old.chat, old.seq // keep the transcript visible
		old.mu.Unlock()
	} else {
		r.chat, r.seq = a.loadChat(id)
	}
	a.runs[id] = r
	a.byToken[r.Token] = r
	return r, nil
}

func (a *App) runByToken(tok string) *Run {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if r := a.byToken[tok]; r != nil && !r.closed {
		return r
	}
	return nil
}

func (a *App) existingRun(id string) *Run {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	return a.runs[id]
}

func (a *App) allRuns() []*Run {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	var out []*Run
	for _, r := range a.runs {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Run) label() string {
	switch r.Kind {
	case KindHome:
		return "home agent"
	case KindProject:
		return "project agent (" + r.Project + ")"
	case KindCustomizer:
		return CustomizerName
	case KindWorkstream:
		return "workstream agent (" + truncate(r.Title, 40) + ")"
	case KindAsk:
		return "reader"
	default:
		return "task agent (" + r.Task + ")"
	}
}

func (r *Run) ensureStarted() error {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	if r.be != nil {
		return nil
	}
	if r.a.cfg.Agents == nil {
		return fmt.Errorf("no agent loop configured (core started with -agent none)")
	}
	wd := r.a.cfg.DataDir
	if p, ok := r.a.Projects.Get(r.Project); ok {
		wd = p.Path
	}
	be, err := r.a.cfg.Agents.Start(context.Background(), RunSpec{ID: r.ID, Kind: r.Kind, Token: r.Token, WorkDir: wd, Instructions: r.a.instructionsFor(r.Kind), Fresh: r.fresh})
	if err != nil {
		return err
	}
	r.fresh = false
	r.be = be
	go r.pump()
	return nil
}

func (r *Run) history() ([]ChatMsg, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ChatMsg(nil), r.chat...), r.busy
}

func (r *Run) add(m ChatMsg) ChatMsg {
	r.mu.Lock()
	r.seq++
	m.ID = r.seq
	m.TS = time.Now().UnixMilli()
	r.chat = append(r.chat, m)
	if len(r.chat) > 400 {
		r.chat = r.chat[len(r.chat)-400:]
	}
	r.mu.Unlock()
	if r.Kind == KindAsk { // a throwaway reader: nobody sees it, nothing is kept
		return m
	}
	r.a.broadcast(map[string]any{"t": "chat", "agent": r.ID, "msg": m})
	r.persistSoon()
	return m
}

func (r *Run) appendStream(role, text string) {
	r.mu.Lock()
	n := len(r.chat)
	if n > 0 && r.chat[n-1].Role == role && !r.chat[n-1].Done {
		r.chat[n-1].Text += text
		m := r.chat[n-1]
		r.mu.Unlock()
		r.a.broadcast(map[string]any{"t": "chatDelta", "agent": r.ID, "id": m.ID, "text": text})
		return
	}
	r.mu.Unlock()
	r.add(ChatMsg{Role: role, Text: text})
}

func (r *Run) closeStream() {
	r.mu.Lock()
	if n := len(r.chat); n > 0 {
		r.chat[n-1].Done = true
	}
	r.mu.Unlock()
	r.persistSoon()
}

// ---- transcripts on disk ----
//
// Transcripts live in the data directory (chats/<agent>.json), so every
// conversation is still there after a restart, whether or not a chat widget
// shows it. The model's own context is the loop's business (pi sessions).

type chatFile struct {
	ID       string    `json:"id"`
	Seq      int       `json:"seq"`
	Messages []ChatMsg `json:"messages"`
}

func (a *App) chatPath(id string) string {
	safe := strings.NewReplacer(":", "_", "/", "_", "\\", "_").Replace(id)
	return filepath.Join(a.cfg.DataDir, "chats", safe+".json")
}

func (a *App) loadChat(id string) ([]ChatMsg, int) {
	b, err := os.ReadFile(a.chatPath(id))
	if err != nil {
		return nil, 0
	}
	var f chatFile
	if json.Unmarshal(b, &f) != nil || f.ID != id {
		return nil, 0
	}
	for i := range f.Messages {
		f.Messages[i].Done = true // nothing is mid-stream after a restart
	}
	return f.Messages, f.Seq
}

// loadChats brings back every saved conversation at startup, so the shell
// can list and reopen them.
func (a *App) loadChats() {
	paths, _ := filepath.Glob(filepath.Join(a.cfg.DataDir, "chats", "*.json"))
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f chatFile
		if json.Unmarshal(b, &f) != nil || f.ID == "" {
			continue
		}
		a.run(f.ID) // the agent (not its loop) with its transcript; gone projects and tasks are skipped
	}
}

func (r *Run) persistSoon() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveT != nil {
		r.saveT.Stop()
	}
	r.saveT = time.AfterFunc(400*time.Millisecond, r.persist)
}

func (r *Run) persist() {
	if r.Kind == KindAsk {
		return
	}
	r.mu.Lock()
	f := chatFile{ID: r.ID, Seq: r.seq, Messages: append([]ChatMsg(nil), r.chat...)}
	r.mu.Unlock()
	if len(f.Messages) == 0 {
		return
	}
	p := r.a.chatPath(r.ID)
	if os.MkdirAll(filepath.Dir(p), 0o700) != nil {
		return
	}
	b, _ := json.Marshal(f)
	if os.WriteFile(p+".tmp", b, 0o600) == nil {
		os.Rename(p+".tmp", p)
	}
}

func (r *Run) setBusy(b bool) {
	r.mu.Lock()
	r.busy = b
	r.mu.Unlock()
	r.a.broadcast(map[string]any{"t": "busy", "agent": r.ID, "busy": b})
	if r.Kind == KindWorkstream { // it shows in Running and in its project's workstream bar
		go r.a.broadcastDesk()
	}
}

func (r *Run) lastAgentText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.chat) - 1; i >= 0; i-- {
		if r.chat[i].Role == "agent" {
			return r.chat[i].Text
		}
	}
	return ""
}

// Say sends a message to the agent. shown is what appears in the transcript
// as yours; context is prepended for the model only.
func (r *Run) Say(shown, context string) error { return r.sayAs("you", shown, context) }

// sayAs is Say with the transcript role: "you" for what you typed, "app"
// for prompts the app generated on your behalf.
func (r *Run) sayAs(role, shown, context string) error {
	shown = strings.TrimSpace(shown)
	if shown == "" {
		return nil
	}
	r.closeStream()
	r.newTurn()
	_, wasAsked := r.pendingQuestion()
	r.add(ChatMsg{Role: role, Text: shown})
	if wasAsked {
		r.a.broadcastDesk() // the question leaves Needs you
	}
	if err := r.ensureStarted(); err != nil {
		r.add(ChatMsg{Role: "system", Text: err.Error(), Error: true})
		return err
	}
	msg := shown
	if context != "" {
		msg = context + "\n\n" + shown
	}
	r.setBusy(true)
	if err := r.be.Prompt(context2(), msg); err != nil {
		r.add(ChatMsg{Role: "system", Text: "agent error: " + err.Error(), Error: true})
		r.setBusy(false)
		return err
	}
	return nil
}

// pendingQuestion is the agent's unanswered ask_user message, if any.
func (r *Run) pendingQuestion() (ChatMsg, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ask ChatMsg
	found := false
	for i := len(r.chat) - 1; i >= 0 && r.chat[i].Role != "you" && r.chat[i].Role != "app"; i-- {
		if m := r.chat[i]; m.Role == "tool" && m.Tool == "ask_user" && !m.Error {
			ask, found = m, true
		}
	}
	return ask, found
}

// newTurn clears what was true only for the turn that ended.
func (r *Run) newTurn() {
	r.mu.Lock()
	r.asked = ""
	r.batches = nil
	r.mu.Unlock()
}

func context2() context.Context { return context.Background() }

func (r *Run) Abort() {
	if r.be != nil {
		r.be.Abort()
	}
}

// Close stops the agent loop (task agents, project removal).
func (r *Run) Close() {
	r.a.runMu.Lock()
	r.closed = true
	delete(r.a.byToken, r.Token)
	r.a.runMu.Unlock()
	if r.be != nil {
		go r.be.Close()
	}
	r.setBusy(false)
}

func (r *Run) pump() {
	for ev := range r.be.Events() {
		switch ev.Type {
		case agent.EventDelta:
			r.appendStream("agent", ev.Text)
		case agent.EventThinking:
			r.appendStream("thinking", ev.Text)
		case agent.EventToolDraft:
			// Big tool calls (a whole HTML page) can take minutes on a local
			// model. Show progress so "working…" isn't a black box.
			if ev.Text == "" {
				r.draftChars = 0
			}
			r.draftChars += len(ev.Text)
			if time.Since(r.draftSent) > 700*time.Millisecond {
				r.draftSent = time.Now()
				r.a.broadcast(map[string]any{"t": "drafting", "agent": r.ID, "chars": r.draftChars})
			}
		case agent.EventToolStart:
			if r.draftChars > 0 {
				r.draftChars = 0
				r.a.broadcast(map[string]any{"t": "drafting", "agent": r.ID, "chars": 0})
			}
			r.closeStream()
			args := ev.Args
			if ev.Tool != "ask_user" { // the question card parses ask_user's args: keep them whole (the tool caps their size)
				args = truncate(args, 400)
			}
			r.add(ChatMsg{Role: "tool", Tool: ev.Tool, CallID: ev.CallID, Args: args})
		case agent.EventToolEnd:
			r.closeStream()
			r.mu.Lock()
			var upd *ChatMsg
			for i := len(r.chat) - 1; i >= 0; i-- {
				if r.chat[i].Role == "tool" && r.chat[i].CallID == ev.CallID {
					r.chat[i].Result = truncate(ev.Result, 600)
					r.chat[i].Error = ev.IsError
					r.chat[i].Done = true
					c := r.chat[i]
					upd = &c
					break
				}
			}
			r.mu.Unlock()
			if upd != nil {
				r.a.broadcast(map[string]any{"t": "chatUpdate", "agent": r.ID, "msg": upd})
			}
			r.persistSoon()
		case agent.EventTurnEnd:
			r.closeStream()
			r.newTurn()
			r.setBusy(false)
			r.a.broadcast(map[string]any{"t": "turnEnd", "agent": r.ID})
			if r.Kind == KindTask {
				r.a.taskTurnEnded(r)
			}
			if r.Kind == KindCustomizer {
				r.a.customizerSettle()
			}
			r.turnEnded()
		case agent.EventError:
			r.closeStream()
			r.add(ChatMsg{Role: "system", Text: ev.Text, Error: true})
			r.setBusy(false)
			if r.Kind == KindTask {
				r.a.taskFailed(r, ev.Text)
			}
			if r.Kind == KindCustomizer {
				r.a.broadcastCustomizer() // the draft stays; Keep or Roll back
			}
			r.turnEnded()
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
