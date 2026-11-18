package app

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/wiki"
)

// wikiProject picks the project whose wiki a call is about.
func (a *App) wikiProject(c *Ctx, m map[string]any) (string, error) {
	if c.inProject() {
		if p := sArg(m, "project"); p != "" && p != c.Project.ID {
			return "", deny("you work in project %s; its wiki is the one you can use", c.Project.ID)
		}
		return c.Project.ID, nil
	}
	p, err := a.projectArg(c, m)
	return p.ID, err
}

func (a *App) wikiTools() []Tool {
	HPT := []string{KindHome, KindProject, KindTask}
	return []Tool{
		{Name: "wiki_list", Label: "Wiki pages", Kinds: HPT,
			Description: "List the project wiki's pages with their one-line summaries (and which may be stale).",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (home agent only)")}), run: toolWikiList},
		{Name: "wiki_read", Label: "Read wiki page", Kinds: HPT,
			Description: "Read a project wiki page (markdown with frontmatter).",
			Parameters:  obj([]string{"page"}, map[string]any{"page": sprop("page name, e.g. 'index' or 'how-sandboxing-works'"), "project": sprop("project id (home agent only)")}), run: toolWikiRead},
		{Name: "wiki_search", Label: "Search wiki", Kinds: HPT,
			Description: "Search the project wiki for a phrase. The home agent can search every project with project='all'.",
			Parameters:  obj([]string{"query"}, map[string]any{"query": sprop("text to find"), "project": sprop("project id, or 'all' (home agent)")}), run: toolWikiSearch},
		{Name: "wiki_write", Label: "Write wiki page", Kinds: []string{KindProject},
			Description: "Create or replace a page in your project's wiki (the previous version is kept). Use for durable knowledge: what the project is, decisions with their reasons, how things work, gotchas, people and vendors. Keep pages short and factual; link pages with [[page-name]]. Always give a one-line summary and the sources the page is based on.",
			Parameters: obj([]string{"page", "content", "summary"}, map[string]any{
				"page": sprop("lowercase-dashed name; 'index' is the front page"), "content": sprop("markdown body (no frontmatter; start with '# Title')"),
				"summary": sprop("one line: what this page tells a reader"), "sources": aprop("app paths it's based on, e.g. ['nexus/README.md']", sprop("")),
			}), run: toolWikiWrite},
		{Name: "wiki_propose", Label: "Propose wiki change", Kinds: []string{KindHome, KindTask},
			Description: "Propose a new or changed wiki page. The user reviews and accepts or rejects it. Send the complete new page body, the reason, and sources.",
			Parameters: obj([]string{"page", "content", "summary", "reason"}, map[string]any{
				"page": sprop("page name"), "content": sprop("complete new markdown body (start with '# Title')"), "summary": sprop("one line"),
				"reason": sprop("why: what was learned and from where"), "sources": aprop("app paths", sprop("")), "project": sprop("project id (home agent only)"),
			}), run: toolWikiPropose},
		{Name: "plan_set", Label: "Set plan", Kinds: []string{KindTask},
			Description: "Pin your plan for this task as a short checklist (3-7 steps). The user sees it live. Call it first, and again if the plan changes.",
			Parameters:  obj([]string{"steps"}, map[string]any{"steps": aprop("step descriptions in order", sprop(""))}), run: toolPlanSet},
		{Name: "plan_update", Label: "Update plan", Kinds: []string{KindTask},
			Description: "Mark a plan step as doing, done or skipped (with an optional short note).",
			Parameters: obj([]string{"step", "status"}, map[string]any{
				"step": iprop("1-based step number"), "status": eprop("", "doing", "done", "skipped"), "note": sprop("optional"),
			}), run: toolPlanUpdate},
	}
}

func toolWikiList(a *App, c *Ctx, m map[string]any) (string, error) {
	pid, err := a.wikiProject(c, m)
	if err != nil {
		return "", err
	}
	pages := a.wikiPages(pid)
	if len(pages) == 0 {
		return "the wiki is empty", nil
	}
	var b strings.Builder
	for _, p := range pages {
		stale := ""
		if len(p.Stale) > 0 {
			stale = " [may be stale: " + strings.Join(p.Stale, ", ") + " changed]"
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", p.Slug, p.Meta.Summary, stale)
	}
	return b.String(), nil
}

func toolWikiRead(a *App, c *Ctx, m map[string]any) (string, error) {
	pid, err := a.wikiProject(c, m)
	if err != nil {
		return "", err
	}
	return a.Wiki.Read(pid, sArg(m, "page"))
}

func toolWikiSearch(a *App, c *Ctx, m map[string]any) (string, error) {
	q := sArg(m, "query")
	var targets []string
	if c.Kind == KindHome && (sArg(m, "project") == "" || sArg(m, "project") == "all") {
		for _, p := range a.visibleProjects(c.Project.ID) {
			targets = append(targets, p.ID)
		}
	} else {
		pid, err := a.wikiProject(c, m)
		if err != nil {
			return "", err
		}
		targets = []string{pid}
	}
	var hits []string
	for _, pid := range targets {
		for _, h := range a.Wiki.Search(pid, q, 40) {
			hits = append(hits, pid+"/"+h)
		}
	}
	if len(hits) == 0 {
		return "no matches for " + q, nil
	}
	return strings.Join(hits, "\n"), nil
}

func (a *App) wikiContent(c *Ctx, pid string, m map[string]any) string {
	// Sources become app paths ("<project>/README.md") so the wiki check can
	// tell when they change, however the agent wrote them.
	var srcs []string
	for _, s := range strs(m["sources"]) {
		s = a.appPath(strings.TrimSpace(s))
		if _, ok := a.Roots.Dir(rootOf(s)); !ok && s != "" {
			s = pid + "/" + strings.TrimPrefix(s, "./")
		}
		srcs = append(srcs, s)
	}
	meta := wiki.Meta{Summary: sArg(m, "summary"), Sources: srcs, Verified: time.Now(), UpdatedBy: c.Actor}
	_, body := wiki.Parse(str(m["content"])) // tolerate frontmatter in content
	return wiki.Render(meta, body)
}

func toolWikiWrite(a *App, c *Ctx, m map[string]any) (string, error) {
	slug, err := a.Wiki.Write(c.Project.ID, sArg(m, "page"), a.wikiContent(c, c.Project.ID, m))
	if err != nil {
		return "", err
	}
	a.broadcast(map[string]any{"t": "fileChanged", "path": "wiki/" + c.Project.ID + "/" + slug + ".md"})
	a.broadcastDesk()
	return fmt.Sprintf("wrote wiki page %s (previous version kept)", slug), nil
}

func toolWikiPropose(a *App, c *Ctx, m map[string]any) (string, error) {
	pid, err := a.wikiProject(c, m)
	if err != nil {
		return "", err
	}
	p, err := a.Wiki.Propose(wiki.Proposal{Project: pid, Slug: sArg(m, "page"), Content: a.wikiContent(c, pid, m),
		Reason: sArg(m, "reason"), Sources: strs(m["sources"]), By: c.Actor, Task: taskOf(c)})
	if err != nil {
		return "", err
	}
	a.broadcastDesk()
	return fmt.Sprintf("proposed a change to wiki page %s (%s); the user will accept or reject it", p.Slug, p.ID), nil
}

func taskOf(c *Ctx) string {
	if c.Run != nil {
		return c.Run.Task
	}
	return ""
}

func toolPlanSet(a *App, c *Ctx, m map[string]any) (string, error) {
	steps := strs(m["steps"])
	if len(steps) == 0 {
		return "", errors.New("give at least one step")
	}
	_, err := a.Desk.UpdateTask(c.Run.Task, func(t *desk.Task) error {
		old := t.Plan
		t.Plan = nil
		for i, s := range steps {
			st := desk.PlanStep{Text: s, Status: "todo"}
			if i < len(old) && old[i].Text == s { // keep progress on unchanged steps
				st = old[i]
			}
			t.Plan = append(t.Plan, st)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("plan pinned (%d steps)", len(steps)), nil
}

func toolPlanUpdate(a *App, c *Ctx, m map[string]any) (string, error) {
	n := iArg(m, "step", 0)
	_, err := a.Desk.UpdateTask(c.Run.Task, func(t *desk.Task) error {
		if n < 1 || n > len(t.Plan) {
			return fmt.Errorf("no step %d (plan has %d)", n, len(t.Plan))
		}
		t.Plan[n-1].Status = sArg(m, "status")
		if note := sArg(m, "note"); note != "" {
			t.Plan[n-1].Note = truncate(note, 200)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return "ok", nil
}

// ---- wiki bootstrap and staleness ----

func (a *App) wikiPages(pid string) []wiki.Page {
	pages := a.Wiki.List(pid)
	if !a.addonOn(pid, "wiki-check") {
		return pages
	}
	for i, p := range pages {
		if p.Meta.Verified.IsZero() {
			continue
		}
		for _, src := range p.Meta.Sources {
			abs, err := a.Roots.Resolve(src)
			if err != nil {
				continue
			}
			if st, err := statMod(abs); err == nil && st.After(p.Meta.Verified.Add(time.Minute)) {
				pages[i].Stale = append(pages[i].Stale, src)
			}
		}
	}
	return pages
}

// wikiBootstrap is the wiki part of every project agent's context: the
// index page plus one line per page. Kept small on purpose.
func (a *App) wikiBootstrap(pid string) string {
	pages := a.wikiPages(pid)
	if len(pages) == 0 {
		return "project wiki: EMPTY. If the user wants it, build it (see your instructions on the wiki).\n"
	}
	var b strings.Builder
	if idx, err := a.Wiki.Read(pid, "index"); err == nil {
		_, body := wiki.Parse(idx)
		fmt.Fprintf(&b, "project wiki index (wiki/%s/index.md):\n%s\n", pid, truncate(strings.TrimSpace(body), 6000))
	}
	b.WriteString("wiki pages (read with wiki_read):\n")
	for _, p := range pages {
		if p.Slug == "index" {
			continue
		}
		st := ""
		if len(p.Stale) > 0 {
			st = " (may be stale)"
		}
		fmt.Fprintf(&b, "- %s: %s%s\n", p.Slug, p.Meta.Summary, st)
	}
	return b.String()
}

func statMod(abs string) (time.Time, error) {
	st, err := os.Stat(abs)
	if err != nil {
		return time.Time{}, err
	}
	return st.ModTime(), nil
}
