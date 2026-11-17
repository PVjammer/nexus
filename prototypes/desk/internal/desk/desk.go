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
	Captures  []*Capture         `json:"captures"`
	Items     []*Item            `json:"items"`
	Tasks     []*Task            `json:"tasks"`
	Widgets   []*Widget          `json:"widgets,omitempty"` // legacy; migrated into Layouts["home"]
	Layouts   map[string]*Layout `json:"layouts"`           // "home" (project dashboards later)
	Dismissed map[string]bool    `json:"dismissed"`         // foreman inbox ids you cleared in Nexus
	Seq       int                `json:"seq"`
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
