package app

// Done and closeout.
//
// Only the user says a work item is done: pressing Done is the
// acknowledgement (no popup; the toast has Undo). An agent can only claim it,
// with how each done-when check was met; the claim waits in Needs you for
// Confirm or Not yet. Done starts the closeout: a task agent that follows the
// closeout steps (skills/closeout, or the project's own desk/<project>/
// closeout.md), writes the report, proposes what's left as work items and
// lists loose ends in the repo. Nexus itself archives: the item and its tasks
// leave your lists but nothing is deleted, so Undo reopens it as it was.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/scan"
)

// closeoutFile is a project's own closeout steps, in its Nexus notes. The
// user (or the project agent, asked to) edits it like any note.
const closeoutFile = "closeout.md"

func (a *App) tracked(ref string) desk.Tracked {
	if t := a.Desk.Snapshot().Tracked[ref]; t != nil {
		return *t
	}
	return desk.Tracked{Ref: ref}
}

// closeWorkItem is the user saying a work item is done.
func (a *App) closeWorkItem(ref string) (desk.Tracked, error) {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return desk.Tracked{}, err
	}
	if w.State == "done" {
		return a.tracked(w.Ref), nil
	}
	claim := a.tracked(w.Ref).Claim
	t := a.Desk.Track(w.Ref, func(t *desk.Tracked) {
		t.PrevState, t.State, t.Closed, t.Claim, t.ParkNote = w.State, "done", time.Now(), nil, ""
	})
	if id, err := a.startCloseout(w); err != nil {
		log.Printf("closeout %s: %v", w.Ref, err)
	} else {
		t = a.Desk.Track(w.Ref, func(t *desk.Tracked) { t.Closeout = id })
	}
	a.workChanged("you", "marked done", w.Title, "")
	if r := a.existingRun(workstreamID(w.Ref)); r != nil { // its workstream closes; the conversation is kept
		r.Close()
	}
	if claim != nil {
		a.tellAgent(claim.Agent, fmt.Sprintf("The user confirmed %q is done. Nexus is closing it out (report, what's left, loose ends); nothing more for you to do on it.", w.Title))
	}
	return t, nil
}

// reopenWorkItem is Undo, or Reopen from the Done list: back to the state it
// had, with its tasks. A closeout still running is stopped; its report stays.
func (a *App) reopenWorkItem(ref string) (desk.Tracked, error) {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return desk.Tracked{}, err
	}
	if w.State != "done" {
		return desk.Tracked{}, fixErr("%q isn't done", w.Title)
	}
	if id := a.tracked(w.Ref).Closeout; id != "" {
		a.stopTask(id)
		a.Desk.UpdateTask(id, func(t *desk.Task) error { t.Reviewed = true; return nil })
	}
	t := a.Desk.Track(w.Ref, func(t *desk.Tracked) {
		t.State, t.PrevState, t.Closed, t.Closeout = firstNonEmpty(t.PrevState, "in-progress"), "", time.Time{}, ""
	})
	a.workChanged("you", "reopened", w.Title, "")
	return t, nil
}

// claimDone is an agent saying a work item is done. It asks the user.
func (a *App) claimDone(r *Run, project, ref string, evidence []string, note string) (string, error) {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return "", err
	}
	if project != "" && w.Project != project {
		return "", deny("work item %s is in project %s, not yours (%s)", ref, w.Project, project)
	}
	if w.State == "done" {
		return fmt.Sprintf("%q is already done (the user said so).", w.Title), nil
	}
	a.Desk.Track(w.Ref, func(t *desk.Tracked) {
		t.Claim = &desk.DoneClaim{Agent: r.ID, By: r.label(), Note: oneLine(note, 300), Evidence: evidence, At: time.Now()}
	})
	a.workChanged(r.label(), "says it's done", w.Title, note)
	return fmt.Sprintf("Asked the user to confirm %q is done: it's in their Needs you list with your evidence. Only they can say it's done, so don't tell them it is; you'll be told what they decide.", w.Title), nil
}

// notDone is the user answering an agent's claim with Not yet.
func (a *App) notDone(ref, reason string) error {
	w, err := a.checkItemRef(ref)
	if err != nil {
		return err
	}
	claim := a.tracked(w.Ref).Claim
	if claim == nil {
		return fixErr("nobody has said %q is done", w.Title)
	}
	a.Desk.Track(w.Ref, func(t *desk.Tracked) { t.Claim = nil })
	a.workChanged("you", "said it isn't done yet", w.Title, reason)
	msg := fmt.Sprintf("The user says %q isn't done yet.", w.Title)
	if reason = strings.TrimSpace(reason); reason != "" {
		msg += " What's missing: " + reason
	}
	a.tellAgent(claim.Agent, msg)
	return nil
}

// NeedCheck is one done-when check with how the agent says it was met.
type NeedCheck struct {
	Text     string `json:"text"`
	Evidence string `json:"evidence,omitempty"`
}

func (a *App) claimNeeds() []Need {
	snap := a.Desk.Snapshot()
	var out []Need
	for _, w := range a.workItems() {
		tr := snap.Tracked[w.Ref]
		if w.State == "done" || tr == nil || tr.Claim == nil {
			continue
		}
		c := tr.Claim
		var checks []NeedCheck
		for i, d := range w.DoneWhen {
			ck := NeedCheck{Text: d}
			if i < len(c.Evidence) {
				ck.Evidence = c.Evidence[i]
			}
			checks = append(checks, ck)
		}
		for i := len(w.DoneWhen); i < len(c.Evidence); i++ { // evidence with no check to go with it
			checks = append(checks, NeedCheck{Evidence: c.Evidence[i]})
		}
		detail := "The " + c.By + " says it's done."
		if c.Note != "" {
			detail = "The " + c.By + " says it's done: " + c.Note
		}
		out = append(out, Need{ID: "done:" + w.Ref, Source: "done", Project: w.Project, Kind: "claim", Title: w.Title,
			Detail: detail, Time: c.At, Ref: w.Ref, Checks: checks, Agent: c.Agent})
	}
	return out
}

// ---- the closeout run ----

// closeoutSteps is how this project closes out work: its own note if it has
// one, else the default skill.
func (a *App) closeoutSteps(pid string) (text, from string) {
	if b, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "desk-files", pid, closeoutFile)); err == nil && strings.TrimSpace(string(b)) != "" {
		return strings.TrimSpace(string(b)), "desk/" + pid + "/" + closeoutFile
	}
	s, err := a.renderSkill("closeout")
	if err != nil {
		return "1. Write the report with report_write: what was done, decisions, what's left, loose ends.\n2. Propose what's left as work items with work_propose.\n3. End with task_finish.", "built in"
	}
	return strings.TrimSpace(frontmatter.ReplaceAllString(s, "")), "skills/closeout"
}

func (a *App) closeoutBrief(w WorkItem) string {
	var b strings.Builder
	p, _ := a.Projects.Get(w.Project)
	fmt.Fprintf(&b, "The user marked **%s** done (%s, project %s). Close it out.\n\n", w.Title, w.Ref, firstNonEmpty(p.Name, w.Project))
	if w.Brief != "" {
		fmt.Fprintf(&b, "**Brief:** %s\n\n", w.Brief)
	}
	claim := a.tracked(w.Ref).Claim
	if len(w.DoneWhen) > 0 {
		b.WriteString("**Done when:**\n")
		for i, d := range w.DoneWhen {
			ev := ""
			if claim != nil && i < len(claim.Evidence) {
				ev = " (the agent's evidence: " + claim.Evidence[i] + ")"
			}
			fmt.Fprintf(&b, "- %s%s\n", d, ev)
		}
		b.WriteString("\n")
	}
	var done, open []string
	for _, t := range a.Desk.Snapshot().WorkTasks {
		if t.Item != w.Ref {
			continue
		}
		if t.Status == "done" {
			done = append(done, t.Title)
		} else {
			open = append(open, fmt.Sprintf("%s (%s, %s)", t.Title, statusName[t.Status], t.Assignee))
		}
	}
	if len(done) > 0 {
		fmt.Fprintf(&b, "**Tasks done:** %s\n\n", strings.Join(done, "; "))
	}
	if len(open) > 0 {
		fmt.Fprintf(&b, "**Tasks still open when it was closed:** %s\n\n", strings.Join(open, "; "))
	}
	if w.Report != "" {
		fmt.Fprintf(&b, "**Its earlier report:** %s\n\n", w.Report)
	}
	if n := len(a.chatOf(workstreamID(w.Ref))); n > 0 {
		fmt.Fprintf(&b, "**Its workstream conversation:** %d messages. Read it with ws_transcript item=%q (query \"decid\" finds decisions).\n\n", n, w.Ref)
	}
	b.WriteString("**Loose ends in the repo** (found by Nexus, which never changes the repo):\n")
	b.WriteString(a.looseEnds(w.Project))
	steps, from := a.closeoutSteps(w.Project)
	fmt.Fprintf(&b, "\n## How to close out (from %s)\n\n%s\n", from, steps)
	return b.String()
}

// looseEnds lists unmerged branches and uncommitted changes, for the
// closeout to mention as commands or tasks for the user.
func (a *App) looseEnds(pid string) string {
	p, ok := a.Projects.Get(pid)
	if !ok {
		return "- (project not found)\n"
	}
	r := scan.Project(p.Path)
	var b strings.Builder
	for i, br := range r.Branches {
		if i == 8 {
			fmt.Fprintf(&b, "- … %d more branches\n", len(r.Branches)-i)
			break
		}
		fmt.Fprintf(&b, "- %s: %s\n", br.Where, br.Text)
	}
	if r.Changes != nil {
		fmt.Fprintf(&b, "- uncommitted: %s\n", r.Changes.Text)
	}
	if b.Len() == 0 {
		return "- none found\n"
	}
	return b.String()
}

func (a *App) startCloseout(w WorkItem) (string, error) {
	t := a.Desk.AddTask(deskTask(w.Project, "Close out: "+truncate(w.Title, 60), a.closeoutBrief(w), "closeout:"+w.Ref))
	if a.cfg.Agents == nil { // tests without an agent loop: the draft is the record
		return t.ID, nil
	}
	return t.ID, a.startTask(t.ID)
}

func isCloseout(t desk.Task) bool { return strings.HasPrefix(t.From, "closeout:") }

// ---- from the shell ----

func (a *App) handleDone(t string, m map[string]any) (any, error) {
	switch t {
	case "workItemDone":
		return a.closeWorkItem(str(m["item"]))
	case "workItemReopen":
		return a.reopenWorkItem(str(m["item"]))
	case "workItemNotDone":
		return nil, a.notDone(str(m["item"]), str(m["reason"]))
	}
	return nil, fmt.Errorf("unknown %s", t)
}
