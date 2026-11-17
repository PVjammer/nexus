// Package projects reads your real projects: their brief, foreman ledger
// (workstreams, backlog, inbox, events) and git state. It is read-only on
// the project directories; Nexus keeps its own state elsewhere.
//
// Prototype note: foreman's on-disk format is read directly. In the full
// design foreman sits behind a workstream-engine interface.
package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Project is one registered project. Path is a directory on your machine.
type Project struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Brief string    `json:"brief,omitempty"` // set by you or the home agent; else read from the README
	Space  string          `json:"space"`            // the layout space for this project
	Addons map[string]bool `json:"addons,omitempty"` // add-ons switched on/off (unset = the add-on's default)
	Added time.Time `json:"added"`
}

type Registry struct {
	mu   sync.Mutex
	path string
	list []Project
}

func OpenRegistry(path string) (*Registry, error) {
	r := &Registry{path: path}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &r.list); err != nil {
			return nil, fmt.Errorf("projects: parse %s: %w", path, err)
		}
	}
	return r, nil
}

func (r *Registry) saveLocked() error {
	b, _ := json.MarshalIndent(r.list, "", "  ")
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *Registry) List() []Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Project(nil), r.list...)
}

func (r *Registry) Get(id string) (Project, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.list {
		if p.ID == id {
			return p, true
		}
	}
	return Project{}, false
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func Slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		s = "project"
	}
	if len(s) > 24 {
		s = strings.Trim(s[:24], "-")
	}
	return s
}

// Add registers a directory as a project. newSpace makes its layout space.
func (r *Registry) Add(name, dir, brief string, newSpace func(name string) string) (Project, error) {
	abs, err := filepath.Abs(expandHome(dir))
	if err != nil {
		return Project{}, err
	}
	st, err := os.Stat(abs)
	if err != nil || !st.IsDir() {
		return Project{}, fmt.Errorf("%s is not a directory", abs)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.list {
		if p.Path == abs {
			return p, fmt.Errorf("already added as %q (%s)", p.Name, p.ID)
		}
	}
	id := Slug(name)
	for n := 2; r.hasLocked(id); n++ {
		id = fmt.Sprintf("%s-%d", Slug(name), n)
	}
	p := Project{ID: id, Name: name, Path: abs, Brief: strings.TrimSpace(brief), Added: time.Now()}
	r.mu.Unlock()
	p.Space = newSpace(name)
	r.mu.Lock()
	r.list = append(r.list, p)
	return p, r.saveLocked()
}

func (r *Registry) hasLocked(id string) bool {
	for _, p := range r.list {
		if p.ID == id {
			return true
		}
	}
	return id == "desk" || id == "home"
}

func (r *Registry) Update(id string, f func(*Project)) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.list {
		if r.list[i].ID == id {
			f(&r.list[i])
			return r.list[i], r.saveLocked()
		}
	}
	return Project{}, fmt.Errorf("no project %q", id)
}

func (r *Registry) Remove(id string) (Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, p := range r.list {
		if p.ID == id {
			r.list = append(r.list[:i], r.list[i+1:]...)
			return p, r.saveLocked()
		}
	}
	return Project{}, fmt.Errorf("no project %q", id)
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

// ---- reading a project ----

type Workstream struct {
	Name         string    `json:"name"`
	Kind         string    `json:"kind,omitempty"`
	Goal         string    `json:"goal,omitempty"`
	AgentStatus  string    `json:"agentStatus,omitempty"`  // working | idle | exited…
	ReportStatus string    `json:"reportStatus,omitempty"` // done | needs-input | failed
	Summary      string    `json:"summary,omitempty"`
	Updated      time.Time `json:"updated"`
	Closed       bool      `json:"closed,omitempty"`
	Report       string    `json:"report,omitempty"` // path relative to the project root
	Harness      string    `json:"harness,omitempty"`
}

type Event struct {
	Time    time.Time `json:"time"`
	WS      string    `json:"ws,omitempty"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
}

type InboxItem struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	WS      string    `json:"ws,omitempty"`
	Kind    string    `json:"kind"`
	Summary string    `json:"summary"`
}

type BacklogItem struct {
	Text string `json:"text"`
	Done bool   `json:"done,omitempty"`
}

type Commit struct {
	Hash    string    `json:"hash"`
	When    time.Time `json:"when"`
	Subject string    `json:"subject"`
}

type Git struct {
	Branch  string   `json:"branch"`
	Dirty   int      `json:"dirty"`
	Commits []Commit `json:"commits,omitempty"`
}

// Detail is everything Nexus shows about a project.
type Detail struct {
	Project
	Exists       bool          `json:"exists"`
	Readme       string        `json:"readme,omitempty"` // first paragraph, when no brief is set
	Foreman      bool          `json:"foreman"`
	Workstreams  []Workstream  `json:"workstreams,omitempty"`
	Archived     int           `json:"archived,omitempty"`
	Backlog      []BacklogItem `json:"backlog,omitempty"`
	Inbox        []InboxItem   `json:"inbox,omitempty"` // unacknowledged in foreman
	Events       []Event       `json:"events,omitempty"`
	Git          *Git          `json:"git,omitempty"`
	Docs         []string      `json:"docs,omitempty"` // markdown files, relative paths
	LastActivity time.Time     `json:"lastActivity"`
}

// Read gathers a project's detail. It never writes to the project.
func Read(p Project) Detail {
	d := Detail{Project: p}
	if st, err := os.Stat(p.Path); err != nil || !st.IsDir() {
		return d
	}
	d.Exists = true
	d.Readme = readmeIntro(p.Path)
	fm := filepath.Join(p.Path, ".foreman")
	if st, err := os.Stat(fm); err == nil && st.IsDir() {
		d.Foreman = true
		d.Workstreams = readWorkstreams(p.Path)
		if ents, err := os.ReadDir(filepath.Join(fm, "archive")); err == nil {
			d.Archived = len(ents)
		}
		d.Backlog = readBacklog(filepath.Join(fm, "backlog.md"))
		d.Inbox = readInbox(fm)
		d.Events = readEvents(filepath.Join(fm, "events.jsonl"), 80)
	}
	d.Git = readGit(p.Path)
	d.Docs = findDocs(p.Path)
	for _, e := range d.Events {
		if e.Time.After(d.LastActivity) {
			d.LastActivity = e.Time
		}
	}
	for _, w := range d.Workstreams {
		if w.Updated.After(d.LastActivity) {
			d.LastActivity = w.Updated
		}
	}
	if d.Git != nil && len(d.Git.Commits) > 0 && d.Git.Commits[0].When.After(d.LastActivity) {
		d.LastActivity = d.Git.Commits[0].When
	}
	return d
}

func readmeIntro(dir string) string {
	for _, n := range []string{"README.md", "readme.md", "README", "AGENTS.md"} {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var para []string
		for _, line := range strings.Split(string(b), "\n") {
			t := strings.TrimSpace(line)
			if t == "" {
				if len(para) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "![") || strings.HasPrefix(t, "[!") || strings.HasPrefix(t, "<") {
				if len(para) > 0 {
					break
				}
				continue
			}
			para = append(para, t)
		}
		s := strings.Join(para, " ")
		if len(s) > 400 {
			s = s[:400] + "…"
		}
		if s != "" {
			return s
		}
	}
	return ""
}

func readWorkstreams(root string) []Workstream {
	base := filepath.Join(root, ".foreman", "ws")
	ents, _ := os.ReadDir(base)
	var out []Workstream
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(base, e.Name())
		w := Workstream{Name: e.Name()}
		var meta struct {
			Kind    string `json:"kind"`
			Goal    string `json:"goal"`
			Closed  bool   `json:"closed"`
			Harness string `json:"harness"`
		}
		if b, err := os.ReadFile(filepath.Join(dir, "meta.json")); err == nil && json.Unmarshal(b, &meta) == nil {
			w.Kind, w.Goal, w.Closed, w.Harness = meta.Kind, meta.Goal, meta.Closed, meta.Harness
		}
		var st struct {
			AgentStatus  string    `json:"agent_status"`
			ReportStatus string    `json:"report_status"`
			Summary      string    `json:"summary"`
			Updated      time.Time `json:"updated"`
		}
		if b, err := os.ReadFile(filepath.Join(dir, "status.json")); err == nil && json.Unmarshal(b, &st) == nil {
			w.AgentStatus, w.ReportStatus, w.Summary, w.Updated = st.AgentStatus, st.ReportStatus, st.Summary, st.Updated
		}
		if _, err := os.Stat(filepath.Join(dir, "report.md")); err == nil {
			w.Report = ".foreman/ws/" + e.Name() + "/report.md"
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

var checkRe = regexp.MustCompile(`^\s*[-*]\s+\[( |x|X)\]\s+(.*)$`)

func readBacklog(path string) []BacklogItem {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []BacklogItem
	for _, line := range strings.Split(string(b), "\n") {
		m := checkRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, BacklogItem{Text: strings.TrimSpace(m[2]), Done: m[1] != " "})
	}
	return out
}

// jsonl reads a JSONL file leniently: raw control characters inside strings
// (which some writers leave in) are replaced, and bad lines are skipped.
func jsonl(path string, f func([]byte)) {
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.Map(func(r rune) rune {
			if r < 0x20 {
				return ' '
			}
			return r
		}, sc.Bytes())
		if len(bytes.TrimSpace(line)) > 0 {
			f(line)
		}
	}
}

func readInbox(fm string) []InboxItem {
	acked := map[string]bool{}
	jsonl(filepath.Join(fm, "acks.jsonl"), func(b []byte) {
		var a struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(b, &a) == nil {
			acked[a.ID] = true
		}
	})
	var out []InboxItem
	jsonl(filepath.Join(fm, "inbox.jsonl"), func(b []byte) {
		var it InboxItem
		if json.Unmarshal(b, &it) == nil && it.ID != "" && !acked[it.ID] {
			out = append(out, it)
		}
	})
	// Newest first; one entry per workstream (the latest) is what needs you.
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	seen := map[string]bool{}
	var dedup []InboxItem
	for _, it := range out {
		k := it.WS
		if k == "" {
			k = it.ID
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		dedup = append(dedup, it)
	}
	return dedup
}

func readEvents(path string, max int) []Event {
	var all []Event
	jsonl(path, func(b []byte) {
		var e Event
		if json.Unmarshal(b, &e) == nil && !e.Time.IsZero() {
			all = append(all, e)
		}
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Time.After(all[j].Time) })
	if len(all) > max {
		all = all[:max]
	}
	return all
}

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0") // never take the index lock
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}

func readGit(dir string) *Git {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return nil
	}
	g := &Git{}
	g.Branch, _ = git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if s, err := git(dir, "status", "--porcelain", "--untracked-files=normal"); err == nil && s != "" {
		g.Dirty = len(strings.Split(s, "\n"))
	}
	if s, err := git(dir, "log", "-8", "--format=%h%x09%ct%x09%s"); err == nil && s != "" {
		for _, line := range strings.Split(s, "\n") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			ts, _ := strconv.ParseInt(parts[1], 10, 64)
			g.Commits = append(g.Commits, Commit{Hash: parts[0], When: time.Unix(ts, 0), Subject: parts[2]})
		}
	}
	return g
}

var skipDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "bin": true, ".foreman": true, "archive": true, "target": true, ".venv": true, "__pycache__": true}

func findDocs(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			if p != root && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") || depth >= 2) {
				return filepath.SkipDir
			}
			// Nested repos are their own projects.
			if p != root {
				if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".md") && len(out) < 40 {
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := strings.Count(out[i], "/"), strings.Count(out[j], "/")
		if di != dj {
			return di < dj
		}
		return out[i] < out[j]
	})
	return out
}

// ---- discovery (onboarding) ----

type Candidate struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Foreman bool   `json:"foreman"`
	Git     bool   `json:"git"`
	Readme  string `json:"readme,omitempty"`
}

// Discover walks the given directories (bounded depth) for folders that look
// like projects: a git repo with a README. With foreman set, a foreman ledger
// also counts (and sorts first).
func Discover(roots []string, maxDepth int, foreman bool) []Candidate {
	var out []Candidate
	seen := map[string]bool{}
	for _, r := range roots {
		r = expandHome(r)
		base, err := filepath.Abs(r)
		if err != nil {
			continue
		}
		filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(base, p)
			depth := 0
			if rel != "." {
				depth = strings.Count(filepath.ToSlash(rel), "/") + 1
			}
			if p != base && (skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			if depth > maxDepth {
				return filepath.SkipDir
			}
			if seen[p] {
				return nil
			}
			c := Candidate{Path: p, Name: d.Name()}
			if st, err := os.Stat(filepath.Join(p, ".foreman", "config.toml")); foreman && err == nil && !st.IsDir() {
				c.Foreman = true
			}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				c.Git = true
			}
			if c.Foreman || c.Git {
				c.Readme = readmeIntro(p)
				if len(c.Readme) > 160 {
					c.Readme = c.Readme[:160] + "…"
				}
				if c.Foreman || c.Readme != "" {
					seen[p] = true
					out = append(out, c)
				}
			}
			return nil
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Foreman != out[j].Foreman {
			return out[i].Foreman
		}
		return out[i].Path < out[j].Path
	})
	return out
}

var ErrNotFound = errors.New("not found")
