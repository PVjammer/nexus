package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
)

// RunSpec is what an agent factory needs to start one agent loop.
type RunSpec struct {
	ID      string // "home", "p:<project>", "t:<task>"
	Kind    string
	Token   string // this agent's own token for the tool endpoint
	WorkDir string
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

	draftChars int       // size of the tool call the model is still writing
	draftSent  time.Time // last progress broadcast
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
	case strings.HasPrefix(id, "p:"):
		r.Kind, r.Project = KindProject, id[2:]
		if _, ok := a.Projects.Get(r.Project); !ok {
			return nil, fmt.Errorf("no project %q", r.Project)
		}
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
	be, err := r.a.cfg.Agents.Start(context.Background(), RunSpec{ID: r.ID, Kind: r.Kind, Token: r.Token, WorkDir: wd})
	if err != nil {
		return err
	}
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
	r.a.broadcast(map[string]any{"t": "chat", "agent": r.ID, "msg": m})
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
}

func (r *Run) setBusy(b bool) {
	r.mu.Lock()
	r.busy = b
	r.mu.Unlock()
	r.a.broadcast(map[string]any{"t": "busy", "agent": r.ID, "busy": b})
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
	r.add(ChatMsg{Role: role, Text: shown})
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
			r.add(ChatMsg{Role: "tool", Tool: ev.Tool, CallID: ev.CallID, Args: truncate(ev.Args, 400)})
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
		case agent.EventTurnEnd:
			r.closeStream()
			r.setBusy(false)
			r.a.broadcast(map[string]any{"t": "turnEnd", "agent": r.ID})
			if r.Kind == KindTask {
				r.a.taskTurnEnded(r)
			}
		case agent.EventError:
			r.closeStream()
			r.add(ChatMsg{Role: "system", Text: ev.Text, Error: true})
			r.setBusy(false)
			if r.Kind == KindTask {
				r.a.taskFailed(r, ev.Text)
			}
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
