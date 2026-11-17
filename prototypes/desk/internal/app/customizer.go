package app

// The customizer: a small floating window (hotkey Ctrl+.) where you ask for
// a change to how Nexus looks, then go back to work. Its agent uses the same
// home_* tools as the home agent, but its changes go to a draft copy of the
// layout, not the screen you're using:
//
//   - while it works, the parts it's changing show as under construction;
//   - when its turn ends, small changes (resize, move, remove, reshape) are
//     applied straight away with Undo, like any other change;
//   - anything it builds (a new widget, a new grid) waits: you try it on
//     your real screen, then Keep it or Roll back.
//
// The name is one constant (CustomizerName) so it can change in one place.

import (
	"fmt"
	"strings"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
)

// CustomizerName is what the shell and the agent call the customizer.
const CustomizerName = "Eitri"

const (
	KindCustomizer = "customizer"
	customizerID   = "customizer"
	byCustomizer   = "customizer"
	homeDraft      = "home.draft"
)

// HC: the home screen tools are for the home agent and the customizer.
var HC = []string{KindHome, KindCustomizer}

// byOf is who a tool call is by, for the home_* functions.
func byOf(c *Ctx) string {
	if c != nil && c.Kind == KindCustomizer {
		return byCustomizer
	}
	return "agent"
}

// storedBy is what a widget records as its author.
func storedBy(by string) string {
	if by == "you" || by == "default" {
		return by
	}
	return "agent"
}

// layoutFor is the layout a caller edits: the customizer works on a draft.
func (a *App) layoutFor(by string) string {
	if by == byCustomizer {
		a.Desk.StartDraft(homeDraft, "home")
		return homeDraft
	}
	return "home"
}

func afterNote(c *Ctx) string {
	if byOf(c) == byCustomizer {
		return "This is on the draft: the user sees it when you finish."
	}
	return "The user can undo it."
}

// draftDiff says which widgets the draft adds and which it changes, and
// whether the grid changed.
func (a *App) draftDiff() (added, changed, removed []string, grid bool) {
	live, draft := a.Desk.Layout("home"), a.Desk.Layout(homeDraft)
	byID := map[string]*desk.LayoutWidget{}
	for _, w := range live.Widgets {
		byID[w.ID] = w
	}
	inDraft := map[string]bool{}
	for _, w := range draft.Widgets {
		inDraft[w.ID] = true
	}
	// Order is compared among the widgets both have, so adding one above
	// doesn't count as moving everything below it.
	rank := func(ws []*desk.LayoutWidget, keep func(string) bool) map[string]int {
		out, n := map[string]int{}, 0
		for _, w := range ws {
			if keep(w.ID) {
				out[w.ID] = n
				n++
			}
		}
		return out
	}
	liveRank := rank(live.Widgets, func(id string) bool { return inDraft[id] })
	draftRank := rank(draft.Widgets, func(id string) bool { return byID[id] != nil })
	for _, w := range draft.Widgets {
		old := byID[w.ID]
		if old == nil {
			added = append(added, w.ID)
			continue
		}
		if w.Kind != old.Kind || w.Shape != old.Shape || w.Scope != old.Scope || w.W != old.W || w.H != old.H || liveRank[w.ID] != draftRank[w.ID] {
			changed = append(changed, w.ID)
		}
	}
	for _, w := range live.Widgets {
		if !inDraft[w.ID] {
			removed = append(removed, w.ID)
		}
	}
	grid = live.Cols != draft.Cols || live.Tiling != draft.Tiling
	return
}

// customizerState is what the shell shows: idle, building (the agent is
// working on a draft), or ready (a draft waits for Keep or Roll back).
func (a *App) customizerState() map[string]any {
	st := map[string]any{"name": CustomizerName, "state": "idle"}
	if !a.Desk.HasLayout(homeDraft) {
		return st
	}
	added, changed, removed, grid := a.draftDiff()
	st["added"], st["changed"], st["removed"], st["grid"] = added, changed, removed, grid
	st["draft"] = a.Desk.Layout(homeDraft)
	st["state"] = "ready"
	if r := a.existingRun(customizerID); r != nil && isBusy(r) {
		st["state"] = "building"
	}
	return st
}

func (a *App) broadcastCustomizer() {
	a.broadcast(map[string]any{"t": "customizer", "customizer": a.customizerState()})
}

// customizerSettle runs when the customizer's turn ends. Small changes go
// live at once (with Undo); builds wait for you to try them.
func (a *App) customizerSettle() {
	if !a.Desk.HasLayout(homeDraft) {
		a.broadcastCustomizer()
		return
	}
	added, changed, removed, grid := a.draftDiff()
	switch {
	case len(added)+len(changed)+len(removed) == 0 && !grid:
		a.Desk.DropLayout(homeDraft) // nothing changed after all
	case len(added) == 0 && !grid:
		a.customizerKeep("small change")
		return
	}
	a.broadcastCustomizer()
}

// customizerKeep makes the draft the home screen (one undoable change).
func (a *App) customizerKeep(how string) error {
	if !a.Desk.HasLayout(homeDraft) {
		return fixErr("there's no draft to keep")
	}
	draft, summary := a.Desk.Layout(homeDraft), a.customizerSummary()
	_, err := a.Desk.EditLayout("home", CustomizerName+": "+summary, func(l *desk.Layout, _ func() string) error {
		*l = draft
		return nil
	})
	if err != nil {
		return err
	}
	a.Desk.DropLayout(homeDraft)
	a.Log.Add(actionlog.Entry{Actor: you, Action: "home: kept " + CustomizerName + "'s change", Target: how})
	a.broadcast(map[string]any{"t": "homeChanged", "by": "agent", "who": CustomizerName, "what": summary, "why": ""})
	a.noteCustomizer("Kept: the change is on the home screen.")
	a.broadcastCustomizer()
	return nil
}

func (a *App) customizerRollback() error {
	if !a.Desk.HasLayout(homeDraft) {
		return fixErr("there's no draft to roll back")
	}
	if r := a.existingRun(customizerID); r != nil && isBusy(r) {
		r.Abort()
	}
	a.Desk.DropLayout(homeDraft)
	a.Log.Add(actionlog.Entry{Actor: you, Action: "home: rolled back " + CustomizerName + "'s change"})
	a.noteCustomizer("Rolled back: the home screen is as it was.")
	a.broadcastCustomizer()
	return nil
}

// noteCustomizer adds a line to the customizer's transcript (not sent to the
// model; it sees the screen with home_show next time).
func (a *App) noteCustomizer(text string) {
	if r := a.existingRun(customizerID); r != nil {
		r.add(ChatMsg{Role: "system", Text: text})
	}
}

func (a *App) customizerSummary() string {
	added, changed, removed, grid := a.draftDiff()
	var parts []string
	if n := len(added); n > 0 {
		parts = append(parts, fmt.Sprintf("added %d", n))
	}
	if n := len(changed); n > 0 {
		parts = append(parts, fmt.Sprintf("changed %d", n))
	}
	if n := len(removed); n > 0 {
		parts = append(parts, fmt.Sprintf("removed %d", n))
	}
	if grid {
		parts = append(parts, "new grid")
	}
	if len(parts) == 0 {
		return "home screen"
	}
	return strings.Join(parts, ", ")
}

// customizerContext is what the customizer sees with each request: the
// screen (the draft if one is open), and what the user pointed at.
func (a *App) customizerContext(view, target string) string {
	var b strings.Builder
	name := "home"
	if a.Desk.HasLayout(homeDraft) {
		name = homeDraft
		b.WriteString("A draft of your earlier change is open; you're changing that draft.\n")
	}
	b.WriteString(a.describeLayout(name))
	if view != "" {
		fmt.Fprintf(&b, "\nThe user is on: %s\n", view)
	}
	if target != "" {
		l := a.Desk.Layout(name)
		if i := widgetIndex(&l, target); i >= 0 {
			w := l.Widgets[i]
			fmt.Fprintf(&b, "They pointed at widget %s (%s, %s): \"this\" means that widget.\n", w.ID, w.Kind, w.Shape)
		}
	}
	return "<screen>\n" + strings.TrimSpace(b.String()) + "\n</screen>"
}

func (a *App) handleCustomizer(t string, m map[string]any) error {
	switch t {
	case "customizerKeep":
		return a.customizerKeep("you pressed Keep")
	case "customizerRollback":
		return a.customizerRollback()
	}
	return fmt.Errorf("unknown %s", t)
}
