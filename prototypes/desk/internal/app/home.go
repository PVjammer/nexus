package app

// The home screen is a layout of widgets, stored as data. You (Customize
// manually) and the home agent (home_* tools) change it through the same
// functions here, so neither can do anything the other can't, every change
// is validated the same way, and every change can be undone.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
)

// WidgetKind is one entry in the widget catalog: what a widget can show.
// The catalog is the single source of truth: the UI's "Add a widget" list,
// the tool schemas and the agents' skill text are all generated from it.
type WidgetKind struct {
	Kind     string   `json:"kind"`
	Label    string   `json:"label"`
	Shows    string   `json:"shows"`
	Shapes   []string `json:"shapes"` // the first is the default
	Scoped   bool     `json:"scoped"` // can be narrowed to one project
	DefaultW int      `json:"defaultW"`
	Internal bool     `json:"internal,omitempty"` // not offered in Add a widget (plugins come from widget_write)
}

var widgetKinds = []WidgetKind{
	{Kind: "chat", Label: "Agent", Shows: "a conversation with an agent: the home agent, or a project's agent when narrowed to that project", Shapes: []string{"minimal", "conversation"}, Scoped: true, DefaultW: 4},
	{Kind: "needs", Label: "Needs you", Shows: "everything waiting on the user: foreman questions and reports, finished tasks, wiki proposals, filings to confirm", Shapes: []string{"list", "compact"}, Scoped: true, DefaultW: 2},
	{Kind: "running", Label: "Running", Shows: "agents and foreman workstreams working right now", Shapes: []string{"list", "compact"}, Scoped: true, DefaultW: 1},
	{Kind: "tasks", Label: "Tasks", Shows: "tasks dispatched to background agents (drafts, running, finished)", Shapes: []string{"list", "board"}, Scoped: true, DefaultW: 2},
	{Kind: "items", Label: "Items", Shows: "open items (tasks, notes, questions) in the user's projects", Shapes: []string{"list", "compact"}, Scoped: true, DefaultW: 1},
	{Kind: "workstreams", Label: "Workstreams", Shows: "open foreman workstreams with their status", Shapes: []string{"list", "compact"}, Scoped: true, DefaultW: 2},
	{Kind: "inbox", Label: "Inbox", Shows: "captures waiting to be filed, with the home agent's proposals", Shapes: []string{"list"}, DefaultW: 1},
	{Kind: "capture", Label: "Capture", Shows: "a box to jot down a thought; the home agent proposes where it goes", Shapes: []string{"box"}, DefaultW: 1},
	{Kind: "projects", Label: "Projects", Shows: "the user's projects with their counts", Shapes: []string{"cards", "list"}, DefaultW: 4},
	{Kind: "plugin", Label: "Plugin", Shows: "a sandboxed widget built as a plugin (widget_write makes one)", Shapes: []string{"frame"}, DefaultW: 1, Internal: true},
}

var allShapes = func() []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range widgetKinds {
		for _, s := range k.Shapes {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}()

func kindByName(k string) (WidgetKind, bool) {
	for _, x := range widgetKinds {
		if x.Kind == k {
			return x, true
		}
	}
	return WidgetKind{}, false
}

func kindNames(includeInternal bool) []string {
	var out []string
	for _, k := range widgetKinds {
		if includeInternal || !k.Internal {
			out = append(out, k.Kind)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// HomeEdit is one change to the home screen, from you or the agent. Fields
// left nil are unchanged.
type HomeEdit struct {
	Kind     string
	Shape    *string
	Project  *string // "" or "all" = everything; else a project id
	Plugin   string
	W, H     *int
	Position *int // 1-based; 1 = top
	Why      string
}

func intp(v int) *int       { return &v }
func strp(v string) *string { return &v }

// fixErr is a validation error that says how to fix the call. Small models
// recover from these far better than from a bare "invalid".
func fixErr(format string, args ...any) error { return fmt.Errorf(format, args...) }

func (a *App) checkShape(k WidgetKind, shape string) error {
	if !contains(k.Shapes, shape) {
		return fixErr("shape %q doesn't fit a %s widget. Use one of: %s", shape, k.Kind, strings.Join(k.Shapes, ", "))
	}
	return nil
}

func (a *App) scopeFor(k WidgetKind, project string) (string, error) {
	project = strings.TrimSpace(project)
	if project == "" || project == "all" || project == "everything" {
		return "", nil
	}
	project = strings.TrimPrefix(project, "p:")
	if !k.Scoped {
		return "", fixErr("a %s widget always shows everything; leave project out", k.Kind)
	}
	if _, ok := a.Projects.Get(project); !ok {
		var ids []string
		for _, p := range a.Projects.List() {
			ids = append(ids, p.ID)
		}
		return "", fixErr("no project %q. Project ids: %s (or leave project out for everything)", project, strings.Join(ids, ", "))
	}
	return "p:" + project, nil
}

func checkSize(name string, v, lo, hi int) error {
	if v < lo || v > hi {
		return fixErr("%s must be %d to %d (got %d)", name, lo, hi, v)
	}
	return nil
}

func placeAt(ws []*desk.LayoutWidget, w *desk.LayoutWidget, pos int) []*desk.LayoutWidget {
	if pos < 1 || pos > len(ws)+1 {
		pos = len(ws) + 1
	}
	out := append([]*desk.LayoutWidget{}, ws[:pos-1]...)
	out = append(out, w)
	return append(out, ws[pos-1:]...)
}

// widgetIndex finds a widget by id. As a convenience for small models, a
// kind name ("needs") also works when exactly one widget of that kind is on
// the screen.
func widgetIndex(l *desk.Layout, id string) int {
	id = strings.TrimSpace(id)
	for i, w := range l.Widgets {
		if w.ID == id {
			return i
		}
	}
	found := -1
	for i, w := range l.Widgets {
		if w.Kind == id {
			if found >= 0 {
				return -1 // ambiguous; noWidget lists the ids
			}
			found = i
		}
	}
	return found
}

func noWidget(l *desk.Layout, id string) error {
	var have []string
	for _, w := range l.Widgets {
		have = append(have, w.ID+" "+w.Kind)
	}
	if len(have) == 0 {
		return fixErr("no widget %q: the home screen is empty", id)
	}
	return fixErr("no widget %q (or more than one of that kind). Use an id. Widgets now: %s", id, strings.Join(have, ", "))
}

// homeAdd adds a widget. by is "you" or "agent".
func (a *App) homeAdd(by string, e HomeEdit) (desk.LayoutWidget, error) {
	k, ok := kindByName(e.Kind)
	if !ok {
		return desk.LayoutWidget{}, fixErr("no widget kind %q. Kinds: %s", e.Kind, strings.Join(kindNames(false), ", "))
	}
	if k.Kind == "plugin" {
		if e.Plugin == "" {
			return desk.LayoutWidget{}, fixErr("a plugin widget needs plugin (a plugin id from plugin_list)")
		}
		if _, ok := a.Plugins.Get(e.Plugin); !ok {
			return desk.LayoutWidget{}, fixErr("no plugin %q; plugin_list shows the ids", e.Plugin)
		}
	}
	w := desk.LayoutWidget{Kind: k.Kind, Shape: k.Shapes[0], W: k.DefaultW, By: by, Why: strings.TrimSpace(e.Why), Plugin: e.Plugin, Added: time.Now()}
	if e.Shape != nil && *e.Shape != "" {
		if err := a.checkShape(k, *e.Shape); err != nil {
			return w, err
		}
		w.Shape = *e.Shape
	}
	if e.Project != nil {
		sc, err := a.scopeFor(k, *e.Project)
		if err != nil {
			return w, err
		}
		w.Scope = sc
	}
	if e.W != nil {
		if err := checkSize("w", *e.W, 1, 4); err != nil {
			return w, err
		}
		w.W = *e.W
	}
	if e.H != nil {
		if err := checkSize("h", *e.H, 0, 4); err != nil {
			return w, err
		}
		w.H = *e.H
	}
	pos := 0
	if e.Position != nil {
		pos = *e.Position
	}
	_, err := a.Desk.EditLayout("home", by+": add "+k.Label, func(l *desk.Layout, newID func() string) error {
		w.ID = newID()
		c := w
		l.Widgets = placeAt(l.Widgets, &c, pos)
		return nil
	})
	if err == nil {
		a.homeChanged(by, "added "+k.Label, w.Why)
	}
	return w, err
}

// homeChange changes a widget's shape, scope, size or position.
func (a *App) homeChange(by, id string, e HomeEdit) error {
	var label string
	_, err := a.Desk.EditLayout("home", by+": change widget", func(l *desk.Layout, _ func() string) error {
		i := widgetIndex(l, id)
		if i < 0 {
			return noWidget(l, id)
		}
		w := l.Widgets[i]
		k, _ := kindByName(w.Kind)
		label = k.Label
		if e.Shape != nil && *e.Shape != "" {
			if err := a.checkShape(k, *e.Shape); err != nil {
				return err
			}
			w.Shape = *e.Shape
		}
		if e.Project != nil {
			sc, err := a.scopeFor(k, *e.Project)
			if err != nil {
				return err
			}
			w.Scope = sc
		}
		if e.W != nil {
			if err := checkSize("w", *e.W, 1, 4); err != nil {
				return err
			}
			w.W = *e.W
		}
		if e.H != nil {
			if err := checkSize("h", *e.H, 0, 4); err != nil {
				return err
			}
			w.H = *e.H
		}
		if by == "agent" {
			w.By, w.Why = "agent", strings.TrimSpace(e.Why)
		}
		if e.Position != nil {
			if err := checkSize("position", *e.Position, 1, len(l.Widgets)); err != nil {
				return err
			}
			rest := append(append([]*desk.LayoutWidget{}, l.Widgets[:i]...), l.Widgets[i+1:]...)
			l.Widgets = placeAt(rest, w, *e.Position)
		}
		return nil
	})
	if err == nil {
		a.homeChanged(by, "changed "+label, e.Why)
	}
	return err
}

func (a *App) homeRemove(by, id, why string) error {
	var label string
	_, err := a.Desk.EditLayout("home", by+": remove widget", func(l *desk.Layout, _ func() string) error {
		i := widgetIndex(l, id)
		if i < 0 {
			return noWidget(l, id)
		}
		k, _ := kindByName(l.Widgets[i].Kind)
		label = k.Label
		l.Widgets = append(l.Widgets[:i], l.Widgets[i+1:]...)
		return nil
	})
	if err == nil {
		a.homeChanged(by, "removed "+label, why)
	}
	return err
}

func (a *App) homeGrid(by string, cols *int, tiling, why string) error {
	if cols != nil {
		if err := checkSize("cols", *cols, 1, 4); err != nil {
			return err
		}
	}
	if tiling != "" && tiling != "flow" && tiling != "tile" {
		return fixErr("tiling must be flow (the page scrolls) or tile (widgets fill the screen)")
	}
	_, err := a.Desk.EditLayout("home", by+": grid", func(l *desk.Layout, _ func() string) error {
		if cols != nil {
			l.Cols = *cols
		}
		if tiling != "" {
			l.Tiling = tiling
		}
		return nil
	})
	if err == nil {
		a.homeChanged(by, "changed the grid", why)
	}
	return err
}

// homePresets are whole layouts to start from.
var homePresets = []string{"prompt", "dashboard"}

func (a *App) homeReset(by, to, why string) error {
	if !contains(homePresets, to) {
		return fixErr("to must be one of: %s", strings.Join(homePresets, ", "))
	}
	now := time.Now()
	_, err := a.Desk.EditLayout("home", by+": reset to "+to, func(l *desk.Layout, newID func() string) error {
		var chat *desk.LayoutWidget
		var plugins []*desk.LayoutWidget
		for _, w := range l.Widgets {
			if w.Kind == "chat" && w.Scope == "" && chat == nil {
				chat = w
			}
			if w.Kind == "plugin" && to == "dashboard" {
				plugins = append(plugins, w)
			}
		}
		if chat == nil {
			chat = &desk.LayoutWidget{ID: newID(), Kind: "chat", Shape: "minimal", W: 4, By: by, Added: now}
		}
		*l = desk.Layout{Cols: 3, Tiling: "flow", Widgets: []*desk.LayoutWidget{chat}}
		if to == "dashboard" {
			for _, s := range []struct {
				kind, shape string
				w           int
			}{{"needs", "list", 2}, {"running", "list", 1}, {"inbox", "list", 1}, {"workstreams", "compact", 2}, {"projects", "cards", 3}} {
				l.Widgets = append(l.Widgets, &desk.LayoutWidget{ID: newID(), Kind: s.kind, Shape: s.shape, W: s.w, By: by, Why: why, Added: now})
			}
			l.Widgets = append(l.Widgets, plugins...)
		}
		return nil
	})
	if err == nil {
		a.homeChanged(by, "reset home to "+to, why)
	}
	return err
}

func (a *App) homeUndo(by string) (string, error) {
	label, ok := a.Desk.UndoLayout("home")
	if !ok {
		return "", fixErr("nothing to undo on the home screen")
	}
	a.homeChanged(by, "undid: "+label, "")
	return label, nil
}

// homeChanged logs a change and tells the shell, which offers Undo when the
// agent made it.
func (a *App) homeChanged(by, what, why string) {
	actor := you
	if by == "agent" {
		actor = "home agent"
	}
	a.Log.Add(actionlog.Entry{Actor: actor, Action: "home: " + what, Target: truncate(why, 80)})
	if by == "agent" {
		a.broadcast(map[string]any{"t": "homeChanged", "by": by, "what": what, "why": why})
	}
}

// describeHome is the layout as short text: for home_show and the home
// agent's turn context.
func (a *App) describeHome() string {
	l := a.Desk.Layout("home")
	var b strings.Builder
	fmt.Fprintf(&b, "home screen: %d columns, %s tiling, %d widget(s)\n", l.Cols, l.Tiling, len(l.Widgets))
	for i, w := range l.Widgets {
		scope := "everything"
		if w.Scope != "" {
			scope = "project " + strings.TrimPrefix(w.Scope, "p:")
		}
		if k, ok := kindByName(w.Kind); ok && !k.Scoped {
			scope = ""
		}
		h := "auto height"
		if w.H > 0 {
			h = fmt.Sprintf("%d rows tall", w.H)
		}
		extra := ""
		if w.Plugin != "" {
			extra = " plugin=" + w.Plugin
		}
		if scope != "" {
			scope = ", " + scope
		}
		by := "added by " + w.By
		if w.By == "default" {
			by = "default"
		}
		wide := "full width"
		if w.W < l.Cols {
			wide = fmt.Sprintf("%d of %d columns", w.W, l.Cols)
		}
		fmt.Fprintf(&b, "%d. id=%s %s (%s)%s%s, %s, %s, %s\n", i+1, w.ID, w.Kind, w.Shape, extra, scope, wide, h, by)
	}
	return b.String()
}

// homeCatalog is the catalog as text, for the skill.
func homeCatalog() string {
	var b strings.Builder
	for _, k := range widgetKinds {
		if k.Internal {
			continue
		}
		scope := "always everything"
		if k.Scoped {
			scope = "can be narrowed to one project"
		}
		fmt.Fprintf(&b, "- `%s` (%s): %s. Shapes: %s (default %s). %s. Default width %d.\n", k.Kind, k.Label, k.Shows, strings.Join(k.Shapes, ", "), k.Shapes[0], strings.ToUpper(scope[:1])+scope[1:], k.DefaultW)
	}
	return b.String()
}

// ---- tools (home agent) ----

func (a *App) homeTools() []Tool {
	reason := sprop("why, in a few words of the user's request (shown to the user)")
	pos := iprop("1-based position; 1 = top. Position 2 is right under the prompt box. Leave out to add at the end")
	return []Tool{
		{Name: "home_show", Label: "Home screen", Kinds: H,
			Description: "See the home screen: its grid and each widget (id, kind, shape, scope, size), top to bottom. Call it before changing the home screen.",
			Parameters:  obj(nil, map[string]any{}), run: func(a *App, _ *Ctx, _ map[string]any) (string, error) { return a.describeHome(), nil }},
		{Name: "home_add", Label: "Add widget", Kinds: H,
			Description: "Add a widget to the home screen. Only when the user asks for something on their home screen or dashboard.",
			Parameters: obj([]string{"kind", "reason"}, map[string]any{
				"kind":     eprop("what it shows (see the home-layout skill for each)", kindNames(false)...),
				"shape":    eprop("how it's drawn; must fit the kind. Leave out for the default", allShapes...),
				"project":  sprop("a project id to show only that project; leave out for everything"),
				"w":        iprop("columns wide, 1-4 (4 = full width). Leave out for the default"),
				"h":        iprop("rows tall, 1-4; 0 = as tall as it needs (default)"),
				"position": pos, "reason": reason,
			}), run: toolHomeAdd},
		{Name: "home_change", Label: "Change widget", Kinds: H,
			Description: "Change one widget: its shape, project, width, height or position. Give only what changes.",
			Parameters: obj([]string{"id", "reason"}, map[string]any{
				"id":       sprop("widget id from home_show, e.g. w12"),
				"shape":    eprop("new shape; must fit the kind", allShapes...),
				"project":  sprop("a project id, or 'all' for everything"),
				"w":        iprop("columns wide, 1-4"),
				"h":        iprop("rows tall, 1-4; 0 = as tall as it needs"),
				"position": pos, "reason": reason,
			}), run: toolHomeChange},
		{Name: "home_remove", Label: "Remove widget", Kinds: H,
			Description: "Take a widget off the home screen. Its data isn't touched.",
			Parameters:  obj([]string{"id", "reason"}, map[string]any{"id": sprop("widget id from home_show"), "reason": reason}), run: toolHomeRemove},
		{Name: "home_grid", Label: "Home grid", Kinds: H,
			Description: "Set the home grid: how many columns, and the tiling (flow: the page scrolls; tile: widgets fill the screen like a tiling window manager).",
			Parameters: obj([]string{"reason"}, map[string]any{
				"cols": iprop("1-4"), "tiling": eprop("", "flow", "tile"), "reason": reason,
			}), run: toolHomeGrid},
		{Name: "home_reset", Label: "Reset home", Kinds: H,
			Description: "Replace the whole home screen with a preset: 'prompt' (just the prompt box) or 'dashboard' (prompt, needs you, running, inbox, workstreams, projects). Plugin widgets stay with 'dashboard'.",
			Parameters:  obj([]string{"to", "reason"}, map[string]any{"to": eprop("", homePresets...), "reason": reason}), run: toolHomeReset},
		{Name: "home_undo", Label: "Undo home change", Kinds: H,
			Description: "Undo the last change to the home screen (yours or the user's). Use when the user says 'undo that' about the home screen.",
			Parameters:  obj(nil, map[string]any{}), run: func(a *App, _ *Ctx, _ map[string]any) (string, error) {
				l, err := a.homeUndo("agent")
				if err != nil {
					return "", err
				}
				return "undid: " + l + "\n" + a.describeHome(), nil
			}},
	}
}

// optInt reads an optional integer argument (nil when absent).
func optInt(m map[string]any, k string) *int {
	if _, ok := m[k]; !ok || m[k] == nil {
		return nil
	}
	v := iArg(m, k, -999)
	if v == -999 {
		return nil
	}
	return &v
}

func optStr(m map[string]any, k string) *string {
	if _, ok := m[k]; !ok || m[k] == nil {
		return nil
	}
	v := sArg(m, k)
	return &v
}

func editFromArgs(m map[string]any) HomeEdit {
	return HomeEdit{Kind: sArg(m, "kind"), Shape: optStr(m, "shape"), Project: optStr(m, "project"), Plugin: sArg(m, "plugin"),
		W: optInt(m, "w"), H: optInt(m, "h"), Position: optInt(m, "position"), Why: sArg(m, "reason")}
}

func needReason(m map[string]any) error {
	if sArg(m, "reason") == "" {
		return fixErr("reason is required: a few words on why, from the user's request")
	}
	return nil
}

func toolHomeAdd(a *App, _ *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	w, err := a.homeAdd("agent", editFromArgs(m))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added %s (%s %s). The user can undo it.\n%s", w.ID, w.Kind, w.Shape, a.describeHome()), nil
}

func toolHomeChange(a *App, _ *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	e := editFromArgs(m)
	if e.Shape == nil && e.Project == nil && e.W == nil && e.H == nil && e.Position == nil {
		return "", fixErr("nothing to change: give shape, project, w, h or position")
	}
	if err := a.homeChange("agent", sArg(m, "id"), e); err != nil {
		return "", err
	}
	return "changed. " + a.describeHome(), nil
}

func toolHomeRemove(a *App, _ *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	if err := a.homeRemove("agent", sArg(m, "id"), sArg(m, "reason")); err != nil {
		return "", err
	}
	return "removed. " + a.describeHome(), nil
}

func toolHomeGrid(a *App, _ *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	cols := optInt(m, "cols")
	if cols == nil && sArg(m, "tiling") == "" {
		return "", fixErr("give cols (1-4) and/or tiling (flow or tile)")
	}
	if err := a.homeGrid("agent", cols, sArg(m, "tiling"), sArg(m, "reason")); err != nil {
		return "", err
	}
	return "done. " + a.describeHome(), nil
}

func toolHomeReset(a *App, _ *Ctx, m map[string]any) (string, error) {
	if err := needReason(m); err != nil {
		return "", err
	}
	if err := a.homeReset("agent", sArg(m, "to"), sArg(m, "reason")); err != nil {
		return "", err
	}
	return "done. " + a.describeHome(), nil
}

// ---- from the shell (you) ----

func (a *App) handleHome(t string, m map[string]any) error {
	e := HomeEdit{Kind: str(m["kind"]), Plugin: str(m["plugin"])}
	if v, ok := m["shape"].(string); ok {
		e.Shape = &v
	}
	if v, ok := m["scope"].(string); ok {
		e.Project = strp(strings.TrimPrefix(v, "p:"))
	}
	if _, ok := m["w"]; ok {
		e.W = intp(iArg(m, "w", 1))
	}
	if _, ok := m["h"]; ok {
		e.H = intp(iArg(m, "h", 0))
	}
	if _, ok := m["position"]; ok {
		e.Position = intp(iArg(m, "position", 0))
	}
	switch t {
	case "homeAdd":
		_, err := a.homeAdd("you", e)
		return err
	case "homeChange":
		return a.homeChange("you", str(m["id"]), e)
	case "homeRemove":
		return a.homeRemove("you", str(m["id"]), "")
	case "homeGrid":
		var cols *int
		if _, ok := m["cols"]; ok {
			cols = intp(iArg(m, "cols", 3))
		}
		return a.homeGrid("you", cols, str(m["tiling"]), "")
	case "homeReset":
		return a.homeReset("you", str(m["to"]), "")
	case "homeUndo":
		_, err := a.homeUndo("you")
		return err
	}
	return fmt.Errorf("unknown %s", t)
}

// homeUndoInfo is for the shell's Undo button.
func (a *App) homeUndoInfo() map[string]any {
	n, label := a.Desk.UndoInfo("home")
	return map[string]any{"depth": n, "label": label}
}

// sortedKinds is the catalog for the shell (stable order).
func shellCatalog() []WidgetKind {
	out := append([]WidgetKind(nil), widgetKinds...)
	sort.SliceStable(out, func(i, j int) bool { return !out[i].Internal && out[j].Internal })
	return out
}
