// Package wiki is each project's wiki: markdown pages with a little
// frontmatter, owned by Nexus (stored in its data directory, never in your
// repo). index.md is the bootstrap every agent in the project starts with.
// Every write keeps the previous version; changes from task agents arrive
// as proposals you accept or reject.
package wiki

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Meta struct {
	Summary   string    `json:"summary,omitempty"`
	Sources   []string  `json:"sources,omitempty"` // app paths the page is based on
	Verified  time.Time `json:"verified,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
}

type Page struct {
	Slug    string    `json:"slug"`
	Title   string    `json:"title"`
	Meta    Meta      `json:"meta"`
	Updated time.Time `json:"updated"`
	Size    int       `json:"size"`
	Stale   []string  `json:"stale,omitempty"` // sources changed since verified
}

type Proposal struct {
	ID      string    `json:"id"`
	Project string    `json:"project"`
	Slug    string    `json:"slug"`
	Content string    `json:"content"`
	Reason  string    `json:"reason"`
	Sources []string  `json:"sources,omitempty"`
	By      string    `json:"by"`
	Task    string    `json:"task,omitempty"`
	Created time.Time `json:"created"`
	Status  string    `json:"status"` // pending | accepted | rejected
}

type Store struct {
	mu   sync.Mutex
	root string // <data>/wiki
	seq  int
}

func New(root string) *Store { os.MkdirAll(root, 0o755); return &Store{root: root} }

func (s *Store) Root() string { return s.root }

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,60}$`)

// Slug normalizes a page name ("How it works" -> "how-it-works").
func Slug(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(name)), ".md")
	var b strings.Builder
	dash := false
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (s *Store) dir(project string) string { return filepath.Join(s.root, project) }
func (s *Store) path(project, slug string) string {
	return filepath.Join(s.dir(project), slug+".md")
}

// Parse splits "---\nkey: value\n---\nbody" frontmatter from a page.
func Parse(src string) (Meta, string) {
	var m Meta
	if !strings.HasPrefix(src, "---\n") {
		return m, src
	}
	end := strings.Index(src[4:], "\n---\n")
	if end < 0 {
		return m, src
	}
	head, body := src[4:4+end], src[4+end+5:]
	for _, line := range strings.Split(head, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "summary":
			m.Summary = v
		case "sources":
			for _, x := range strings.Split(strings.Trim(v, "[]"), ",") {
				if x = strings.Trim(strings.TrimSpace(x), `"'`); x != "" {
					m.Sources = append(m.Sources, x)
				}
			}
		case "verified":
			m.Verified, _ = time.Parse(time.RFC3339, v)
			if m.Verified.IsZero() {
				m.Verified, _ = time.Parse("2006-01-02", v)
			}
		case "updated_by":
			m.UpdatedBy = v
		}
	}
	return m, body
}

// Render writes frontmatter plus body.
func Render(m Meta, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	if m.Summary != "" {
		fmt.Fprintf(&b, "summary: %s\n", strings.ReplaceAll(m.Summary, "\n", " "))
	}
	if len(m.Sources) > 0 {
		fmt.Fprintf(&b, "sources: %s\n", strings.Join(m.Sources, ", "))
	}
	if !m.Verified.IsZero() {
		fmt.Fprintf(&b, "verified: %s\n", m.Verified.UTC().Format(time.RFC3339))
	}
	if m.UpdatedBy != "" {
		fmt.Fprintf(&b, "updated_by: %s\n", m.UpdatedBy)
	}
	b.WriteString("---\n")
	b.WriteString(strings.TrimLeft(body, "\n"))
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func titleOf(slug, body string) string {
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "# ") {
			return strings.TrimSpace(l[2:])
		}
	}
	return slug
}

func (s *Store) List(project string) []Page {
	ents, _ := os.ReadDir(s.dir(project))
	var out []Page
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		b, err := os.ReadFile(filepath.Join(s.dir(project), e.Name()))
		if err != nil {
			continue
		}
		info, _ := e.Info()
		m, body := Parse(string(b))
		out = append(out, Page{Slug: slug, Title: titleOf(slug, body), Meta: m, Updated: info.ModTime(), Size: len(b)})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Slug == "index") != (out[j].Slug == "index") {
			return out[i].Slug == "index"
		}
		return out[i].Slug < out[j].Slug
	})
	return out
}

func (s *Store) Read(project, slug string) (string, error) {
	slug = Slug(slug)
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("bad page name %q", slug)
	}
	b, err := os.ReadFile(s.path(project, slug))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no wiki page %q (see wiki_list)", slug)
	}
	return string(b), err
}

// Write replaces a page, keeping the previous version under .history/.
func (s *Store) Write(project, slug, content string) (string, error) {
	slug = Slug(slug)
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("bad page name %q (use lowercase words with dashes)", slug)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.path(project, slug)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if old, err := os.ReadFile(p); err == nil {
		h := filepath.Join(s.dir(project), ".history")
		os.MkdirAll(h, 0o755)
		os.WriteFile(filepath.Join(h, fmt.Sprintf("%s.%s.%09d.md", slug, time.Now().UTC().Format("20060102T150405"), time.Now().Nanosecond())), old, 0o644)
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	return slug, os.Rename(tmp, p)
}

// Search is a simple case-insensitive line search over a project's pages.
func (s *Store) Search(project, q string, max int) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var hits []string
	for _, p := range s.List(project) {
		b, _ := os.ReadFile(s.path(project, p.Slug))
		for i, l := range strings.Split(string(b), "\n") {
			if strings.Contains(strings.ToLower(l), q) {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", p.Slug, i+1, strings.TrimSpace(l)))
				if len(hits) >= max {
					return hits
				}
			}
		}
	}
	return hits
}

// ---- proposals ----

func (s *Store) propDir(project string) string { return filepath.Join(s.dir(project), ".proposals") }

func (s *Store) Propose(p Proposal) (Proposal, error) {
	p.Slug = Slug(p.Slug)
	if !slugRe.MatchString(p.Slug) {
		return p, fmt.Errorf("bad page name %q", p.Slug)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	p.ID = fmt.Sprintf("%s-%d", time.Now().UTC().Format("20060102150405"), s.seq)
	p.Created, p.Status = time.Now(), "pending"
	os.MkdirAll(s.propDir(p.Project), 0o755)
	b, _ := json.MarshalIndent(p, "", "  ")
	return p, os.WriteFile(filepath.Join(s.propDir(p.Project), p.ID+".json"), b, 0o644)
}

func (s *Store) Proposals(project string) []Proposal {
	ents, _ := os.ReadDir(s.propDir(project))
	var out []Proposal
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(s.propDir(project), e.Name()))
		if err != nil {
			continue
		}
		var p Proposal
		if json.Unmarshal(b, &p) == nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

func (s *Store) Proposal(project, id string) (Proposal, error) {
	b, err := os.ReadFile(filepath.Join(s.propDir(project), filepath.Base(id)+".json"))
	if err != nil {
		return Proposal{}, fmt.Errorf("no proposal %s", id)
	}
	var p Proposal
	return p, json.Unmarshal(b, &p)
}

// Resolve accepts (writing the page) or rejects a pending proposal.
func (s *Store) Resolve(project, id string, accept bool) (Proposal, error) {
	p, err := s.Proposal(project, id)
	if err != nil {
		return p, err
	}
	if p.Status != "pending" {
		return p, fmt.Errorf("proposal already %s", p.Status)
	}
	if accept {
		if _, err := s.Write(project, p.Slug, p.Content); err != nil {
			return p, err
		}
		p.Status = "accepted"
	} else {
		p.Status = "rejected"
	}
	b, _ := json.MarshalIndent(p, "", "  ")
	return p, os.WriteFile(filepath.Join(s.propDir(project), p.ID+".json"), b, 0o644)
}
