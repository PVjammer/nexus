// Package desk holds what Nexus itself owns, layered over your projects:
// captures and their filing, items, dispatched tasks, dashboard widgets and
// what you've dismissed. Your project folders and foreman ledgers are never
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
	ID       string    `json:"id"`
	Project  string    `json:"project"`
	Title    string    `json:"title"`
	Brief    string    `json:"brief"`
	Status   string    `json:"status"` // draft | running | done | failed | stopped
	Summary  string    `json:"summary,omitempty"`
	Report   string    `json:"report,omitempty"`  // app path of the latest report
	Reports  []string  `json:"reports,omitempty"` // earlier versions, oldest first
	Plan     []PlanStep `json:"plan,omitempty"`   // the task agent's pinned checklist
	From     string    `json:"from,omitempty"`   // what it came from (item id, backlog text)
	Reviewed bool      `json:"reviewed,omitempty"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
}

type PlanStep struct {
	Text   string `json:"text"`
	Status string `json:"status"` // todo | doing | done | skipped
	Note   string `json:"note,omitempty"`
}

type Widget struct {
	ID     string `json:"id"`
	Plugin string `json:"plugin"`
	Wide   bool   `json:"wide,omitempty"`
}

type State struct {
	Captures  []*Capture      `json:"captures"`
	Items     []*Item         `json:"items"`
	Tasks     []*Task         `json:"tasks"`
	Widgets   []*Widget       `json:"widgets"`
	Dismissed map[string]bool `json:"dismissed"` // foreman inbox ids you cleared in Nexus
	Seq       int             `json:"seq"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	st    State
	onChg func()
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

// ---- widgets ----

func (s *Store) AddWidget(plugin string, wide bool) Widget {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.st.Widgets {
		if w.Plugin == plugin {
			w.Wide = wide
			s.commitLocked()
			return *w
		}
	}
	w := &Widget{ID: s.id("w"), Plugin: plugin, Wide: wide}
	s.st.Widgets = append(s.st.Widgets, w)
	s.commitLocked()
	return *w
}

func (s *Store) Widget(id string) (Widget, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range s.st.Widgets {
		if w.ID == id {
			return *w, true
		}
	}
	return Widget{}, false
}

func (s *Store) RemoveWidget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.st.Widgets {
		if w.ID == id || w.Plugin == id {
			s.st.Widgets = append(s.st.Widgets[:i], s.st.Widgets[i+1:]...)
			s.commitLocked()
			return
		}
	}
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
