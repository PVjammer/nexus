package app

// Work items and tasks (the Tasks app).
//
// A work item lives in its source: a foreman workstream, a line of a
// foreman backlog, or an item on a project's Nexus list (later: Jira, GitHub).
// Nexus reads it and never writes to the source. What Nexus keeps is its own:
// whether the work item is in progress or parked, its done-when list, and the
// tasks toward it. Tasks are concrete next actions on the usual board (to do,
// in progress, blocked, in review, done), assigned to you, an agent, or
// someone else. A task with no work item is a todo.
//
// You (the Tasks app) and agents (work_* tools) go through the same functions.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
)

var taskStatuses = []string{"todo", "doing", "blocked", "review", "done"}
var itemStates = []string{"in-progress", "parked", "not-started"}

var statusName = map[string]string{"todo": "To do", "doing": "In progress", "blocked": "Blocked", "review": "In review", "done": "Done"}

// WorkItem is a work item as Nexus sees it: read from its source, plus what
// Nexus tracks about it and its tasks.
type WorkItem struct {
	Ref         string   `json:"ref"`
	Project     string   `json:"project"`
	Title       string   `json:"title"`
	Source      string   `json:"source"`                // project list | foreman workstream | foreman backlog
	SourceState string   `json:"sourceState,omitempty"` // what the source says (e.g. working · needs-input)
	Detail      string   `json:"detail,omitempty"`
	Report      string   `json:"report,omitempty"` // app path
	State       string   `json:"state"`            // in-progress | parked | not-started
	ParkNote    string   `json:"parkNote,omitempty"`
	DoneWhen    []string `json:"doneWhen,omitempty"`
	Brief       string   `json:"brief,omitempty"`
	// Unshaped: in progress but nobody has said what done looks like, or
	// there's no task yet. The Shape button asks the agent to fix that.
	Unshaped bool      `json:"unshaped,omitempty"`
	Open     int       `json:"open"`
	Done     int       `json:"done"`
	Move     string    `json:"move"` // you | agent | waiting | blocked | ready | none
	MoveNote string    `json:"moveNote,omitempty"`
	Updated  time.Time `json:"updated"`
}

func shortHash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])[:8]
}

// workItems lists every work item Nexus can see, in-progress first.
func (a *App) workItems() []WorkItem {
	snap := a.Desk.Snapshot()
	var out []WorkItem
	seen := map[string]bool{}
	add := func(w WorkItem, defState string) {
		if t := snap.Tracked[w.Ref]; t != nil && t.State != "" {
			w.State, w.ParkNote, w.DoneWhen, w.Brief = t.State, t.ParkNote, t.DoneWhen, t.Brief
		} else {
			w.State = defState
			if t != nil {
				w.DoneWhen, w.Brief = t.DoneWhen, t.Brief
			}
		}
		seen[w.Ref] = true
		out = append(out, w)
	}
	for _, d := range a.allDetails() {
		for _, ws := range d.Workstreams {
			if ws.Closed {
				continue
			}
			w := WorkItem{Ref: "ws:" + d.ID + "/" + ws.Name, Project: d.ID, Title: ws.Name, Source: "foreman workstream",
				SourceState: strings.Trim(ws.AgentStatus+" · "+ws.ReportStatus, " ·"), Detail: oneLine(firstNonEmpty(ws.Summary, ws.Goal), 200), Updated: ws.Updated}
			if ws.Report != "" {
				w.Report = d.ID + "/" + ws.Report
			}
			add(w, "in-progress")
		}
		for _, bl := range d.Backlog {
			if bl.Done {
				continue
			}
			add(WorkItem{Ref: "bl:" + d.ID + "/" + shortHash(bl.Text), Project: d.ID, Title: oneLine(bl.Text, 160), Source: "foreman backlog", Detail: oneLine(bl.Text, 400)}, "not-started")
		}
	}
	for _, it := range snap.Items {
		if it.State != "open" || it.Kind != "task" {
			continue
		}
		add(WorkItem{Ref: "item:" + it.ID, Project: it.Project, Title: it.Title, Source: "project list", Detail: oneLine(it.Body, 200), Updated: it.Created}, "not-started")
	}
	// Tasks pointing at a work item the source no longer lists (a workstream
	// closed, a backlog line ticked) keep a home here.
	for _, t := range snap.WorkTasks {
		if t.Item != "" && !seen[t.Item] && t.Status != "done" {
			add(WorkItem{Ref: t.Item, Project: t.Project, Title: refTitle(t.Item), Source: "closed in its source"}, "in-progress")
		}
	}
	for i := range out {
		a.fillMove(&out[i], snap.WorkTasks)
		out[i].Unshaped = out[i].State == "in-progress" && (len(out[i].DoneWhen) == 0 || out[i].Open+out[i].Done == 0)
	}
	rank := map[string]int{"in-progress": 0, "parked": 1, "not-started": 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].State] < rank[out[j].State] })
	return out
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if strings.TrimSpace(x) != "" {
			return x
		}
	}
	return ""
}

func refTitle(ref string) string {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

// ---- whose move ----

func needsMe(t *desk.WorkTask) bool {
	switch t.Status {
	case "todo", "doing":
		return t.Assignee == "you"
	case "review":
		return t.Reviewer == "you" || (t.Reviewer == "" && t.Assignee != "you")
	}
	return false
}

func agentOn(t *desk.WorkTask) bool { return t.Assignee == "agent" && t.Status == "doing" }

func waitingOn(t *desk.WorkTask) string {
	switch t.Status {
	case "todo", "doing":
		if t.Assignee != "you" && t.Assignee != "agent" {
			return t.Assignee
		}
	case "review":
		if t.Reviewer != "" && t.Reviewer != "you" {
			return t.Reviewer
		}
	}
	return ""
}

func (a *App) fillMove(w *WorkItem, all []*desk.WorkTask) {
	var mine, ag, wait, blocked, open int
	who := ""
	for _, t := range all {
		if t.Item != w.Ref {
			continue
		}
		if t.Status == "done" {
			w.Done++
			continue
		}
		open++
		switch {
		case needsMe(t):
			mine++
		case agentOn(t):
			ag++
		case waitingOn(t) != "":
			wait++
			if who == "" {
				who = waitingOn(t)
			}
		case t.Status == "blocked":
			blocked++
		}
	}
	w.Open = open
	switch {
	case mine > 0:
		w.Move, w.MoveNote = "you", fmt.Sprintf("%d for you", mine)
	case ag > 0:
		w.Move, w.MoveNote = "agent", "agent working"
	case wait > 0:
		w.Move, w.MoveNote = "waiting", "waiting on "+who
	case blocked > 0:
		w.Move, w.MoveNote = "blocked", "blocked"
	case open > 0:
		w.Move, w.MoveNote = "ready", "ready"
	default:
		w.Move = "none"
		if w.State == "in-progress" {
			w.MoveNote = "no next action yet"
		}
	}
}

// live says whether a task shows in your lists: todos always, and tasks of
// work items that are in progress (parked work stays out of the way).
func (a *App) liveTaskFn() func(t *desk.WorkTask) bool {
	state := map[string]string{}
	for _, w := range a.workItems() {
		state[w.Ref] = w.State
	}
	return func(t *desk.WorkTask) bool {
		if t.Item == "" {
			return true
		}
		s, ok := state[t.Item]
		return !ok || s == "in-progress"
	}
}

// workNeeds are tasks waiting on you, for the needs-you list.
func (a *App) workNeeds() []Need {
	live := a.liveTaskFn()
	titles := map[string]string{}
	for _, w := range a.workItems() {
		titles[w.Ref] = w.Title
	}
	var out []Need
	for _, t := range a.Desk.Snapshot().WorkTasks {
		if !needsMe(t) || !live(t) {
			continue
		}
		kind := "task"
		if t.Status == "review" {
			kind = "review"
		}
		detail := titles[t.Item]
		if t.Item == "" {
			detail = "todo"
		}
		out = append(out, Need{ID: "work:" + t.ID, Source: "work", Project: t.Project, Title: t.Title, Detail: detail, Kind: kind, Time: t.Updated, Task: t.ID})
	}
	return out
}

// ---- changes (you and agents) ----

type TaskEdit struct {
	Title, Item, Project, Status, Assignee, Reviewer, Due, Note *string
	BlockedBy                                                   *[]string
	Why                                                         string
}

// checkItemRef finds a work item by ref, or by its exact title when that's
// unique (small models remember titles better than refs).
func (a *App) checkItemRef(ref string) (WorkItem, error) {
	ref = strings.TrimSpace(ref)
	var refs []string
	var byTitle []WorkItem
	for _, w := range a.workItems() {
		if w.Ref == ref {
			return w, nil
		}
		if normTitle(w.Title) == normTitle(ref) {
			byTitle = append(byTitle, w)
		}
		if len(refs) < 12 {
			refs = append(refs, w.Ref)
		}
	}
	if len(byTitle) == 1 {
		return byTitle[0], nil
	}
	return WorkItem{}, fixErr("no work item %q. Call work_items for refs; some now: %s", ref, strings.Join(refs, ", "))
}

// findTask finds a task by id, or by its exact title when that's unique
// (small models often remember the title, not the id).
func (a *App) findTask(id string) (desk.WorkTask, error) {
	id = strings.TrimSpace(id)
	if t, ok := a.Desk.WorkTask(id); ok {
		return t, nil
	}
	var hit []*desk.WorkTask
	for _, t := range a.Desk.Snapshot().WorkTasks {
		if strings.EqualFold(t.Title, id) {
			hit = append(hit, t)
		}
	}
	if len(hit) == 1 {
		return *hit[0], nil
	}
	return desk.WorkTask{}, fixErr("no task %q. work_tasks lists the ids (like k12)", id)
}

func (a *App) applyTaskEdit(t *desk.WorkTask, e TaskEdit) error {
	if e.Title != nil {
		if strings.TrimSpace(*e.Title) == "" {
			return fixErr("title can't be empty")
		}
		t.Title = strings.TrimSpace(*e.Title)
	}
	if e.Status != nil && *e.Status != "" {
		if !contains(taskStatuses, *e.Status) {
			return fixErr("status must be one of: %s", strings.Join(taskStatuses, ", "))
		}
		t.Status = *e.Status
	}
	if e.Assignee != nil && *e.Assignee != "" {
		t.Assignee = strings.TrimSpace(*e.Assignee)
	}
	if e.Reviewer != nil {
		t.Reviewer = strings.TrimSpace(*e.Reviewer)
	}
	if e.Due != nil {
		d := strings.TrimSpace(*e.Due)
		if d != "" {
			if _, err := time.Parse("2006-01-02", d); err != nil {
				return fixErr("due must be a date like 2026-11-20")
			}
		}
		t.Due = d
	}
	if e.Note != nil {
		t.Note = *e.Note
	}
	if e.BlockedBy != nil {
		var ids []string
		for _, b := range *e.BlockedBy {
			o, err := a.findTask(b)
			if err != nil {
				return err
			}
			if o.ID == t.ID {
				return fixErr("a task can't wait on itself")
			}
			ids = append(ids, o.ID)
		}
		t.BlockedBy = ids
	}
	return nil
}

func (a *App) workTaskAdd(by, project string, e TaskEdit) (desk.WorkTask, error) {
	t := desk.WorkTask{Status: "todo", Assignee: "you", By: by, Why: e.Why, Project: project}
	if e.Item != nil && *e.Item != "" {
		w, err := a.checkItemRef(*e.Item)
		if err != nil {
			return t, err
		}
		if project != "" && w.Project != project {
			return t, deny("work item %s is in project %s, not yours (%s)", w.Ref, w.Project, project)
		}
		t.Item, t.Project = w.Ref, w.Project
	} else if e.Project != nil && *e.Project != "" {
		if _, ok := a.Projects.Get(*e.Project); !ok {
			return t, fixErr("no project %q", *e.Project)
		}
		if project != "" && *e.Project != project {
			return t, deny("you can only add tasks in your own project (%s)", project)
		}
		t.Project = *e.Project
	}
	if e.Title == nil || strings.TrimSpace(*e.Title) == "" {
		return t, fixErr("title is required: the next concrete action, e.g. \"Answer the platform lead's question\"")
	}
	if err := a.applyTaskEdit(&t, e); err != nil {
		return t, err
	}
	if len(t.BlockedBy) > 0 && (e.Status == nil || *e.Status == "") {
		t.Status = "blocked"
	}
	nt := a.Desk.AddWorkTask(t)
	a.workChanged(by, "added task", nt.Title, e.Why)
	return nt, nil
}

func (a *App) workTaskUpdate(by, project, id string, e TaskEdit) (desk.WorkTask, error) {
	cur, err := a.findTask(id)
	if err != nil {
		return cur, err
	}
	if project != "" && cur.Project != project {
		return cur, deny("task %s is in project %s, not yours (%s)", cur.ID, cur.Project, project)
	}
	nt, err := a.Desk.UpdateWorkTask(cur.ID, func(t *desk.WorkTask) error { return a.applyTaskEdit(t, e) })
	if err == nil {
		what := "changed task"
		if e.Status != nil && *e.Status != cur.Status {
			what = "moved task to " + statusName[*e.Status]
		}
		a.workChanged(by, what, nt.Title, e.Why)
	}
	return nt, err
}

func (a *App) workItemSet(by, project, ref, state, note string, doneWhen *[]string, brief *string, why string) (desk.Tracked, error) {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return desk.Tracked{}, err
	}
	if project != "" && w.Project != project {
		return desk.Tracked{}, deny("work item %s is in project %s, not yours (%s)", ref, w.Project, project)
	}
	if state != "" && !contains(itemStates, state) {
		return desk.Tracked{}, fixErr("state must be one of: %s", strings.Join(itemStates, ", "))
	}
	ref = w.Ref
	t := a.Desk.Track(ref, func(t *desk.Tracked) {
		if state != "" {
			t.State = state
			if state != "parked" {
				t.ParkNote = ""
			}
		}
		if note != "" {
			t.ParkNote = note
		}
		if doneWhen != nil {
			t.DoneWhen = *doneWhen
		}
		if brief != nil {
			t.Brief = strings.TrimSpace(*brief)
		}
	})
	what := "updated"
	switch state {
	case "in-progress":
		what = "started"
	case "parked":
		what = "parked"
	case "not-started":
		what = "took out of progress"
	}
	a.workChanged(by, what+" work item", w.Title, why)
	return t, nil
}

// inProgress counts work items in progress.
func (a *App) inProgress() []WorkItem {
	var out []WorkItem
	for _, w := range a.workItems() {
		if w.State == "in-progress" {
			out = append(out, w)
		}
	}
	return out
}

// overLimitNote is what the agent is told after starting a work item that
// takes the user over their in-progress limit: suggest parking one, don't do it.
func (a *App) overLimitNote() string {
	lim := a.Desk.WipLimit()
	ip := a.inProgress()
	if lim <= 0 || len(ip) <= lim {
		return ""
	}
	var cands []string
	for _, w := range ip {
		if w.Move == "waiting" || w.Move == "blocked" || w.Move == "none" {
			cands = append(cands, fmt.Sprintf("%s (%s)", w.Ref, firstNonEmpty(w.MoveNote, w.Move)))
		}
	}
	s := fmt.Sprintf("\nNote: the user now has %d work items in progress; their limit is %d. Suggest parking one (ask; don't park it yourself).", len(ip), lim)
	if len(cands) > 0 {
		s += " Good candidates, nothing to do on their side: " + strings.Join(cands, ", ")
	}
	return s
}

func (a *App) workChanged(by, what, target, why string) {
	actor := you
	if by != "you" {
		actor = by
	}
	a.Log.Add(actionlog.Entry{Actor: actor, Action: what, Target: truncate(target, 80), Detail: truncate(why, 200)})
	a.broadcastDesk()
}

// ---- describing (tools and context) ----

func (a *App) describeTasks(filter func(*desk.WorkTask) bool) string {
	var b strings.Builder
	n := 0
	for _, t := range a.Desk.Snapshot().WorkTasks {
		if !filter(t) {
			continue
		}
		n++
		item := t.Item
		if item == "" {
			item = "todo"
		}
		extra := ""
		if t.Reviewer != "" {
			extra += " reviewer=" + t.Reviewer
		}
		if len(t.BlockedBy) > 0 {
			extra += " blocked_by=" + strings.Join(t.BlockedBy, ",")
		}
		if t.Due != "" {
			extra += " due=" + t.Due
		}
		fmt.Fprintf(&b, "- %s [%s] %s · assignee=%s%s · %s\n", t.ID, t.Status, t.Title, t.Assignee, extra, item)
	}
	if n == 0 {
		return "no tasks\n"
	}
	return b.String()
}

func (a *App) describeItems(project string, all bool) string {
	var b strings.Builder
	for _, w := range a.workItems() {
		if project != "" && w.Project != project {
			continue
		}
		if !all && w.State != "in-progress" {
			continue
		}
		state := w.State
		if w.ParkNote != "" {
			state += " (" + w.ParkNote + ")"
		}
		rep := ""
		if w.Report != "" {
			rep = " report=" + w.Report
		}
		if w.Unshaped {
			rep += " · needs shaping"
		}
		fmt.Fprintf(&b, "- %s · %q · %s · %s · move: %s · tasks %d open, %d done%s\n", w.Ref, w.Title, w.Source, state, firstNonEmpty(w.MoveNote, w.Move), w.Open, w.Done, rep)
	}
	if b.Len() == 0 {
		if all {
			return "no work items\n"
		}
		return "no work items in progress (work_items with all=true lists the rest)\n"
	}
	return b.String()
}

// ---- tools ----

func (a *App) workTools() []Tool {
	reason := sprop("why, in a few words (shown to the user)")
	return []Tool{
		{Name: "needs_you", Label: "Needs you", Kinds: HP,
			Description: "Everything waiting on the user, exactly as their Needs you list shows it: foreman questions and reports, finished or stuck task agents, wiki proposals, filings to confirm, and tasks of theirs. Use it to answer \"what needs me?\".",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (default: yours; home: every project)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				p := sArg(m, "project")
				if c.inProject() {
					p = c.Project.ID
				}
				var b strings.Builder
				for _, n := range a.needs() {
					if p != "" && n.Project != p {
						continue
					}
					what := map[string]string{"foreman": "foreman " + n.Kind, "task": "task agent " + n.Kind, "wiki": "wiki proposal", "filing": "filings to confirm", "work": "task (" + n.Kind + ")"}[n.Source]
					extra := ""
					if n.Report != "" {
						extra = " report=" + n.Report
					}
					if n.Source == "work" {
						extra = " task_id=" + n.Task
					}
					fmt.Fprintf(&b, "- [%s] %s · project %s · %s%s\n", what, oneLine(n.Title+": "+n.Detail, 200), firstNonEmpty(n.Project, "none"), ago(n.Time), extra)
				}
				if b.Len() == 0 {
					return "nothing is waiting on the user\n", nil
				}
				return b.String(), nil
			}},
		{Name: "work_limit", Label: "In-progress limit", Kinds: H,
			Description: "Set how many work items the user can have in progress before starting another one suggests parking one (0 = never suggest). Only when the user asks.",
			Parameters:  obj([]string{"limit", "reason"}, map[string]any{"limit": iprop("0-20; 0 = never"), "reason": reason}),
			run: func(a *App, _ *Ctx, m map[string]any) (string, error) {
				if err := needReason(m); err != nil {
					return "", err
				}
				n := iArg(m, "limit", -1)
				if n < 0 || n > 20 {
					return "", fixErr("limit must be 0 to 20 (0 = never suggest parking)")
				}
				a.Desk.SetWipLimit(n)
				a.workChanged("home agent", "set the in-progress limit", fmt.Sprint(n), sArg(m, "reason"))
				return fmt.Sprintf("limit set to %d (%d in progress now)", n, len(a.inProgress())), nil
			}},
		{Name: "work_items", Label: "Work items", Kinds: HP,
			Description: "List work items (foreman workstreams, backlog lines, Nexus list items) with their ref, state (in-progress, parked, not-started), whose move it is and task counts. In-progress only unless all=true.",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (default: yours; home: every project)"), "all": bprop("include parked and not-started work items")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				p := sArg(m, "project")
				if c.inProject() {
					p = c.Project.ID
				}
				return a.describeItems(p, bArg(m, "all")), nil
			}},
		{Name: "work_tasks", Label: "Tasks", Kinds: HPT,
			Description: "List tasks: for one work item (item=ref), the ones waiting on the user (needs_me=true), or every open task in a project.",
			Parameters: obj(nil, map[string]any{"item": sprop("work item ref"), "project": sprop("project id"), "needs_me": bprop("only tasks waiting on the user"),
				"include_done": bprop("include finished tasks")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				item, p := sArg(m, "item"), sArg(m, "project")
				if c.inProject() {
					p = c.Project.ID
				}
				nm, done := bArg(m, "needs_me"), bArg(m, "include_done")
				live := a.liveTaskFn()
				return a.describeTasks(func(t *desk.WorkTask) bool {
					return (item == "" || t.Item == item) && (p == "" || t.Project == p) && (done || t.Status != "done") && (!nm || (needsMe(t) && live(t)))
				}), nil
			}},
		{Name: "work_task_add", Label: "Add task", Kinds: HP,
			Description: "Add a task: one concrete next action toward a work item (or a plain todo with no item). Assign it to you (the user, default), agent, or a person's name for something you're waiting on.",
			Parameters: obj([]string{"title", "reason"}, map[string]any{
				"title": sprop("the next concrete action, e.g. 'Review MR !41' or 'Grant API access (IT-882)'"), "item": sprop("work item ref from work_items; leave out for a todo"),
				"project": sprop("for a todo: the project it belongs to (optional)"), "assignee": sprop("you (default), agent, or a person's name"),
				"status": eprop("default todo; blocked when blocked_by is given", taskStatuses...), "blocked_by": aprop("ids of tasks this waits on", sprop("")),
				"reviewer": sprop("who reviews it when it's in review: you, or a name"), "due": sprop("YYYY-MM-DD"), "reason": reason,
			}), run: toolWorkTaskAdd},
		{Name: "work_task_update", Label: "Update task", Kinds: HP,
			Description: "Change a task: move it on the board (status), reassign it, set a reviewer, what it waits on, a due date or a note. Give only what changes.",
			Parameters: obj([]string{"id", "reason"}, map[string]any{
				"id": sprop("task id from work_tasks (or its exact title)"), "status": eprop("", taskStatuses...), "assignee": sprop("you, agent, or a name"),
				"reviewer": sprop("you, a name, or '' for none"), "title": sprop("new title"), "blocked_by": aprop("ids of tasks it waits on ([] for none)", sprop("")),
				"due": sprop("YYYY-MM-DD, or '' for none"), "note": sprop("a short note"), "reason": reason,
			}), run: toolWorkTaskUpdate},
		{Name: "work_item_set", Label: "Work item state", Kinds: HP,
			Description: "Start, park or stop tracking a work item in Nexus (the source is never changed), and set its done-when list and brief.",
			Parameters: obj([]string{"item", "reason"}, map[string]any{
				"item": sprop("work item ref"), "state": eprop("in-progress, parked (out of the way until something happens) or not-started", itemStates...),
				"park_note": sprop("when parking: what it waits on, e.g. 'until IT grants access'"), "done_when": aprop("what done looks like, 2-5 short checks", sprop("")),
				"brief": sprop("what the work is, in 1-3 lines from what the user told you: scope, constraints, who's involved"), "reason": reason,
			}), run: toolWorkItemSet},
	}
}

func optStrs(m map[string]any, k string) *[]string {
	if _, ok := m[k]; !ok || m[k] == nil {
		return nil
	}
	v := strs(m[k])
	return &v
}

func taskEditFromArgs(m map[string]any) TaskEdit {
	return TaskEdit{Title: optStr(m, "title"), Item: optStr(m, "item"), Project: optStr(m, "project"), Status: optStr(m, "status"),
		Assignee: optStr(m, "assignee"), Reviewer: optStr(m, "reviewer"), Due: optStr(m, "due"), Note: optStr(m, "note"),
		BlockedBy: optStrs(m, "blocked_by"), Why: sArg(m, "reason")}
}

func agentName(c *Ctx) string {
	if c.Kind == KindHome {
		return "home agent"
	}
	return c.Run.label()
}

func toolWorkTaskAdd(a *App, c *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	t, err := a.workTaskAdd(agentName(c), c.Project.ID, taskEditFromArgs(m))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added %s [%s] %q for %s", t.ID, t.Status, t.Title, t.Assignee), nil
}

func toolWorkTaskUpdate(a *App, c *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	t, err := a.workTaskUpdate(agentName(c), c.Project.ID, sArg(m, "id"), taskEditFromArgs(m))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s is now [%s] %q, assignee %s", t.ID, t.Status, t.Title, t.Assignee), nil
}

func toolWorkItemSet(a *App, c *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	t, err := a.workItemSet(agentName(c), c.Project.ID, sArg(m, "item"), sArg(m, "state"), sArg(m, "park_note"), optStrs(m, "done_when"), optStr(m, "brief"), sArg(m, "reason"))
	if err != nil {
		return "", err
	}
	note := ""
	if sArg(m, "state") == "in-progress" {
		note = a.overLimitNote()
	}
	return fmt.Sprintf("%s: %s%s, done when: %s%s", t.Ref, firstNonEmpty(t.State, "unchanged"), map[bool]string{true: " (" + t.ParkNote + ")", false: ""}[t.ParkNote != ""], strings.Join(t.DoneWhen, "; "), note), nil
}

// ---- from the shell ----

func (a *App) handleWork(t string, m map[string]any) (any, error) {
	e := TaskEdit{}
	for k, dst := range map[string]**string{"title": &e.Title, "item": &e.Item, "project": &e.Project, "status": &e.Status, "assignee": &e.Assignee, "reviewer": &e.Reviewer, "due": &e.Due, "note": &e.Note} {
		if v, ok := m[k].(string); ok {
			vv := v
			*dst = &vv
		}
	}
	if _, ok := m["blockedBy"]; ok {
		e.BlockedBy = optStrs(m, "blockedBy")
	}
	switch t {
	case "workTaskAdd":
		return a.workTaskAdd("you", "", e)
	case "workTaskUpdate":
		return a.workTaskUpdate("you", "", str(m["id"]), e)
	case "workTaskRemove":
		tk, err := a.findTask(str(m["id"]))
		if err != nil {
			return nil, err
		}
		a.Desk.RemoveWorkTask(tk.ID)
		a.workChanged("you", "removed task", tk.Title, "")
		return nil, nil
	case "workLimit":
		n := iArg(m, "limit", -1)
		if n < 0 || n > 20 {
			return nil, fixErr("limit must be 0 to 20 (0 = never)")
		}
		a.Desk.SetWipLimit(n)
		a.workChanged("you", "set the in-progress limit", fmt.Sprint(n), "")
		return nil, nil
	case "workItemSet":
		return a.workItemSet("you", "", str(m["item"]), str(m["state"]), str(m["parkNote"]), optStrs(m, "doneWhen"), optStr(m, "brief"), "")
	}
	return nil, fmt.Errorf("unknown %s", t)
}
