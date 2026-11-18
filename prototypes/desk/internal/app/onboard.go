package app

// Onboarding a project, and shaping its work.
//
// A new project starts with no work items. Setting it up is a conversation
// with the project agent, in four steps you can skip or redo any time:
//
//  1. scan: plain code (no model) finds signs of work in the folder: TODO
//     comments, unchecked checklists, plan sections, design docs, unmerged
//     branches, uncommitted changes. Each finding says where it came from.
//  2. propose: the agent groups the findings into a few work items and
//     proposes them. You tick the ones you want (a checklist, not a chat).
//  3. ask: "what else do you want to do with this project?", then which of
//     these you're working on now (the in-progress limit applies).
//  4. shape: for each item in progress, a few questions (with suggested
//     answers to click), then 2-5 tasks to accept.
//
// Nothing here knows about any particular tool's files. Whatever a project
// keeps (a backlog, design notes, a report) is found by looking, the same way
// for every project.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/scan"
)

// ---- scan ----

func (a *App) scanProject(pid string) (scan.Result, error) {
	p, ok := a.Projects.Get(pid)
	if !ok {
		return scan.Result{}, fixErr("no project %q", pid)
	}
	r := scan.Project(p.Path)
	r.Root = pid + "/"
	a.Desk.TouchOnboarding(pid, func(o *desk.Onboarding) { o.Scanned = time.Now() })
	return r, nil
}

// describeScan is the scan as compact text for a model: numbered findings
// grouped by kind, each with where it came from.
func describeScan(r scan.Result, known []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Scanned %s (%d files).\n", r.Root, r.Files)
	if r.Readme != "" {
		fmt.Fprintf(&b, "\nREADME (start). Check where it says things stand before proposing; don't propose what it or a newer doc says is done:\n%s\n", r.Readme)
	}
	group := func(title string, fs []scan.Finding, withSection bool) {
		if len(fs) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s (%d):\n", title, len(fs))
		for _, f := range fs {
			sec := ""
			if withSection && f.Section != "" {
				sec = " [" + f.Section + "]"
			}
			fmt.Fprintf(&b, "- %s · %s%s\n", f.Where, f.Text, sec)
		}
	}
	group("Unmerged branches (work in flight)", r.Branches, false)
	if r.Changes != nil {
		fmt.Fprintf(&b, "\nUncommitted changes: %s · %s\n", r.Changes.Text, r.Changes.Where)
	}
	group("Unchecked checklist items", r.Checks, true)
	group("Plan sections (todo, roadmap, next steps, open questions...)", r.Plans, true)
	group("TODO comments", r.Todos, false)
	if len(r.Docs) > 0 {
		fmt.Fprintf(&b, "\nDesign docs and notes, newest first (read the ones that look like plans):\n")
		for _, f := range r.Docs {
			fmt.Fprintf(&b, "- %s · %s · %s\n", f.Where, f.Text, ago(f.When))
		}
	}
	if len(r.Branches)+len(r.Checks)+len(r.Plans)+len(r.Todos)+len(r.Docs) == 0 && r.Changes == nil {
		b.WriteString("\nNothing that looks like planned work. Ask the user what they want to do with this project.\n")
	}
	if r.Capped {
		b.WriteString("\n(Some lists were cut short; the folder is big.)\n")
	}
	if len(known) > 0 {
		fmt.Fprintf(&b, "\nAlready work items or proposed before (don't propose these again): %s\n", strings.Join(known, "; "))
	}
	return b.String()
}

// knownTitles is what a project already has or was offered: open work items,
// open and dismissed proposals. Rescans don't propose them again.
func (a *App) knownTitles(pid string) []string {
	var out []string
	for _, w := range a.workItems() {
		if w.Project == pid {
			out = append(out, w.Title)
		}
	}
	for _, p := range a.Desk.Snapshot().Proposals {
		if p.Project == pid && p.Item == "" && p.Status != "accepted" {
			out = append(out, p.Title)
		}
	}
	return out
}

func normTitle(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// ---- proposals ----

// ProposalIn is one proposal as an agent sends it.
type ProposalIn struct {
	Title, Why, Assignee, Brief string
	Evidence, DoneWhen, After   []string
}

// propose stores a batch of proposals: work items for a project (item == "")
// or tasks toward one work item. Duplicates of what's already there, or was
// dismissed before, are skipped and named.
func (a *App) propose(agentID, pid, item string, in []ProposalIn) ([]desk.WorkProposal, []string, error) {
	return a.proposeIn(nil, agentID, pid, item, in)
}

// proposeIn is propose for an agent run: calls in the same turn for the same
// project and item go into one batch (one checklist), as long as the user
// hasn't started ticking it.
func (a *App) proposeIn(r *Run, agentID, pid, item string, in []ProposalIn) ([]desk.WorkProposal, []string, error) {
	if item != "" {
		w, err := a.checkItemRef(item)
		if err != nil {
			return nil, nil, err
		}
		if pid != "" && w.Project != pid {
			return nil, nil, deny("work item %s is in project %s, not yours (%s)", item, w.Project, pid)
		}
		pid, item = w.Project, w.Ref
	}
	if pid == "" {
		return nil, nil, fixErr("project is required: the project id these are for")
	}
	if _, ok := a.Projects.Get(pid); !ok {
		return nil, nil, fixErr("no project %q", pid)
	}
	if len(in) == 0 {
		return nil, nil, fixErr("proposals is empty: give 1 to 8, each with a title")
	}
	if len(in) > 8 {
		return nil, nil, fixErr("too many proposals (%d): give at most 8, the ones that matter most", len(in))
	}
	seen := map[string]bool{}
	if item == "" {
		for _, t := range a.knownTitles(pid) {
			seen[normTitle(t)] = true
		}
	} else {
		for _, t := range a.Desk.Snapshot().WorkTasks {
			if t.Item == item {
				seen[normTitle(t.Title)] = true
			}
		}
		for _, p := range a.Desk.Snapshot().Proposals {
			if p.Item == item && p.Status != "accepted" {
				seen[normTitle(p.Title)] = true
			}
		}
	}
	var keep []desk.WorkProposal
	var skipped []string
	for _, p := range in {
		title := strings.TrimSpace(p.Title)
		if title == "" {
			return nil, nil, fixErr("every proposal needs a title")
		}
		if seen[normTitle(title)] {
			skipped = append(skipped, title)
			continue
		}
		seen[normTitle(title)] = true
		as := p.Assignee
		if item != "" && as == "" {
			as = "you"
		}
		wp := desk.WorkProposal{Project: pid, Item: item, Title: oneLine(title, 160), Why: oneLine(p.Why, 400),
			Evidence: p.Evidence, Assignee: as, Agent: agentID}
		if item == "" {
			wp.Brief, wp.DoneWhen = strings.TrimSpace(p.Brief), cleanList(p.DoneWhen)
		} else {
			wp.After = cleanList(p.After)
		}
		keep = append(keep, wp)
	}
	if len(keep) == 0 {
		return nil, skipped, nil
	}
	batch, key := "", pid+"|"+item
	if r != nil {
		r.mu.Lock()
		batch = r.batches[key]
		r.mu.Unlock()
		for _, p := range a.Desk.Snapshot().Proposals {
			if p.Batch == batch && p.Status != "open" {
				batch = "" // the user is already going through it
			}
		}
	}
	out := a.Desk.AddToBatch(batch, keep)
	if r != nil {
		r.mu.Lock()
		if r.batches == nil {
			r.batches = map[string]string{}
		}
		r.batches[key] = out[0].Batch
		r.mu.Unlock()
	}
	what := "work items"
	if item != "" {
		what = "tasks for " + item
	}
	a.workChanged(agentID, fmt.Sprintf("proposed %d %s", len(out), what), pid, "")
	return out, skipped, nil
}

// resolveProposals applies the user's checklist: accepted proposals become
// work items (or tasks); start=true also puts accepted work items in
// progress. The agent that proposed them is told what happened, so the
// conversation carries on.
func (a *App) resolveProposals(accept, dismiss []string, start bool) (string, error) {
	var took, dropped []string
	agentID := ""
	// In the order they were proposed, so "after" can point at a task
	// accepted a moment earlier in the same checklist.
	accept = a.inProposalOrder(accept)
	taskIDs := map[string]string{} // item + normalized title -> task id
	for _, t := range a.Desk.Snapshot().WorkTasks {
		taskIDs[t.Item+"\x00"+normTitle(t.Title)] = t.ID
	}
	for _, id := range accept {
		p, ok := a.Desk.Proposal(id)
		if !ok || p.Status != "open" {
			continue
		}
		if err := a.Desk.SetProposal(id, "accepted"); err != nil {
			return "", err
		}
		agentID = p.Agent
		if p.Item == "" {
			body := p.Why
			if len(p.Evidence) > 0 {
				body += "\n\nFound in: " + strings.Join(p.Evidence, ", ")
			}
			it := a.Desk.AddItem(desk.Item{Project: p.Project, Kind: "task", Title: p.Title, Body: strings.TrimSpace(body), State: "open", Source: "agent"})
			ref := "item:" + it.ID
			a.Desk.Track(ref, func(t *desk.Tracked) {
				t.Brief, t.DoneWhen = p.Brief, p.DoneWhen
				if start {
					t.State = "in-progress"
				}
			})
			took = append(took, fmt.Sprintf("%q (%s)", p.Title, ref))
		} else {
			t := desk.WorkTask{Item: p.Item, Project: p.Project, Title: p.Title, Status: "todo", Assignee: firstNonEmpty(p.Assignee, "you"), By: "agent", Why: p.Why}
			// "after" names tasks by title; the ones that exist (accepted
			// earlier in this checklist, or already there) become blockers.
			// A dismissed predecessor just drops out.
			for _, ti := range p.After {
				if bid := taskIDs[p.Item+"\x00"+normTitle(ti)]; bid != "" {
					t.BlockedBy = append(t.BlockedBy, bid)
				}
			}
			if len(t.BlockedBy) > 0 {
				t.Status = "blocked"
			}
			nt := a.Desk.AddWorkTask(t)
			taskIDs[p.Item+"\x00"+normTitle(p.Title)] = nt.ID
			took = append(took, fmt.Sprintf("%q (%s)", p.Title, nt.ID))
		}
	}
	for _, id := range dismiss {
		p, ok := a.Desk.Proposal(id)
		if !ok || p.Status != "open" {
			continue
		}
		a.Desk.SetProposal(id, "dismissed")
		agentID = p.Agent
		dropped = append(dropped, fmt.Sprintf("%q", p.Title))
	}
	if len(took)+len(dropped) == 0 {
		return "", fixErr("nothing to do: those proposals are already handled")
	}
	msg := "I went through your proposals."
	if len(took) > 0 {
		msg += " Accepted: " + strings.Join(took, ", ") + "."
		if start {
			msg += " I'm working on the accepted work items now."
		}
	}
	if len(dropped) > 0 {
		msg += " Dismissed (they won't be suggested again; not parked): " + strings.Join(dropped, ", ") + "."
	}
	a.workChanged("you", "reviewed proposals", fmt.Sprintf("%d accepted, %d dismissed", len(took), len(dropped)), "")
	if agentID != "" {
		a.tellAgent(agentID, msg)
	}
	return msg, nil
}

// tellAgent sends a message from Nexus on the user's behalf (shown in the
// transcript as Nexus's), waiting for the agent's turn to end first.
func (a *App) tellAgent(agentID, text string) {
	r, err := a.run(agentID)
	if err != nil || a.cfg.Agents == nil {
		return
	}
	go func() {
		for i := 0; i < 1200 && isBusy(r); i++ {
			time.Sleep(500 * time.Millisecond)
		}
		ctx := ""
		if r.Kind == KindProject {
			ctx = a.projectContext(r.Project, "")
		}
		r.sayAs("app", text, ctx)
	}()
}

// openProposals groups open proposals by batch, for the shell and Needs you.
func (a *App) openProposals() []desk.WorkProposal {
	var out []desk.WorkProposal
	for _, p := range a.Desk.Snapshot().Proposals {
		if p.Status == "open" {
			out = append(out, *p)
		}
	}
	return out
}

func (a *App) proposalNeeds() []Need {
	type grp struct {
		n     Need
		what  string
		items int
	}
	by := map[string]*grp{}
	var order []string
	for _, p := range a.openProposals() {
		g := by[p.Batch]
		if g == nil {
			what := "work items"
			if p.Item != "" {
				what = "tasks for " + refTitleOf(a, p.Item)
			}
			from := "the agent"
			if r := a.existingRun(p.Agent); r != nil {
				from = "the " + r.label()
			} else if strings.HasPrefix(p.Agent, "p:") {
				from = "the project agent"
			}
			g = &grp{what: what, n: Need{ID: "batch:" + p.Batch, Source: "proposals", Project: p.Project, Kind: "proposals", Time: p.Created,
				Detail: "From " + from + ". Tick the ones you want; the rest won't be suggested again."}}
			by[p.Batch] = g
			order = append(order, p.Batch)
		}
		g.items++
	}
	var out []Need
	for _, b := range order {
		g := by[b]
		g.n.Count = g.items
		g.n.Title = fmt.Sprintf("%d suggested %s", g.items, g.what)
		out = append(out, g.n)
	}
	return out
}

func refTitleOf(a *App, ref string) string {
	for _, w := range a.workItems() {
		if w.Ref == ref {
			return w.Title
		}
	}
	return refTitle(ref)
}

// ---- work items the user names ----

func (a *App) workItemAdd(by, pid, title, brief string, start bool) (WorkItem, error) {
	if _, ok := a.Projects.Get(pid); !ok {
		return WorkItem{}, fixErr("no project %q", pid)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return WorkItem{}, fixErr("title is required: what the user wants done, e.g. \"Add CSV export\"")
	}
	for _, w := range a.workItems() {
		if w.Project == pid && normTitle(w.Title) == normTitle(title) {
			return w, fixErr("%s already exists as %s; use that ref", title, w.Ref)
		}
	}
	it := a.Desk.AddItem(desk.Item{Project: pid, Kind: "task", Title: oneLine(title, 160), State: "open", Source: map[bool]string{true: "you", false: "agent"}[by == "you"]})
	ref := "item:" + it.ID
	a.Desk.Track(ref, func(t *desk.Tracked) {
		t.Brief = strings.TrimSpace(brief)
		if start {
			t.State = "in-progress"
		}
	})
	a.workChanged(by, "added work item", title, "")
	return itemByRefApp(a, ref), nil
}

func itemByRefApp(a *App, ref string) WorkItem {
	for _, w := range a.workItems() {
		if w.Ref == ref {
			return w
		}
	}
	return WorkItem{Ref: ref}
}

// ---- starting the conversations ----

const onboardPrompt = "Set up this project: look through it for work, and help me decide what to work on."

func (a *App) startOnboarding(pid string) error {
	if _, ok := a.Projects.Get(pid); !ok {
		return fixErr("no project %q", pid)
	}
	a.Desk.TouchOnboarding(pid, func(o *desk.Onboarding) {
		if o.Started.IsZero() {
			o.Started = time.Now()
		}
	})
	a.broadcastDesk()
	go a.Chat("p:"+pid, onboardPrompt, "", "")
	return nil
}

func shapePrompt(w WorkItem) string {
	return fmt.Sprintf("Shape %q (%s): ask me what you need to know, then propose the next tasks.", w.Title, w.Ref)
}

func (a *App) startShaping(ref string) error {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return err
	}
	if w.Project == "" {
		return fixErr("%s has no project", ref)
	}
	go a.Chat("p:"+w.Project, shapePrompt(w), "", "")
	return nil
}

// ---- tools ----

func (a *App) onboardTools() []Tool {
	reason := sprop("why, in a few words (shown to the user)")
	proposal := obj([]string{"title"}, map[string]any{
		"title":     sprop("short, starting with a verb: 'Add CSV export', 'Finish the auth refactor (branch auth-v2)'"),
		"why":       sprop("one line: why this looks like work, from what you found"),
		"evidence":  aprop("where you found it, copied from the scan: 'docs/plan.md:12', 'branch auth-v2'", sprop("")),
		"assignee":  sprop("tasks only: you (default), agent, or a person's name"),
		"brief":     sprop("work items only: what it is and what's in scope, 1-2 lines, from what you read"),
		"done_when": aprop("work items only: 2-4 short checks that say it's finished, e.g. 'CSV export works from the admin page'", sprop("")),
		"after":     aprop("tasks only: titles of tasks this one comes after (earlier in this list, or already on the work item). It waits until they're done", sprop("")),
	})
	return []Tool{
		{Name: "project_scan", Label: "Scan the project", Kinds: HP,
			Description: "Look through a project folder for signs of work: unmerged branches, uncommitted changes, unchecked checklists, plan sections (todo, roadmap, next steps, open questions), TODO comments and design docs. Read-only, no model; every finding says where it is. Start onboarding with it.",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (default: yours)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				pid := sArg(m, "project")
				if c.inProject() {
					pid = c.Project.ID
				}
				if pid == "" {
					return "", fixErr("project is required. project_list shows the ids")
				}
				r, err := a.scanProject(pid)
				if err != nil {
					return "", err
				}
				return describeScan(r, a.knownTitles(pid)), nil
			}},
		{Name: "work_propose", Label: "Propose work", Kinds: HP,
			Description: "Propose work items for a project (leave item out), or tasks toward one work item (item=ref). The user gets a checklist and accepts or dismisses each; you're told what they chose. Group related findings into one work item; 3-8 items, the ones that matter.",
			Parameters: obj([]string{"proposals", "reason"}, map[string]any{
				"project":   sprop("project id (default: yours)"),
				"item":      sprop("work item ref, to propose tasks toward it"),
				"proposals": aprop("what you propose", proposal),
				"reason":    reason,
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				if err := needReason(m); err != nil {
					return "", err
				}
				pid := sArg(m, "project")
				if c.inProject() {
					pid = c.Project.ID
				}
				var in []ProposalIn
				list, _ := m["proposals"].([]any)
				if js, ok := m["proposals"].(string); ok { // some models send the list as a JSON string
					if json.Unmarshal([]byte(js), &list) != nil {
						return "", fixErr("proposals must be a list of objects, not a string")
					}
				}
				for _, x := range list {
					pm, _ := x.(map[string]any)
					if pm == nil {
						if s, ok := x.(string); ok {
							in = append(in, ProposalIn{Title: s})
							continue
						}
						return "", fixErr("each proposal is an object like {\"title\": \"Add CSV export\", \"why\": \"...\"}")
					}
					in = append(in, ProposalIn{Title: sArg(pm, "title"), Why: sArg(pm, "why"), Assignee: sArg(pm, "assignee"), Evidence: strs(pm["evidence"]),
						Brief: sArg(pm, "brief"), DoneWhen: strs(pm["done_when"]), After: strs(pm["after"])})
				}
				out, skipped, err := a.proposeIn(c.Run, c.Run.ID, pid, sArg(m, "item"), in)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				if len(out) > 0 {
					fmt.Fprintf(&b, "Proposed %d (batch %s). They're suggestions in the user's checklist, not work items yet: you'll be told what they accept. Tell them in one line, and you can ask your next question meanwhile.", len(out), out[0].Batch)
				}
				if len(skipped) > 0 {
					fmt.Fprintf(&b, " Skipped (already there or dismissed before): %s.", strings.Join(skipped, "; "))
				}
				return strings.TrimSpace(b.String()), nil
			}},
		{Name: "work_item_add", Label: "Add work item", Kinds: HP,
			Description: "Add a work item the user asked for in their own words (\"I also want to add dark mode\"). A work item is a goal the project works toward, not a step or a wait: next steps and things the user is waiting on are tasks (work_task_add). Things you found yourself go through work_propose instead.",
			Parameters: obj([]string{"title", "reason"}, map[string]any{
				"project": sprop("project id (default: yours)"), "title": sprop("short, starting with a verb"),
				"brief": sprop("what the user said about it, in a line or two"), "start": bprop("true if the user is working on it now"), "reason": reason,
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				if err := needReason(m); err != nil {
					return "", err
				}
				pid := sArg(m, "project")
				if c.inProject() {
					pid = c.Project.ID
				}
				w, err := a.workItemAdd(agentName(c), pid, sArg(m, "title"), sArg(m, "brief"), bArg(m, "start"))
				if err != nil {
					return "", err
				}
				note := ""
				if bArg(m, "start") {
					note = a.overLimitNote()
				}
				return fmt.Sprintf("added %s %q (%s)%s", w.Ref, w.Title, w.State, note), nil
			}},
		{Name: "project_setup", Label: "Set up projects", Kinds: H,
			Description: "Start setting up projects the user just added: each project's agent looks through its folder in the background and proposes work items. What they find (a checklist) and their questions reach the user in Needs you. Only for projects the user wants set up now.",
			Parameters: obj([]string{"projects", "reason"}, map[string]any{
				"projects": aprop("project ids", sprop("")), "reason": reason,
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				if err := needReason(m); err != nil {
					return "", err
				}
				ids := strs(m["projects"])
				if len(ids) == 0 {
					return "", fixErr("projects is required: the ids project_add gave, e.g. [\"gamma\"]")
				}
				var started, already []string
				for _, id := range ids {
					if o, ok := a.Desk.Onboarding(id); ok && !o.Started.IsZero() {
						already = append(already, id)
						continue
					}
					if err := a.startOnboarding(id); err != nil {
						return "", err
					}
					started = append(started, id)
				}
				out := ""
				if len(started) > 0 {
					out = fmt.Sprintf("Setting up %s in the background. Their suggestions and questions will show in Needs you. Tell the user that in one line.", strings.Join(started, ", "))
				}
				if len(already) > 0 {
					out += fmt.Sprintf(" Already set up before: %s (the user can press Set up in the project to look again).", strings.Join(already, ", "))
				}
				return strings.TrimSpace(out), nil
			}},
		{Name: "ask_user", Label: "Ask", Kinds: []string{KindHome, KindProject, KindCustomizer},
			Description: "Ask the user one question, with 2-5 suggested answers they can click (they can also type their own). Use it when you need a decision or a detail. Then end your turn: the answer is their next message.",
			Parameters: obj([]string{"question", "options"}, map[string]any{
				"question": sprop("one short question"),
				"options":  aprop("2-5 short answers to click", sprop("")),
				"multi":    bprop("true if they can pick several (e.g. which items to work on now)"),
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				q := strings.TrimSpace(sArg(m, "question"))
				opts := strs(m["options"])
				if q == "" {
					return "", fixErr("question is required")
				}
				if len(opts) < 2 || len(opts) > 6 {
					return "", fixErr("give 2-5 options to click, e.g. [\"This week\", \"This month\", \"No deadline\"]")
				}
				if len([]rune(q)) > 200 {
					return "", fixErr("the question is %d characters; keep it under 200. Put the background in your message before asking, and ask only the question", len([]rune(q)))
				}
				for _, o := range opts {
					if len([]rune(o)) > 100 {
						return "", fixErr("option %q is too long; keep each under 100 characters, a few words the user can click", oneLine(o, 40))
					}
				}
				return "Asked. The user's answer will be their next message: end your turn now and wait.", nil
			}},
	}
}

// ---- from the shell ----

func (a *App) handleOnboard(t string, m map[string]any) (any, error) {
	switch t {
	case "onboardStart":
		return nil, a.startOnboarding(str(m["project"]))
	case "shapeStart":
		return nil, a.startShaping(str(m["item"]))
	case "proposalsResolve":
		return a.resolveProposals(strs(m["accept"]), strs(m["dismiss"]), m["start"] == true)
	case "workItemAdd":
		return a.workItemAdd("you", str(m["project"]), str(m["title"]), str(m["brief"]), m["start"] == true)
	}
	return nil, fmt.Errorf("unknown %s", t)
}

// cleanList trims a list of short lines and drops empty ones.
func cleanList(l []string) []string {
	var out []string
	for _, s := range l {
		if s = oneLine(strings.TrimSpace(s), 200); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// inProposalOrder sorts proposal ids the way they were proposed.
func (a *App) inProposalOrder(ids []string) []string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, p := range a.Desk.Snapshot().Proposals {
		if want[p.ID] {
			out = append(out, p.ID)
			delete(want, p.ID)
		}
	}
	for _, id := range ids { // unknown ids keep their place at the end
		if want[id] {
			out = append(out, id)
		}
	}
	return out
}
