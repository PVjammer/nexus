// Package desk holds what Nexus itself owns, layered over your projects:
// captures and their filing, items, dispatched tasks, screen layouts (the home
// screen's widgets) and what you've dismissed. Your project folders and foreman ledgers are never
// written; this lives in the Nexus data directory.
//
// Prototype note: one JSON file. Not a storage decision.
package desk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Proposal struct {
	Project string `json:"project"`
	Kind    string `json:"kind"` // note | task | question
	Title   string `json:"title"`
	Reason  string `json:"reason,omitempty"`
}

type Capture struct {
	ID       string    `json:"id"`
	Text     string    `json:"text"`
	Created  time.Time `json:"created"`
	Status   string    `json:"status"` // new | proposed | filed | dismissed
	Proposal *Proposal `json:"proposal,omitempty"`
	Item     string    `json:"item,omitempty"` // item created when filed
}

type Item struct {
	ID      string    `json:"id"`
	Project string    `json:"project"`
	Kind    string    `json:"kind"` // note | task | question
	Title   string    `json:"title"`
	Body    string    `json:"body,omitempty"`
	State   string    `json:"state"`  // open | done
	Source  string    `json:"source"` // capture | you | agent
	Created time.Time `json:"created"`
}

type Task struct {
	ID       string     `json:"id"`
	Project  string     `json:"project"`
	Title    string     `json:"title"`
	Brief    string     `json:"brief"`
	Status   string     `json:"status"` // draft | running | done | failed | stopped
	Summary  string     `json:"summary,omitempty"`
	Report   string     `json:"report,omitempty"`  // app path of the latest report
	Reports  []string   `json:"reports,omitempty"` // earlier versions, oldest first
	Plan     []PlanStep `json:"plan,omitempty"`    // the task agent's pinned checklist
	From     string     `json:"from,omitempty"`    // what it came from (item id, backlog text)
	Reviewed bool       `json:"reviewed,omitempty"`
	Created  time.Time  `json:"created"`
	Updated  time.Time  `json:"updated"`
}

type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"` // todo | doing | done | skipped
	Note   string `json:"note,omitempty"`
}

// Widget is a dashboard plugin widget from before layouts existed. Old data
// files are migrated into the home layout on open.
type Widget struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Wide   bool   `json:"wide,omitempty"`
}

// WorkTask is a task: a concrete next action toward a work item, or a plain
// todo (Item == ""). Tasks live only in Nexus; they're never written to the
// work item's source. Status is the usual board: todo, doing, blocked,
// review, done.
type WorkTask struct {
	ID        string    `json:"id"`
	Item      string    `json:"item,omitempty"` // work item ref ("" = a todo)
	Project   string    `json:"project,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`             // todo | doing | blocked | review | done
	Assignee  string    `json:"assignee"`           // you | agent | someone's name
	Reviewer  string    `json:"reviewer,omitempty"` // who reviews it in "review"
	BlockedBy []string  `json:"blockedBy,omitempty"`
	Due       string    `json:"due,omitempty"` // YYYY-MM-DD
	Note      string    `json:"note,omitempty"`
	By        string    `json:"by"` // who added it: you | agent
	Why       string    `json:"why,omitempty"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}

// Tracked is what Nexus keeps about a work item it reads from a source:
// whether it's in progress, parked (and on what), and when it's done.
type Tracked struct {
	Ref      string   `json:"ref"`
	State    string   `json:"state,omitempty"` // in-progress | parked | not-started
	ParkNote string   `json:"parkNote,omitempty"`
	DoneWhen []string `json:"doneWhen,omitempty"`
	// Brief is what the work is, in a few lines: what the user said when the
	// agent asked (scope, constraints, who's involved). Shaping fills it.
	Brief string `json:"brief,omitempty"`
	// Done: the user said so (pressing Done, or confirming an agent's
	// claim). Closed is when; PrevState is what Undo goes back to; Closeout
	// is the task that wraps it up (report, what's left, loose ends).
	Closed    time.Time `json:"closed,omitempty"`
	PrevState string    `json:"prevState,omitempty"`
	Closeout  string    `json:"closeout,omitempty"`
	// Claim: an agent thinks it's done. Only the user can say so.
	Claim   *DoneClaim `json:"claim,omitempty"`
	Updated time.Time  `json:"updated"`
}

// DoneClaim is an agent saying a work item is done, with how each done-when
// check was met. It waits in Needs you until the user confirms or says not yet.
type DoneClaim struct {
	Agent    string    `json:"agent"` // run id, told the answer
	By       string    `json:"by"`    // its label, for the user
	Note     string    `json:"note,omitempty"`
	Evidence []string  `json:"evidence,omitempty"` // one per done-when check, in order
	At       time.Time `json:"at"`
}

// WorkProposal is something an agent suggests and the user accepts or
// dismisses: a new work item for a project (Item == "") or a task toward a
// work item. Proposals come in batches the user reviews as one checklist.
type WorkProposal struct {
	ID       string   `json:"id"`
	Batch    string   `json:"batch"`
	Project  string   `json:"project"`
	Item     string   `json:"item,omitempty"` // "" = proposes a work item; else a task toward this ref
	Title    string   `json:"title"`
	Why      string   `json:"why,omitempty"`
	Evidence []string `json:"evidence,omitempty"` // where it came from: "src/x.go:12", "branch feat/y"
	Assignee string   `json:"assignee,omitempty"` // tasks: you | agent | a name
	// Work items: a draft of what it is and what done looks like, from the
	// evidence, so an accepted item is clear before anyone shapes it.
	Brief    string   `json:"brief,omitempty"`
	DoneWhen []string `json:"doneWhen,omitempty"`
	// Tasks: titles of tasks this one comes after (earlier in the batch, or
	// already on the work item). Accepted, it starts blocked on them.
	After   []string  `json:"after,omitempty"`
	Status  string    `json:"status"` // open | accepted | dismissed
	Agent   string    `json:"agent"`  // who proposed it ("p:<project>", "home")
	Created time.Time `json:"created"`
}

// Onboarding is where a project's setup conversation is.
type Onboarding struct {
	Started time.Time `json:"started"`
	Scanned time.Time `json:"scanned,omitempty"` // last project_scan
}

// LayoutWidget is one widget on a screen: what it shows (Kind, from the
// catalog the app keeps), how (Shape), for what (Scope) and its size. A widget
// is data, so you and the agent change it the same way.
type LayoutWidget struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`             // catalog key: chat, needs, running, ...
	Shape  string    `json:"shape,omitempty"`  // how it's drawn: list, compact, minimal, ...
	Scope  string    `json:"scope,omitempty"`  // "" = everything, "p:<project>" = one project
	Plugin string    `json:"plugin,omitempty"` // kind "plugin": which plugin
	W      int       `json:"w"`                // columns wide (clamped to the grid)
	H      int       `json:"h,omitempty"`      // rows tall; 0 = as tall as it needs
	By     string    `json:"by"`               // you | agent | default
	Why    string    `json:"why,omitempty"`    // the agent's reason, shown to you
	Added  time.Time `json:"added"`
}

// Layout is a screen: a grid and the widgets on it, in order.
type Layout struct {
	Cols    int             `json:"cols"`   // 1..4
	Tiling  string          `json:"tiling"` // flow (page scrolls) | tile (fills the screen)
	Widgets []*LayoutWidget `json:"widgets"`
}

func (l Layout) clone() Layout {
	out := Layout{Cols: l.Cols, Tiling: l.Tiling}
	for _, w := range l.Widgets {
		c := *w
		out.Widgets = append(out.Widgets, &c)
	}
	return out
}

// DefaultHome is the home screen before anyone changes it: just the prompt.
func DefaultHome(now time.Time) Layout {
	return Layout{Cols: 3, Tiling: "flow", Widgets: []*LayoutWidget{{ID: "chat", Kind: "chat", Shape: "minimal", W: 4, By: "default", Added: now}}}
}

type undoEntry struct {
	layout Layout
	label  string
}

type State struct {
	Captures  []*Capture          `json:"captures"`
	Items     []*Item             `json:"items"`
	Tasks     []*Task             `json:"tasks"`
	Widgets   []*Widget           `json:"widgets,omitempty"` // legacy; migrated into Layouts["home"]
	Layouts   map[string]*Layout  `json:"layouts"`           // "home" (project dashboards later)
	WorkTasks []*WorkTask         `json:"workTasks"`
	Tracked   map[string]*Tracked `json:"tracked"` // by work item ref
	// WipLimit is how many work items can be in progress before starting
	// another one suggests parking one. nil = the default (4); 0 = never.
	WipLimit  *int                   `json:"wipLimit,omitempty"`
	Proposals []*WorkProposal        `json:"proposals,omitempty"`
	Onboard   map[string]*Onboarding `json:"onboard,omitempty"` // by project id
	Dismissed map[string]bool        `json:"dismissed"`         // foreman inbox ids you cleared in Nexus
	// Pins: messages you pinned in a conversation, by agent id, oldest
	// first. They're yours: a strip at the top of the chat, never sent to
	// the agent.
	Pins map[string][]Pin `json:"pins,omitempty"`
	// Private: projects walled off from every agent outside them (by id).
	// Only the user sets it.
	Private map[string]bool `json:"private,omitempty"`
	Seq     int             `json:"seq"`
}

// Pin is one pinned message: its id in the agent's transcript, and a copy
// of its text (the transcript keeps its newest 400 messages).
type Pin struct {
	Msg  int       `json:"msg"`
	Role string    `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	st    State
	onChg func()
	undo  map[string][]undoEntry // per layout; memory only
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, fmt.Errorf("desk: parse %s: %w", path, err)
		}
	}
	if s.st.Dismissed == nil {
		s.st.Dismissed = map[string]bool{}
	}
	s.undo = map[string][]undoEntry{}
	if s.st.Onboard == nil {
		s.st.Onboard = map[string]*Onboarding{}
	}
	if s.st.Tracked == nil {
		s.st.Tracked = map[string]*Tracked{}
	}
	if s.st.Layouts == nil {
		s.st.Layouts = map[string]*Layout{}
	}
	if s.st.Layouts["home"] == nil {
		home := DefaultHome(time.Now())
		// Widgets built before layouts existed keep their place on home.
		for _, w := range s.st.Widgets {
			width := 1
			if w.Wide {
				width = 2
			}
			home.Widgets = append(home.Widgets, &LayoutWidget{ID: w.ID, Kind: "plugin", Shape: "frame", Plugin: w.Plugin, W: width, By: "you", Added: time.Now()})
		}
		s.st.Layouts["home"] = &home
	}
	s.st.Widgets = nil
	// Tasks can't survive a restart mid-run (their agent is gone).
	for _, t := range s.st.Tasks {
		if t.Status == "running" {
			t.Status, t.Summary = "stopped", "stopped: Nexus restarted while it was running"
		}
	}
	return s, nil
}

func (s *Store) OnChange(f func()) { s.onChg = f }

func (s *Store) commitLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err == nil {
		b, _ := json.MarshalIndent(s.st, "", "  ")
		if os.WriteFile(s.path+".tmp", b, 0o600) == nil {
			_ = os.Rename(s.path+".tmp", s.path)
		}
	}
	if s.onChg != nil {
		go s.onChg()
	}
}

func (s *Store) id(prefix string) string {
	s.st.Seq++
	return fmt.Sprintf("%s%d", prefix, s.st.Seq)
}

func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.st)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}

// ---- captures ----

// AddCaptures splits a dump into one capture per non-empty line (a single
// paragraph stays one capture) and returns them.
func (s *Store) AddCaptures(text string) []Capture {
	var parts []string
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 1 {
		parts = lines
	} else {
		for _, l := range lines {
			if t := strings.TrimSpace(l); t != "" {
				parts = append(parts, t)
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Capture
	for _, p := range parts {
		c := &Capture{ID: s.id("c"), Text: p, Created: time.Now(), Status: "new"}
		s.st.Captures = append(s.st.Captures, c)
		out = append(out, *c)
	}
	s.commitLocked()
	return out
}

func (s *Store) capLocked(id string) (*Capture, error) {
	for _, c := range s.st.Captures {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no capture %s", id)
}

func (s *Store) Propose(id string, p Proposal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.capLocked(id)
	if err != nil {
		return err
	}
	if c.Status == "filed" || c.Status == "dismissed" {
		return fmt.Errorf("capture %s is already %s", id, c.Status)
	}
	c.Proposal, c.Status = &p, "proposed"
	s.commitLocked()
	return nil
}

// File turns a capture into an item in a project (you accepted the filing).
func (s *Store) File(id, project, kind, title string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.capLocked(id)
	if err != nil {
		return Item{}, err
	}
	if title == "" {
		title = c.Text
	}
	it := &Item{ID: s.id("i"), Project: project, Kind: kindOr(kind), Title: title, State: "open", Source: "capture", Created: time.Now()}
	if title != c.Text {
		it.Body = c.Text
	}
	s.st.Items = append(s.st.Items, it)
	c.Status, c.Item = "filed", it.ID
	if c.Proposal == nil {
		c.Proposal = &Proposal{Project: project, Kind: it.Kind, Title: title}
	}
	s.commitLocked()
	return *it, nil
}

func (s *Store) DismissCapture(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.capLocked(id)
	if err != nil {
		return err
	}
	c.Status = "dismissed"
	s.commitLocked()
	return nil
}

func kindOr(k string) string {
	switch k {
	case "note", "task", "question":
		return k
	}
	return "task"
}

// ---- items ----

func (s *Store) AddItem(it Item) Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	it.ID, it.Kind, it.State, it.Created = s.id("i"), kindOr(it.Kind), "open", time.Now()
	s.st.Items = append(s.st.Items, &it)
	s.commitLocked()
	return it
}

func (s *Store) UpdateItem(id string, f func(*Item)) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.st.Items {
		if it.ID == id {
			f(it)
			s.commitLocked()
			return *it, nil
		}
	}
	return Item{}, fmt.Errorf("no item %s", id)
}

// ---- tasks ----

func (s *Store) AddTask(t Task) Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID, t.Status, t.Created, t.Updated = s.id("t"), "draft", time.Now(), time.Now()
	s.st.Tasks = append(s.st.Tasks, &t)
	s.commitLocked()
	return t
}

func (s *Store) Task(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.st.Tasks {
		if t.ID == id {
			return *t, true
		}
	}
	return Task{}, false
}

func (s *Store) UpdateTask(id string, f func(*Task) error) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.st.Tasks {
		if t.ID == id {
			if err := f(t); err != nil {
				return *t, err
			}
			t.Updated = time.Now()
			s.commitLocked()
			return *t, nil
		}
	}
	return Task{}, fmt.Errorf("no task %s", id)
}

func (s *Store) RemoveTask(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.st.Tasks {
		if t.ID == id {
			s.st.Tasks = append(s.st.Tasks[:i], s.st.Tasks[i+1:]...)
			s.commitLocked()
			return
		}
	}
}

// ---- layouts ----

// Layout returns a copy of a screen's layout.
func (s *Store) Layout(name string) Layout {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.st.Layouts[name]; l != nil {
		return l.clone()
	}
	return Layout{}
}

// EditLayout changes a layout under the lock. f gets a working copy and a
// way to make widget ids; if it returns an error nothing changes. A change
// is undoable (label says what it was).
func (s *Store) EditLayout(name, label string, f func(l *Layout, newID func() string) error) (Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.st.Layouts[name]
	if cur == nil {
		d := DefaultHome(time.Now())
		cur = &d
	}
	work := cur.clone()
	if err := f(&work, func() string { return s.id("w") }); err != nil {
		return cur.clone(), err
	}
	before, _ := json.Marshal(cur)
	after, _ := json.Marshal(work)
	if string(before) == string(after) {
		return work, nil
	}
	st := append(s.undo[name], undoEntry{layout: cur.clone(), label: label})
	if len(st) > 30 {
		st = st[len(st)-30:]
	}
	s.undo[name] = st
	s.st.Layouts[name] = &work
	s.commitLocked()
	return work.clone(), nil
}

// HasLayout says whether a layout exists (drafts exist only while staged).
func (s *Store) HasLayout(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Layouts[name] != nil
}

// StartDraft makes draft a copy of from, unless the draft already exists.
func (s *Store) StartDraft(draft, from string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Layouts[draft] != nil {
		return
	}
	src := s.st.Layouts[from]
	if src == nil {
		d := DefaultHome(time.Now())
		src = &d
	}
	c := src.clone()
	s.st.Layouts[draft] = &c
	s.undo[draft] = nil
	s.commitLocked()
}

// DropLayout deletes a layout (a discarded draft) and its undo history.
func (s *Store) DropLayout(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Layouts[name] == nil {
		return
	}
	delete(s.st.Layouts, name)
	delete(s.undo, name)
	s.commitLocked()
}

// UndoLayout puts back the layout before the last change. It returns what
// was undone.
func (s *Store) UndoLayout(name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.undo[name]
	if len(st) == 0 {
		return "", false
	}
	last := st[len(st)-1]
	s.undo[name] = st[:len(st)-1]
	l := last.layout.clone()
	s.st.Layouts[name] = &l
	s.commitLocked()
	return last.label, true
}

// UndoInfo is how many changes can be undone, and the last one's label.
func (s *Store) UndoInfo(name string) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.undo[name]
	if len(st) == 0 {
		return 0, ""
	}
	return len(st), st[len(st)-1].label
}

// ---- plugin widgets (kept for widget_write and the plugin bridge) ----

// AddWidget puts a plugin on the home screen, or resizes it if it's there.
func (s *Store) AddWidget(plugin string, wide bool) Widget {
	width := 1
	if wide {
		width = 2
	}
	var out Widget
	s.EditLayout("home", "add widget "+plugin, func(l *Layout, newID func() string) error {
		for _, w := range l.Widgets {
			if w.Kind == "plugin" && w.Plugin == plugin {
				w.W = width
				out = Widget{ID: w.ID, Plugin: plugin, Wide: wide}
				return nil
			}
		}
		w := &LayoutWidget{ID: newID(), Kind: "plugin", Shape: "frame", Plugin: plugin, W: width, By: "agent", Why: "built as a widget", Added: time.Now()}
		l.Widgets = append(l.Widgets, w)
		out = Widget{ID: w.ID, Plugin: plugin, Wide: wide}
		return nil
	})
	return out
}

// Widget finds a plugin widget on the home screen by widget id.
func (s *Store) Widget(id string) (Widget, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l := s.st.Layouts["home"]; l != nil {
		for _, w := range l.Widgets {
			if w.ID == id && w.Kind == "plugin" {
				return Widget{ID: w.ID, Plugin: w.Plugin, Wide: w.W > 1}, true
			}
		}
	}
	return Widget{}, false
}

// RemoveWidget takes a widget off the home screen, by widget id or plugin id.
func (s *Store) RemoveWidget(id string) {
	s.EditLayout("home", "remove widget", func(l *Layout, _ func() string) error {
		for i, w := range l.Widgets {
			if w.ID == id || (w.Kind == "plugin" && w.Plugin == id) {
				l.Widgets = append(l.Widgets[:i], l.Widgets[i+1:]...)
				return nil
			}
		}
		return nil
	})
}

// ---- work tasks ----

func (s *Store) AddWorkTask(t WorkTask) WorkTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID, t.Created, t.Updated = s.id("k"), time.Now(), time.Now()
	s.st.WorkTasks = append(s.st.WorkTasks, &t)
	s.settleLocked()
	s.commitLocked()
	return t
}

func (s *Store) WorkTask(id string) (WorkTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.st.WorkTasks {
		if t.ID == id {
			return *t, true
		}
	}
	return WorkTask{}, false
}

// UpdateWorkTask changes a task; f's error leaves it untouched. Tasks that
// were blocked only on finished tasks move on (settle).
func (s *Store) UpdateWorkTask(id string, f func(*WorkTask) error) (WorkTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.st.WorkTasks {
		if t.ID == id {
			c := *t
			if err := f(&c); err != nil {
				return *t, err
			}
			c.Updated = time.Now()
			*t = c
			s.settleLocked()
			s.commitLocked()
			return *t, nil
		}
	}
	return WorkTask{}, fmt.Errorf("no task %s", id)
}

func (s *Store) RemoveWorkTask(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.st.WorkTasks {
		if t.ID == id {
			s.st.WorkTasks = append(s.st.WorkTasks[:i], s.st.WorkTasks[i+1:]...)
			for _, o := range s.st.WorkTasks {
				o.BlockedBy = remove(o.BlockedBy, id)
			}
			s.settleLocked()
			s.commitLocked()
			return
		}
	}
}

func remove(l []string, v string) []string {
	var out []string
	for _, x := range l {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// settleLocked moves a blocked task to "todo" once everything it waits on is
// done (or gone). Blocked tasks with no BlockedBy wait on something outside
// Nexus and stay blocked until someone moves them.
func (s *Store) settleLocked() {
	byID := map[string]*WorkTask{}
	for _, t := range s.st.WorkTasks {
		byID[t.ID] = t
	}
	for _, t := range s.st.WorkTasks {
		if t.Status != "blocked" || len(t.BlockedBy) == 0 {
			continue
		}
		open := false
		for _, b := range t.BlockedBy {
			if o := byID[b]; o != nil && o.Status != "done" {
				open = true
			}
		}
		if !open {
			t.Status, t.Updated = "todo", time.Now()
		}
	}
}

// DefaultWipLimit is the in-progress limit until the user sets one.
const DefaultWipLimit = 4

func (s *Store) WipLimit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.WipLimit == nil {
		return DefaultWipLimit
	}
	return *s.st.WipLimit
}

func (s *Store) SetWipLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.WipLimit = &n
	s.commitLocked()
}

// ---- private projects ----

func (s *Store) SetPrivate(pid string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Private == nil {
		s.st.Private = map[string]bool{}
	}
	if on {
		s.st.Private[pid] = true
	} else {
		delete(s.st.Private, pid)
	}
	s.commitLocked()
}

func (s *Store) IsPrivate(pid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pid != "" && s.st.Private[pid]
}

// ---- pins ----

// SetPin pins (on) or unpins a message in an agent's conversation.
func (s *Store) SetPin(agent string, p Pin, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Pins == nil {
		s.st.Pins = map[string][]Pin{}
	}
	var keep []Pin
	for _, x := range s.st.Pins[agent] {
		if x.Msg != p.Msg {
			keep = append(keep, x)
		}
	}
	if on {
		p.At = time.Now()
		keep = append(keep, p)
	}
	if len(keep) == 0 {
		delete(s.st.Pins, agent)
	} else {
		s.st.Pins[agent] = keep
	}
	s.commitLocked()
}

// ---- tracked work items ----

func (s *Store) Track(ref string, f func(*Tracked)) Tracked {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.st.Tracked[ref]
	if t == nil {
		t = &Tracked{Ref: ref}
		s.st.Tracked[ref] = t
	}
	f(t)
	t.Updated = time.Now()
	s.commitLocked()
	return *t
}

// ---- dismissals ----

func (s *Store) Dismiss(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Dismissed[id] = true
	s.commitLocked()
}

func (s *Store) Dismissed(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.Dismissed[id]
}

// ---- proposals ----

// AddProposals stores a batch and returns it with ids.
func (s *Store) AddProposals(ps []WorkProposal) []WorkProposal { return s.AddToBatch("", ps) }

// AddToBatch adds proposals to batch (a new one when ""), so what an agent
// proposes over several calls in one turn is one checklist.
func (s *Store) AddToBatch(batch string, ps []WorkProposal) []WorkProposal {
	s.mu.Lock()
	defer s.mu.Unlock()
	if batch == "" {
		batch = s.id("b")
	}
	out := make([]WorkProposal, 0, len(ps))
	for _, p := range ps {
		p.ID, p.Batch, p.Status, p.Created = s.id("q"), batch, "open", time.Now()
		c := p
		s.st.Proposals = append(s.st.Proposals, &c)
		out = append(out, p)
	}
	s.commitLocked()
	return out
}

func (s *Store) Proposal(id string) (WorkProposal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.Proposals {
		if p.ID == id {
			return *p, true
		}
	}
	return WorkProposal{}, false
}

// SetProposal marks a proposal accepted or dismissed.
func (s *Store) SetProposal(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.st.Proposals {
		if p.ID == id {
			if p.Status != "open" {
				return fmt.Errorf("proposal %s is already %s", id, p.Status)
			}
			p.Status = status
			s.commitLocked()
			return nil
		}
	}
	return fmt.Errorf("no proposal %q", id)
}

// ---- onboarding ----

func (s *Store) Onboarding(project string) (Onboarding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.st.Onboard[project]
	if o == nil {
		return Onboarding{}, false
	}
	return *o, true
}

func (s *Store) TouchOnboarding(project string, f func(*Onboarding)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.st.Onboard[project]
	if o == nil {
		o = &Onboarding{}
		s.st.Onboard[project] = o
	}
	f(o)
	s.commitLocked()
}
