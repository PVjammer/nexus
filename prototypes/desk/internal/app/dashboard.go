package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
	"github.com/pvjammer/nexus/prototypes/desk/internal/wiki"
)

// Need is one thing waiting on you, from any source.
type Need struct {
	ID      string    `json:"id"`
	Source  string    `json:"source"` // foreman | task | filing
	Project string    `json:"project,omitempty"`
	Title   string    `json:"title"`
	Detail  string    `json:"detail,omitempty"`
	Kind    string    `json:"kind,omitempty"` // foreman inbox kind, or task status
	Time    time.Time `json:"time"`
	Report  string    `json:"report,omitempty"` // app path to open
	Task    string    `json:"task,omitempty"`
	Count   int       `json:"count,omitempty"`
	// Questions: the agent that asked, and the ask_user message (its args
	// carry the question and the answers to click).
	Agent string `json:"agent,omitempty"`
	Msg   int    `json:"msg,omitempty"`
	// Done claims: the work item, and each done-when check with evidence.
	Ref    string      `json:"ref,omitempty"`
	Checks []NeedCheck `json:"checks,omitempty"`
}

// Running is one thing in progress.
type Running struct {
	Source  string    `json:"source"` // foreman | task | agent
	Project string    `json:"project,omitempty"`
	Title   string    `json:"title"`
	Detail  string    `json:"detail,omitempty"`
	Since   time.Time `json:"since"`
	Task    string    `json:"task,omitempty"`
	Agent   string    `json:"agent,omitempty"`
}

func (a *App) needsFor(d projects.Detail) []Need {
	var out []Need
	for _, it := range d.Inbox {
		if a.Desk.Dismissed(it.ID) {
			continue
		}
		n := Need{ID: it.ID, Source: "foreman", Project: d.ID, Title: it.WS, Detail: it.Summary, Kind: it.Kind, Time: it.Time}
		for _, w := range d.Workstreams {
			if w.Name == it.WS && w.Report != "" {
				n.Report = d.ID + "/" + w.Report
			}
		}
		if n.Title == "" {
			n.Title = "foreman"
		}
		out = append(out, n)
	}
	return out
}

func (a *App) needs() []Need {
	var out []Need
	for _, d := range a.allDetails() {
		out = append(out, a.needsFor(d)...)
	}
	snap := a.Desk.Snapshot()
	for _, t := range snap.Tasks {
		if t.Reviewed {
			continue
		}
		switch t.Status {
		case "done", "failed", "needs-input", "stopped":
			out = append(out, Need{ID: "task:" + t.ID, Source: "task", Project: t.Project, Title: t.Title, Detail: t.Summary, Kind: t.Status, Time: t.Updated, Report: t.Report, Task: t.ID})
		}
	}
	for _, p := range a.Projects.List() {
		for _, w := range a.pendingProposals(p.ID) {
			out = append(out, Need{ID: "wiki:" + p.ID + ":" + w.ID, Source: "wiki", Project: p.ID, Title: "Wiki: " + w.Slug,
				Detail: w.Reason, Kind: "proposal", Time: w.Created, Task: w.Task})
		}
	}
	proposed, newest := 0, time.Time{}
	for _, c := range snap.Captures {
		if c.Status == "proposed" {
			proposed++
			if c.Created.After(newest) {
				newest = c.Created
			}
		}
	}
	if proposed > 0 {
		out = append(out, Need{ID: "filing", Source: "filing", Title: "Filing to confirm", Count: proposed, Time: newest})
	}
	out = append(out, a.workNeeds()...)
	out = append(out, a.proposalNeeds()...)
	out = append(out, a.questionNeeds()...)
	out = append(out, a.claimNeeds()...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

func (a *App) running() []Running {
	var out []Running
	for _, d := range a.allDetails() {
		for _, w := range d.Workstreams {
			if !w.Closed && w.AgentStatus == "working" {
				out = append(out, Running{Source: "foreman", Project: d.ID, Title: w.Name, Detail: w.Summary, Since: w.Updated})
			}
		}
	}
	for _, t := range a.Desk.Snapshot().Tasks {
		if t.Status == "running" {
			out = append(out, Running{Source: "task", Project: t.Project, Title: t.Title, Since: t.Updated, Task: t.ID, Agent: "t:" + t.ID})
		}
	}
	for _, r := range a.allRuns() {
		if r.Kind == KindWorkstream && r.isBusy() {
			out = append(out, Running{Source: "workstream", Project: r.Project, Title: r.Title, Detail: "workstream agent", Since: time.Now(), Agent: r.ID})
		}
	}
	return out
}

func (a *App) deskMsg() map[string]any {
	wikis := map[string]any{}
	addons := map[string]any{}
	for _, p := range a.Projects.List() {
		wikis[p.ID] = map[string]any{"pages": a.wikiPages(p.ID), "proposals": a.pendingProposals(p.ID)}
		addons[p.ID] = a.addonStates(p.ID)
	}
	return map[string]any{
		"t": "desk", "desk": a.Desk.Snapshot(), "projects": a.allDetails(),
		"needs": a.needs(), "running": a.running(), "wikis": wikis, "addons": addons,
		"homeKinds": shellCatalog(), "homeUndo": a.homeUndoInfo(), "workItems": a.workItems(), "wipLimit": a.Desk.WipLimit(),
		"customizer": a.customizerState(), "queues": a.queues(),
	}
}

func (a *App) broadcastDesk() {
	a.deskBcast.Lock()
	defer a.deskBcast.Unlock()
	a.broadcast(a.deskMsg()) // built and sent in order: the last one sent is the newest
}

// projectsSummary is a compact, chartable view of every project (for the
// home agent and for dashboard widgets).
func (a *App) projectsSummary() []map[string]any {
	snap := a.Desk.Snapshot()
	var out []map[string]any
	now := time.Now()
	for _, d := range a.allDetails() {
		active, working, open := 0, 0, 0
		for _, w := range d.Workstreams {
			if !w.Closed {
				active++
				if w.AgentStatus == "working" {
					working++
				}
			}
		}
		for _, b := range d.Backlog {
			if !b.Done {
				open++
			}
		}
		items := 0
		for _, it := range snap.Items {
			if it.Project == d.ID && it.State == "open" {
				items++
			}
		}
		// activity per day for the last 14 days (foreman events + commits)
		days := make([]int, 14)
		bump := func(t time.Time) {
			n := int(now.Sub(t).Hours() / 24)
			if n >= 0 && n < 14 {
				days[13-n]++
			}
		}
		for _, e := range d.Events {
			bump(e.Time)
		}
		dirty := 0
		if d.Git != nil {
			dirty = d.Git.Dirty
			for _, c := range d.Git.Commits {
				bump(c.When)
			}
		}
		out = append(out, map[string]any{
			"id": d.ID, "name": d.Name, "brief": oneLine(briefOf(d), 200),
			"workstreams_open": active, "workstreams_working": working, "workstreams_archived": d.Archived,
			"needs_you": len(a.needsFor(d)), "backlog_open": open, "items_open": items,
			"git_dirty": dirty, "last_activity": d.LastActivity, "activity_14d": days,
		})
	}
	return out
}

func (a *App) pendingProposals(pid string) []wiki.Proposal {
	var out []wiki.Proposal
	for _, p := range a.Wiki.Proposals(pid) {
		if p.Status == "pending" {
			out = append(out, p)
		}
	}
	return out
}

// questionNeeds: an agent asked you something and you haven't answered.
// A project's setup can run while you're elsewhere; its question has to
// reach you here, not sit in a chat you haven't opened.
func (a *App) questionNeeds() []Need {
	var out []Need
	for _, r := range a.allRuns() {
		if r.Kind == KindTask {
			continue
		}
		ask, ok := r.pendingQuestion() // the first question since you last said something
		if !ok {
			continue
		}
		var q struct {
			Question string `json:"question"`
		}
		json.Unmarshal([]byte(ask.Args), &q)
		out = append(out, Need{ID: fmt.Sprintf("q:%s:%d", r.ID, ask.ID), Source: "question", Project: r.Project, Kind: "question",
			Title: firstNonEmpty(q.Question, "A question for you"), Detail: "The " + r.label() + " asked.", Time: time.UnixMilli(ask.TS), Agent: r.ID, Msg: ask.ID})
	}
	return out
}
