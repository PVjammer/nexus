package app

// Project add-ons: optional tools and automations you switch on per project
// (the code graph, the wiki check, the foreman ledger view). Each has a
// default that depends on what the project looks like.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
)

type Addon struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // tool | automation | view
	defaultOn   func(a *App, d projects.Detail) bool
	available   func() (bool, string)
}

func (a *App) addons() []Addon {
	return []Addon{
		{ID: "codegraph", Name: "Code graph", Kind: "tool",
			Description: "Agents can explore code by symbol: callers, callees, impact, and source with context (codegraph, tree-sitter + SQLite). Nexus indexes its own mirror of your git-tracked files (secret-looking files and files over 1 MB skipped), so your repo is never written to. Telemetry is off.",
			defaultOn:   func(a *App, d projects.Detail) bool { return d.Git != nil },
			available: func() (bool, string) {
				if codegraphBin() == "" {
					return false, "codegraph isn't installed (npm i -g @colbymchenry/codegraph)"
				}
				return true, ""
			}},
		{ID: "wiki-check", Name: "Wiki check", Kind: "automation",
			Description: "Flags wiki pages whose sources changed after the page was last verified, so the wiki doesn't quietly go stale.",
			defaultOn:   func(a *App, d projects.Detail) bool { return true }},
		{ID: "foreman", Name: "Foreman ledger", Kind: "view",
			Description: "Shows this project's foreman workstreams, backlog and inbox (read-only) and tells agents about them.",
			defaultOn:   func(a *App, d projects.Detail) bool { return d.Foreman }},
	}
}

func (a *App) addonOn(pid, id string) bool {
	p, ok := a.Projects.Get(pid)
	if !ok {
		return false
	}
	for _, ad := range a.addons() {
		if ad.ID != id {
			continue
		}
		if ad.available != nil {
			if ok, _ := ad.available(); !ok {
				return false
			}
		}
		if v, set := p.Addons[id]; set {
			return v
		}
		d, _ := a.detail(pid)
		return ad.defaultOn(a, d)
	}
	return false
}

// addonStates is what the UI shows per project.
func (a *App) addonStates(pid string) []map[string]any {
	var out []map[string]any
	for _, ad := range a.addons() {
		e := map[string]any{"id": ad.ID, "name": ad.Name, "description": ad.Description, "kind": ad.Kind, "on": a.addonOn(pid, ad.ID)}
		if ad.available != nil {
			if ok, why := ad.available(); !ok {
				e["unavailable"] = why
			}
		}
		if ad.ID == "codegraph" {
			if st, ok := a.cg.status(pid); ok {
				e["status"] = st
			}
		}
		out = append(out, e)
	}
	return out
}

func (a *App) setAddon(pid, id string, on bool) error {
	_, err := a.Projects.Update(pid, func(p *projects.Project) {
		if p.Addons == nil {
			p.Addons = map[string]bool{}
		}
		p.Addons[id] = on
	})
	if err != nil {
		return err
	}
	// Tool lists are fixed when an agent starts; restart the project's agent
	// (it resumes its session, so nothing is forgotten).
	if r := a.existingRun("p:" + pid); r != nil && !isBusy(r) {
		r.Close()
	}
	if id == "codegraph" && on {
		go a.cg.syncProject(a, pid)
	}
	a.broadcastDesk()
	return nil
}

func isBusy(r *Run) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.busy }

// ---- codegraph ----

func codegraphBin() string {
	if p, err := exec.LookPath("codegraph"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".local", "bin", "codegraph")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

type cgState struct {
	LastSync time.Time `json:"lastSync"`
	Files    int       `json:"files"`
	Error    string    `json:"error,omitempty"`
	Syncing  bool      `json:"syncing,omitempty"`
}

type codeGraph struct {
	mu    sync.Mutex
	state map[string]*cgState
	locks map[string]*sync.Mutex
}

func newCodeGraph() *codeGraph {
	return &codeGraph{state: map[string]*cgState{}, locks: map[string]*sync.Mutex{}}
}

func (g *codeGraph) status(pid string) (cgState, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.state[pid]
	if !ok {
		return cgState{}, false
	}
	return *s, true
}

func (g *codeGraph) lock(pid string) *sync.Mutex {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locks[pid] == nil {
		g.locks[pid] = &sync.Mutex{}
	}
	return g.locks[pid]
}

func (g *codeGraph) set(pid string, f func(s *cgState)) {
	g.mu.Lock()
	if g.state[pid] == nil {
		g.state[pid] = &cgState{}
	}
	f(g.state[pid])
	g.mu.Unlock()
}

func (a *App) cgMirror(pid string) string { return filepath.Join(a.cfg.DataDir, "codegraph", pid) }

func cgEnv() []string {
	return append(os.Environ(), "DO_NOT_TRACK=1", "CODEGRAPH_TELEMETRY=0", "CODEGRAPH_NO_UPDATE_CHECK=1",
		"CODEGRAPH_NO_DAEMON=1", "CODEGRAPH_NO_WATCH=1", "NO_COLOR=1")
}

func runCodegraph(dir string, timeout time.Duration, args ...string) (string, error) {
	bin := codegraphBin()
	if bin == "" {
		return "", errors.New("codegraph isn't installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.Env = cgEnv()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("codegraph %s timed out", args[0])
	}
	return string(out), err
}

// syncProject refreshes the mirror from the project's git-tracked (and
// untracked, not ignored) files, then updates the index.
func (g *codeGraph) syncProject(a *App, pid string) {
	p, ok := a.Projects.Get(pid)
	if !ok || !a.addonOn(pid, "codegraph") {
		return
	}
	l := g.lock(pid)
	if !l.TryLock() {
		return
	}
	defer l.Unlock()
	g.set(pid, func(s *cgState) { s.Syncing = true })
	defer func() { g.set(pid, func(s *cgState) { s.Syncing = false }); a.broadcastDesk() }()
	dst := a.cgMirror(pid)
	n, err := mirrorTracked(p.Path, dst)
	if err == nil {
		args := []string{"sync", "-q", "."}
		if _, statErr := os.Stat(filepath.Join(dst, ".codegraph", "codegraph.db")); statErr != nil {
			args = []string{"init", "-y", "."}
		}
		if out, cgErr := runCodegraph(dst, 5*time.Minute, args...); cgErr != nil {
			err = fmt.Errorf("%v: %s", cgErr, truncate(strings.TrimSpace(out), 300))
		}
	}
	g.set(pid, func(s *cgState) {
		s.Files, s.LastSync = n, time.Now()
		s.Error = ""
		if err != nil {
			s.Error = err.Error()
		}
	})
}

func (a *App) codegraphLoop() {
	t := time.NewTicker(2 * time.Minute)
	sweep := func() {
		for _, p := range a.Projects.List() {
			if a.addonOn(p.ID, "codegraph") {
				a.cg.syncProject(a, p.ID)
			}
		}
	}
	time.Sleep(3 * time.Second)
	sweep()
	for range t.C {
		sweep()
	}
}

// mirrorTracked copies src's git-visible files into dst (changed files only)
// and removes files that disappeared. Secret-looking and large files are
// never copied.
func mirrorTracked(src, dst string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", src, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("not a git repo, or git failed: %v", err)
	}
	want := map[string]bool{}
	n := 0
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || files.LooksSecret(rel) || strings.HasPrefix(rel, ".codegraph") {
			continue
		}
		s := filepath.Join(src, rel)
		st, err := os.Lstat(s)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
			continue
		}
		want[filepath.ToSlash(rel)] = true
		n++
		d := filepath.Join(dst, rel)
		if dt, err := os.Stat(d); err == nil && dt.Size() == st.Size() && dt.ModTime().Equal(st.ModTime()) {
			continue
		}
		if err := copyFile(s, d); err == nil {
			os.Chtimes(d, st.ModTime(), st.ModTime())
		}
	}
	filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		if d.IsDir() {
			if rel == ".codegraph" || strings.HasPrefix(rel, ".codegraph") {
				return filepath.SkipDir
			}
			return nil
		}
		if !want[filepath.ToSlash(rel)] {
			os.Remove(p)
		}
		return nil
	})
	return n, nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst + ".tmp")
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	out.Close()
	return os.Rename(dst+".tmp", dst)
}

// codeTool runs a codegraph query against the project's mirror.
func (a *App) codeTool(c *Ctx, args ...string) (string, error) {
	if !c.inProject() {
		return "", errors.New("code tools work inside a project")
	}
	if !a.addonOn(c.Project.ID, "codegraph") {
		return "", deny("the code graph add-on is off for %s (the user can turn it on in the project's Add-ons)", c.Project.ID)
	}
	dir := a.cgMirror(c.Project.ID)
	if _, err := os.Stat(filepath.Join(dir, ".codegraph", "codegraph.db")); err != nil {
		go a.cg.syncProject(a, c.Project.ID)
		return "", errors.New("the code graph is still being built; try again in a minute, or use search/file_read meanwhile")
	}
	out, err := runCodegraph(dir, 60*time.Second, args...)
	out = strings.TrimSpace(out)
	if err != nil && out == "" {
		return "", err
	}
	note := fmt.Sprintf("(paths are relative to the project root; read files as %q)\n", c.Project.ID+"/<path>")
	return note + truncate(out, 24000), nil
}

func (a *App) codeTools() []Tool {
	PT := []string{KindProject, KindTask}
	return []Tool{
		{Name: "code_explore", Label: "Explore code", Kinds: PT, Addon: "codegraph",
			Description: "Explore an area of the codebase by describing it in words: returns the relevant symbols, their source with line numbers, call paths and what depends on them. Start here for 'how does X work' or before changing code.",
			Parameters:  obj([]string{"query"}, map[string]any{"query": sprop("what you're looking for, e.g. 'how task reports are written'"), "max_files": iprop("files to include source from (default 4)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				return a.codeTool(c, "explore", sArg(m, "query"), "--max-files", fmt.Sprint(iArg(m, "max_files", 4)))
			}},
		{Name: "code_symbols", Label: "Find symbols", Kinds: PT, Addon: "codegraph",
			Description: "Find functions, types and methods by name.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("symbol name or part of it"), "kind": sprop("function, class, method, … (optional)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				args := []string{"query", sArg(m, "name"), "--limit", "15"}
				if k := sArg(m, "kind"); k != "" {
					args = append(args, "--kind", k)
				}
				return a.codeTool(c, args...)
			}},
		{Name: "code_symbol", Label: "Symbol detail", Kinds: PT, Addon: "codegraph",
			Description: "One symbol's source plus its callers and callees.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("symbol name"), "file": sprop("disambiguate by file (optional)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				args := []string{"node", sArg(m, "name")}
				if f := sArg(m, "file"); f != "" {
					args = append(args, "--file", strings.TrimPrefix(f, c.Project.ID+"/"))
				}
				return a.codeTool(c, args...)
			}},
		{Name: "code_callers", Label: "Callers", Kinds: PT, Addon: "codegraph",
			Description: "Everything that calls a function or method.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("symbol name")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				return a.codeTool(c, "callers", sArg(m, "name"))
			}},
		{Name: "code_impact", Label: "Impact", Kinds: PT, Addon: "codegraph",
			Description: "What code is affected if a symbol changes (blast radius).",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("symbol name")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				return a.codeTool(c, "impact", sArg(m, "name"))
			}},
	}
}
