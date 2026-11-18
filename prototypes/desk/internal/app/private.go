package app

// Private projects: data that belongs to you alone. A private project's work
// items, tasks, notes, files, wiki and conversations are hidden from every
// agent outside it: the home agent (and so the quick window), Eitri, and
// other projects' agents. They see only that it exists, with counts and
// day-only due dates, like "busy" on someone's calendar, so no agent tells
// you you're free when you aren't. Its own agents work inside the wall as
// usual. You see everything; private items are marked in shared widgets.
//
// The wall is in the core, at three places:
//   - every tool call: arguments that name something private (a project, a
//     work item, a task, a path) get the same answer as something that
//     doesn't exist (guardArgs, in CallTool);
//   - tools and contexts that list across projects skip private ones and add
//     the "busy" line (visibleProjects, busyNote);
//   - a last line of defence scrubs any line of a tool result or context
//     that still names a private item, task, path or project (scrub).
// TestPrivateProjectWall calls every outside agent's tools against it.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
)

// walled: the caller (by its project; "" = home, Eitri, plugins) can't see pid.
func (a *App) walled(callerProject, pid string) bool {
	if pid == "" || pid == callerProject {
		return false
	}
	m := a.priv.Load()
	return m != nil && (*m)[pid]
}

func (a *App) anyPrivate() bool { m := a.priv.Load(); return m != nil && len(*m) > 0 }

func (a *App) loadPrivate() {
	m := map[string]bool{}
	for pid, on := range a.Desk.Snapshot().Private {
		if on {
			m[pid] = true
		}
	}
	a.priv.Store(&m)
}

func runProject(r *Run) string {
	if r == nil {
		return ""
	}
	return r.Project
}

// visibleProjects is the project list as this caller may see it.
func (a *App) visibleProjects(caller string) []projects.Project {
	var out []projects.Project
	for _, p := range a.Projects.List() {
		if !a.walled(caller, p.ID) {
			out = append(out, p)
		}
	}
	return out
}

func (a *App) visibleIDs(caller string) string {
	var ids []string
	for _, p := range a.visibleProjects(caller) {
		ids = append(ids, p.ID)
	}
	return strings.Join(ids, ", ")
}

// pathWalled: an app path inside a private project's folder, desk files or wiki.
func (a *App) pathWalled(caller, p string) bool {
	p = strings.TrimPrefix(p, "/")
	r := rootOf(p)
	if a.walled(caller, r) {
		return true
	}
	if r == "desk" || r == "wiki" {
		rest := strings.TrimPrefix(strings.TrimPrefix(p, r), "/")
		return a.walled(caller, rootOf(rest))
	}
	return false
}

// guardArgs answers a call that names something private as if it didn't exist.
func (a *App) guardArgs(r *Run, args map[string]any) error {
	caller := runProject(r)
	if !a.anyPrivate() {
		return nil
	}
	for _, k := range []string{"project", "scope"} {
		if pid := strings.TrimPrefix(sArg(args, k), "p:"); a.walled(caller, pid) {
			return fixErr("no project %q (projects: %s)", pid, a.visibleIDs(caller))
		}
	}
	for _, k := range []string{"item", "ref"} {
		if v := sArg(args, k); v != "" {
			if w, err := a.checkItemRef(v); err == nil && a.walled(caller, w.Project) {
				return fixErr("no work item %q. Call work_items for refs", v)
			}
		}
	}
	ids := append([]string{sArg(args, "id"), sArg(args, "task_id"), sArg(args, "after")}, strs(args["blocked_by"])...)
	for _, v := range ids {
		if v == "" {
			continue
		}
		if t, err := a.findTask(v); err == nil && a.walled(caller, t.Project) {
			return fixErr("no task %q. Call work_tasks for ids", v)
		}
	}
	for _, k := range []string{"path", "from", "to", "file"} {
		if p := sArg(args, k); p != "" && a.pathWalled(caller, a.appPath(p)) {
			return fixErr("no such file or folder: %s", p)
		}
	}
	return nil
}

// busyNote is what an outside agent may know about private projects:
// that they exist, how much in them needs the user, and on which days
// something is due. No names, no titles.
func (a *App) busyNote(caller string) string {
	snap := a.Desk.Snapshot()
	n := 0
	for pid := range snap.Private {
		if a.walled(caller, pid) {
			if _, ok := a.Projects.Get(pid); ok {
				n++
			}
		}
	}
	if n == 0 {
		return ""
	}
	needs, open := 0, 0
	days := map[string]bool{}
	for _, t := range snap.WorkTasks {
		if !a.walled(caller, t.Project) || t.Status == "done" {
			continue
		}
		open++
		if needsMe(t) {
			needs++
		}
		if d, err := time.Parse("2006-01-02", t.Due); err == nil && time.Until(d) < 14*24*time.Hour {
			days[d.Format("Mon Jan 2")] = true
		}
	}
	for _, nd := range a.needs() {
		if a.walled(caller, nd.Project) && nd.Source != "work" {
			needs++
		}
	}
	var ds []string
	for d := range days {
		ds = append(ds, d)
	}
	sort.Strings(ds)
	s := fmt.Sprintf("private projects: %d (the user's own: you can't see their names, work or files, and nobody can tell you; if the user asks about one, say it's private and they can open it themselves) · %d open task(s), %d thing(s) need the user", n, open, needs)
	if len(ds) > 0 {
		s += " · something due " + strings.Join(ds, ", ")
	}
	return s + "\n"
}

// scrub is the last line of defence: any line that still names a private
// item, task, path or project is replaced for an outside caller.
func (a *App) scrub(caller, text string) string {
	if text == "" || !a.anyPrivate() {
		return text
	}
	var marks []string
	for pid := range a.Desk.Snapshot().Private {
		if !a.walled(caller, pid) {
			continue
		}
		if p, ok := a.Projects.Get(pid); ok {
			marks = append(marks, p.Path, "\""+p.ID+"\"", p.ID+"/", "p:"+p.ID, "project "+p.ID)
			if len(p.Name) >= 4 {
				marks = append(marks, p.Name)
			}
		}
		for _, w := range a.workItems() {
			if w.Project == pid {
				marks = append(marks, w.Ref, w.Title)
			}
		}
		snap := a.Desk.Snapshot()
		for _, t := range snap.WorkTasks {
			if t.Project == pid {
				marks = append(marks, t.ID, t.Title)
			}
		}
		for _, it := range snap.Items {
			if it.Project == pid {
				marks = append(marks, it.Title)
			}
		}
	}
	if len(marks) == 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	changed := false
	for i, l := range lines {
		for _, m := range marks {
			if len(m) >= 3 && strings.Contains(l, m) {
				lines[i], changed = "(hidden: about a private project)", true
				break
			}
		}
	}
	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
}

// ---- from the shell ----

func (a *App) setPrivate(pid string, on bool) error {
	if _, ok := a.Projects.Get(pid); !ok {
		return fixErr("no project %q", pid)
	}
	a.Desk.SetPrivate(pid, on)
	a.loadPrivate()
	what := "made private"
	if !on {
		what = "made not private"
	}
	a.workChanged("you", what, pid, "")
	return nil
}
