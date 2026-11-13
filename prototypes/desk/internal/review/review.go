// Package review stores comment threads and edit suggestions for documents,
// in sidecar files (<root>/.nexus/comments/<path>.json) so any agent or tool
// can take part. Anchors are W3C TextQuoteSelector-style (exact + context),
// so they survive edits made outside the app or are reported as orphaned.
package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
)

type Anchor struct {
	Exact  string `json:"exact"`
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
}

type Comment struct {
	ID     string    `json:"id"`
	Author string    `json:"author"` // "you" | "agent"
	Text   string    `json:"text"`
	TS     time.Time `json:"ts"`
}

type Suggestion struct {
	ID      string    `json:"id"`
	Author  string    `json:"author"`
	Find    string    `json:"find"`
	Replace string    `json:"replace"`
	Note    string    `json:"note,omitempty"`
	Status  string    `json:"status"` // pending | accepted | rejected
	TS      time.Time `json:"ts"`
}

type Thread struct {
	ID          string       `json:"id"`
	Anchor      Anchor       `json:"anchor"`
	Status      string       `json:"status"` // open | resolved
	Comments    []Comment    `json:"comments"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

type Doc struct {
	Path    string    `json:"path"`
	Threads []*Thread `json:"threads"`
	Seq     int       `json:"seq"`
}

type Store struct {
	roots *files.Roots
	mu    sync.Mutex
}

func New(r *files.Roots) *Store { return &Store{roots: r} }

func (s *Store) loadLocked(path string) (*Doc, string, error) {
	sc, err := s.roots.Sidecar(path, "comments")
	if err != nil {
		return nil, "", err
	}
	d := &Doc{Path: path}
	if b, err := os.ReadFile(sc); err == nil {
		if err := json.Unmarshal(b, d); err != nil {
			return nil, "", err
		}
	}
	if d.Threads == nil {
		d.Threads = []*Thread{}
	}
	return d, sc, nil
}

func (s *Store) saveLocked(sc string, d *Doc) error {
	b, _ := json.MarshalIndent(d, "", "  ")
	return files.WriteAtomic(sc, b)
}

func (s *Store) Get(path string) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, _, err := s.loadLocked(path)
	return d, err
}

// Content returns the document source.
func (s *Store) Content(path string) (string, error) {
	abs, err := s.roots.Resolve(path)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *Store) mutate(path string, f func(d *Doc) error) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, sc, err := s.loadLocked(path)
	if err != nil {
		return nil, err
	}
	if err := f(d); err != nil {
		return nil, err
	}
	return d, s.saveLocked(sc, d)
}

func (d *Doc) next(p string) string {
	d.Seq++
	return fmt.Sprintf("%s%d", p, d.Seq)
}

func (d *Doc) thread(id string) (*Thread, error) {
	for _, t := range d.Threads {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, fmt.Errorf("no thread %s", id)
}

func (s *Store) AddThread(path string, a Anchor, author, text string) (*Doc, *Thread, error) {
	if strings.TrimSpace(a.Exact) == "" {
		return nil, nil, errors.New("select some text to comment on")
	}
	var th *Thread
	d, err := s.mutate(path, func(d *Doc) error {
		th = &Thread{ID: d.next("t"), Anchor: a, Status: "open"}
		th.Comments = []Comment{{ID: d.next("c"), Author: author, Text: text, TS: time.Now()}}
		d.Threads = append(d.Threads, th)
		return nil
	})
	return d, th, err
}

func (s *Store) Reply(path, thread, author, text string) (*Doc, error) {
	return s.mutate(path, func(d *Doc) error {
		t, err := d.thread(thread)
		if err != nil {
			return err
		}
		t.Comments = append(t.Comments, Comment{ID: d.next("c"), Author: author, Text: text, TS: time.Now()})
		if author == "you" {
			t.Status = "open"
		}
		return nil
	})
}

func (s *Store) SetStatus(path, thread, status string) (*Doc, error) {
	return s.mutate(path, func(d *Doc) error {
		t, err := d.thread(thread)
		if err != nil {
			return err
		}
		t.Status = status
		return nil
	})
}

// Suggest attaches a proposed edit (exact find/replace on the source) to a
// thread. thread "" creates a new thread anchored on the find text.
func (s *Store) Suggest(path, thread, author, find, replace, note string) (*Doc, error) {
	src, err := s.Content(path)
	if err != nil {
		return nil, err
	}
	if n := strings.Count(src, find); n != 1 {
		return nil, fmt.Errorf("find text must occur exactly once in the source (found %d); include more context", n)
	}
	return s.mutate(path, func(d *Doc) error {
		var t *Thread
		if thread == "" {
			anchorText := firstLine(find)
			t = &Thread{ID: d.next("t"), Anchor: Anchor{Exact: plain(anchorText)}, Status: "open"}
			if note != "" {
				t.Comments = []Comment{{ID: d.next("c"), Author: author, Text: note, TS: time.Now()}}
			}
			d.Threads = append(d.Threads, t)
		} else {
			var err error
			if t, err = d.thread(thread); err != nil {
				return err
			}
		}
		t.Suggestions = append(t.Suggestions, Suggestion{ID: d.next("x"), Author: author, Find: find, Replace: replace, Note: note, Status: "pending", TS: time.Now()})
		return nil
	})
}

// Resolve accepts or rejects a suggestion. Accept applies it to the file.
func (s *Store) ResolveSuggestion(path, thread, sugg string, accept bool) (*Doc, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, sc, err := s.loadLocked(path)
	if err != nil {
		return nil, err
	}
	t, err := d.thread(thread)
	if err != nil {
		return nil, err
	}
	var sg *Suggestion
	for i := range t.Suggestions {
		if t.Suggestions[i].ID == sugg {
			sg = &t.Suggestions[i]
		}
	}
	if sg == nil {
		return nil, fmt.Errorf("no suggestion %s", sugg)
	}
	if sg.Status != "pending" {
		return nil, fmt.Errorf("suggestion already %s", sg.Status)
	}
	if accept {
		abs, err := s.roots.Resolve(path)
		if err != nil {
			return nil, err
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		src := string(b)
		if n := strings.Count(src, sg.Find); n != 1 {
			return nil, fmt.Errorf("can't apply: the text to replace now occurs %d times (the file changed)", n)
		}
		if err := files.WriteAtomic(abs, []byte(strings.Replace(src, sg.Find, sg.Replace, 1))); err != nil {
			return nil, err
		}
		sg.Status = "accepted"
		// Re-anchor the thread on the new text if the old anchor was inside the edit.
		if strings.Contains(sg.Find, t.Anchor.Exact) {
			t.Anchor = Anchor{Exact: plain(firstLine(sg.Replace))}
		}
	} else {
		sg.Status = "rejected"
	}
	return d, s.saveLocked(sc, d)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}

// plain strips common markdown markup so anchors match rendered text.
func plain(s string) string {
	r := strings.NewReplacer("**", "", "__", "", "`", "", "# ", "", "> ", "")
	s = r.Replace(s)
	s = strings.TrimLeft(s, "#-* ")
	return strings.TrimSpace(s)
}
