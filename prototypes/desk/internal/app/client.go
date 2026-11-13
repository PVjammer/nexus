package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
	"github.com/pvjammer/nexus/prototypes/desk/internal/review"
	"github.com/pvjammer/nexus/prototypes/desk/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/desk/internal/store"
)

const you = store.ActorUser

// projectOfPath maps an app path to its project ("<project>/…" or "desk/<project>/…").
func (a *App) projectOfPath(p string) (projects.Project, bool) {
	r := rootOf(p)
	if r == "desk" {
		parts := strings.SplitN(strings.TrimPrefix(p, "desk/"), "/", 2)
		r = parts[0]
	}
	return a.Projects.Get(r)
}

// handleClient handles one message from the shell (always the user).
func (a *App) handleClient(c *client, m map[string]any, raw []byte) {
	t := str(m["t"])
	reply := func(v map[string]any) {
		if rid := str(m["rid"]); rid != "" {
			v["rid"] = rid
		}
		a.sendTo(c, v)
	}
	fail := func(err error) { reply(map[string]any{"t": "error", "error": err.Error()}) }
	space := str(m["space"])
	logUser := func(action, target, detail string) {
		a.Log.Add(actionlog.Entry{Actor: you, Action: action, Target: target, Detail: detail, Space: space})
	}
	switch t {
	// ---- chat ----
	case "chat":
		go a.Chat(str(m["agent"]), str(m["text"]), str(m["selection"]), str(m["view"]))
	case "abort":
		if r := a.existingRun(str(m["agent"])); r != nil {
			r.Abort()
		}

	// ---- projects and the desk ----
	case "discover":
		reply(map[string]any{"t": "candidates", "candidates": projects.Discover(a.cfg.ScanRoots, 6)})
	case "projectAdd":
		p, err := a.addProject(str(m["name"]), str(m["path"]), str(m["brief"]), you)
		if err != nil {
			fail(err)
			return
		}
		reply(map[string]any{"t": "projectAdded", "project": p.ID})
	case "projectRemove":
		id := str(m["project"])
		p, err := a.Projects.Remove(id)
		if err != nil {
			fail(err)
			return
		}
		if r := a.existingRun("p:" + id); r != nil {
			r.Close()
		}
		for _, pn := range a.Store.RemoveSpace(p.Space) {
			a.cleanupPane(pn)
		}
		a.Roots.Remove(id)
		logUser("remove project", p.Name, "Nexus forgets it; the folder is untouched")
		a.refreshDetails()
		a.broadcastState()
		a.broadcastDesk()
	case "projectBrief":
		a.Projects.Update(str(m["project"]), func(p *projects.Project) { p.Brief = strings.TrimSpace(str(m["brief"])) })
		a.refreshDetails()
		a.broadcastDesk()
	case "capture":
		caps := a.Desk.AddCaptures(str(m["text"]))
		if len(caps) == 0 {
			return
		}
		logUser("capture", fmt.Sprintf("%d note(s)", len(caps)), "")
		if len(a.Projects.List()) > 0 {
			var lines []string
			for _, cp := range caps {
				lines = append(lines, fmt.Sprintf("- %s: %s", cp.ID, cp.Text))
			}
			go a.appPrompt("home", fmt.Sprintf("Captured %d note(s). Propose where each belongs (capture_propose):\n%s", len(caps), strings.Join(lines, "\n")))
		}
	case "fileCapture":
		it, err := a.Desk.File(str(m["capture"]), str(m["project"]), str(m["kind"]), strings.TrimSpace(str(m["title"])))
		if err != nil {
			fail(err)
			return
		}
		logUser("file capture", it.Title, "→ "+it.Project)
	case "dismissCapture":
		a.Desk.DismissCapture(str(m["capture"]))
	case "dismissNeed":
		a.Desk.Dismiss(str(m["id"]))
		a.broadcastDesk()
	case "itemAdd":
		it := a.Desk.AddItem(desk.Item{Project: str(m["project"]), Kind: str(m["kind"]), Title: strings.TrimSpace(str(m["title"])), Source: "you"})
		logUser("add item", it.Title, it.Project)
	case "itemUpdate":
		a.Desk.UpdateItem(str(m["item"]), func(it *desk.Item) {
			if s := str(m["state"]); s == "open" || s == "done" {
				it.State = s
			}
			if t := strings.TrimSpace(str(m["title"])); t != "" {
				it.Title = t
			}
		})

	// ---- tasks (dispatch) ----
	case "taskNew":
		p, ok := a.Projects.Get(str(m["project"]))
		if !ok {
			fail(errors.New("no such project"))
			return
		}
		title := strings.TrimSpace(str(m["title"]))
		brief := str(m["brief"])
		if brief == "" {
			brief = "## Goal\n" + title + "\n\n## Done looks like\n- A short report: what you found, what you recommend, open questions.\n\n## Constraints\n- Read-only: don't change anything; report instead.\n"
		}
		tk := a.Desk.AddTask(desk.Task{Project: p.ID, Title: title, Brief: brief, From: str(m["from"])})
		a.openPane(p.Space, store.Pane{Kind: "task", Title: "Task: " + truncate(title, 40), Props: map[string]any{"task": tk.ID}}, "", you)
		logUser("draft task", title, "")
	case "taskUpdate":
		_, err := a.Desk.UpdateTask(str(m["task"]), func(t *desk.Task) error {
			if t.Status != "draft" {
				return errors.New("only drafts can be edited")
			}
			if s := strings.TrimSpace(str(m["title"])); s != "" {
				t.Title = s
			}
			if b, ok := m["brief"]; ok {
				t.Brief = str(b)
			}
			return nil
		})
		if err != nil {
			fail(err)
		}
	case "taskStart":
		if err := a.startTask(str(m["task"])); err != nil {
			fail(err)
		}
	case "taskStop":
		a.stopTask(str(m["task"]))
		logUser("stop task", str(m["task"]), "")
	case "taskDiscard":
		id := str(m["task"])
		a.stopTask(id)
		a.Desk.RemoveTask(id)
		for _, pn := range a.Store.Snapshot().Panes {
			if pn.Kind == "task" && str(pn.Props["task"]) == id {
				a.Store.ClosePane(pn.ID, you)
			}
		}
	case "taskReviewed":
		a.Desk.UpdateTask(str(m["task"]), func(t *desk.Task) error { t.Reviewed = true; return nil })

	// ---- add-ons and the wiki ----
	case "addonSet":
		on, _ := m["on"].(bool)
		if err := a.setAddon(str(m["project"]), str(m["addon"]), on); err != nil {
			fail(err)
			return
		}
		logUser(map[bool]string{true: "add-on on", false: "add-on off"}[on], str(m["addon"]), str(m["project"]))
	case "wikiResolve":
		accept, _ := m["accept"].(bool)
		p, err := a.Wiki.Resolve(str(m["project"]), str(m["proposal"]), accept)
		if err != nil {
			fail(err)
			return
		}
		logUser(map[bool]string{true: "accept wiki change", false: "reject wiki change"}[accept], p.Slug, p.Reason)
		a.broadcast(map[string]any{"t": "fileChanged", "path": "wiki/" + p.Project + "/" + p.Slug + ".md"})
		a.broadcastDesk()
	case "wikiProposal":
		p, err := a.Wiki.Proposal(str(m["project"]), str(m["proposal"]))
		if err != nil {
			fail(err)
			return
		}
		cur, _ := a.Wiki.Read(p.Project, p.Slug)
		reply(map[string]any{"t": "wikiProposal", "proposal": p, "current": cur})
	case "wikiBuild":
		pid := str(m["project"])
		if _, ok := a.Projects.Get(pid); !ok {
			fail(errors.New("no such project"))
			return
		}
		go a.appPrompt("p:"+pid, "Build this project's wiki (see 'The project wiki' in your instructions): read what the project already documents, write index.md first, then the handful of pages a newcomer needs most. Tell me what you wrote.")
	case "fileSave":
		// You editing a wiki page or a desk file (never your project folders from here).
		p := str(m["path"])
		if r := rootOf(p); r != "wiki" && r != "desk" {
			fail(errors.New("only wiki and desk files can be edited here"))
			return
		}
		abs, err := a.Roots.Resolve(p)
		if err != nil {
			fail(err)
			return
		}
		if r := rootOf(p); r == "wiki" {
			parts := strings.SplitN(strings.TrimPrefix(p, "wiki/"), "/", 2)
			if len(parts) != 2 {
				fail(errors.New("bad wiki path"))
				return
			}
			if _, err := a.Wiki.Write(parts[0], strings.TrimSuffix(parts[1], ".md"), str(m["content"])); err != nil {
				fail(err)
				return
			}
		} else if err := files.WriteAtomic(abs, []byte(str(m["content"]))); err != nil {
			fail(err)
			return
		}
		logUser("edit", p, "")
		a.broadcast(map[string]any{"t": "fileChanged", "path": p})
		a.broadcastDesk()
		reply(map[string]any{"t": "saved", "path": p})

	// ---- widgets ----
	case "widgetRemove":
		a.Desk.RemoveWidget(str(m["widget"]))

	// ---- layout (per project space) ----
	case "activate":
		a.Store.Activate(str(m["pane"]))
	case "focusGroup":
		a.Store.FocusGroup(space, str(m["group"]))
	case "close":
		p, err := a.Store.ClosePane(str(m["pane"]), you)
		if err != nil {
			fail(err)
			return
		}
		a.cleanupPane(p)
		logUser("close", p.Title, "")
	case "move":
		if err := a.Store.MovePane(str(m["pane"]), str(m["placement"]), you); err != nil {
			fail(err)
		}
	case "resize":
		var sizes []float64
		if arr, ok := m["sizes"].([]any); ok {
			for _, v := range arr {
				f, _ := v.(float64)
				sizes = append(sizes, f)
			}
		}
		a.Store.Resize(space, str(m["split"]), sizes)
	case "setTarget":
		a.Store.SetTarget(space, str(m["group"]))
	case "preset":
		if err := a.Store.Preset(space, str(m["name"]), you); err != nil {
			fail(err)
		}
	case "undo":
		label, err := a.Store.UndoAgent(space)
		if err != nil {
			fail(err)
			return
		}
		logUser("undo agent layout", label, "")
	case "open":
		if space == "" {
			fail(errors.New("open needs a project"))
			return
		}
		p := store.Pane{Kind: str(m["kind"]), Title: str(m["title"]), Props: map[string]any{}}
		if props, ok := m["props"].(map[string]any); ok {
			p.Props = props
		}
		if p.Title == "" {
			p.Title = defaultTitle(p)
		}
		if pth := str(p.Props["path"]); pth != "" && (p.Kind == "sheet" || p.Kind == "review") {
			if ex, ok := a.Store.FindPaneIn(space, p.Kind, "path", pth); ok {
				a.Store.Activate(ex.ID)
				return
			}
		}
		if p.Kind == "task" {
			if ex, ok := a.Store.FindPaneIn(space, "task", "task", str(p.Props["task"])); ok {
				a.Store.Activate(ex.ID)
				return
			}
		}
		np, err := a.openPane(space, p, str(m["placement"]), you)
		if err != nil {
			fail(err)
			return
		}
		logUser("open", np.Title, np.Kind)

	// ---- files ----
	case "filesList":
		ents, err := a.Roots.List(str(m["path"]))
		if err != nil {
			fail(err)
			return
		}
		reply(map[string]any{"t": "files", "path": str(m["path"]), "entries": ents})

	// ---- sheets ----
	case "sheetLoad":
		sh, err := a.Sheets.Load(str(m["path"]))
		if err != nil {
			fail(err)
			return
		}
		reply(map[string]any{"t": "sheet", "sheet": sh})
	case "sheetOps":
		var req struct {
			Path string      `json:"path"`
			Rev  string      `json:"rev"`
			Ops  []sheets.Op `json:"ops"`
		}
		json.Unmarshal(raw, &req)
		sh, err := a.Sheets.Apply(req.Path, req.Rev, req.Ops)
		if errors.Is(err, sheets.ErrStale) {
			reply(map[string]any{"t": "sheetStale", "sheet": sh})
			return
		}
		if err != nil {
			fail(err)
			return
		}
		a.broadcast(map[string]any{"t": "sheet", "sheet": sh, "by": "you"})
		logUser("edit sheet", req.Path, fmt.Sprintf("%d op(s)", len(req.Ops)))
	case "sheetNew":
		p := str(m["path"])
		if !strings.HasSuffix(p, ".csv") {
			p += ".csv"
		}
		if _, err := a.Sheets.Create(p, []string{"Item", "Notes"}, [][]string{{"", ""}}); err != nil {
			fail(err)
			return
		}
		np, err := a.openPane(space, store.Pane{Kind: "sheet", Title: path.Base(p), Props: map[string]any{"path": p}}, "", you)
		if err == nil {
			logUser("new sheet", np.Title, "")
		}
	case "sheetView":
		var req struct {
			Pane string      `json:"pane"`
			View sheets.View `json:"view"`
		}
		json.Unmarshal(raw, &req)
		a.Store.UpdatePane(req.Pane, you, func(p *store.Pane) error { p.Props["view"] = req.View; return nil })

	// ---- review ----
	case "reviewLoad":
		p := str(m["path"])
		content, err := a.Review.Content(p)
		if err != nil {
			fail(err)
			return
		}
		d, _ := a.Review.Get(p)
		reply(map[string]any{"t": "reviewDoc", "path": p, "content": content, "doc": d})
	case "reviewComment":
		var req struct {
			Path   string        `json:"path"`
			Anchor review.Anchor `json:"anchor"`
			Text   string        `json:"text"`
		}
		json.Unmarshal(raw, &req)
		d, th, err := a.Review.AddThread(req.Path, req.Anchor, you, req.Text)
		if err != nil {
			fail(err)
			return
		}
		a.broadcast(map[string]any{"t": "review", "path": req.Path, "doc": d})
		logUser("comment", req.Path, th.ID)
		if b, _ := m["ask"].(bool); b {
			a.askAboutDoc(space, req.Path, fmt.Sprintf("I left a comment (thread %s) on %s, on the passage %q: %s\nPlease reply in the thread (review_reply) and, if an edit is warranted, propose it with review_suggest.", th.ID, req.Path, truncate(req.Anchor.Exact, 300), req.Text))
		}
	case "reviewReply":
		p := str(m["path"])
		d, err := a.Review.Reply(p, str(m["thread"]), you, str(m["text"]))
		if err != nil {
			fail(err)
			return
		}
		a.broadcast(map[string]any{"t": "review", "path": p, "doc": d})
		if b, _ := m["ask"].(bool); b {
			a.askAboutDoc(space, p, fmt.Sprintf("I replied in thread %s on %s: %s\nPlease respond in the thread.", str(m["thread"]), p, str(m["text"])))
		}
	case "reviewStatus":
		p := str(m["path"])
		d, err := a.Review.SetStatus(p, str(m["thread"]), str(m["status"]))
		if err != nil {
			fail(err)
			return
		}
		a.broadcast(map[string]any{"t": "review", "path": p, "doc": d})
	case "suggestion":
		p := str(m["path"])
		accept, _ := m["accept"].(bool)
		d, err := a.Review.ResolveSuggestion(p, str(m["thread"]), str(m["sugg"]), accept)
		if err != nil {
			fail(err)
			return
		}
		a.broadcast(map[string]any{"t": "review", "path": p, "doc": d})
		logUser(map[bool]string{true: "accept suggestion", false: "reject suggestion"}[accept], p, str(m["sugg"]))

	// ---- secure ----
	case "secretSubmit":
		h, label, agentID := str(m["handle"]), "", str(m["agent"])
		if pid := str(m["pane"]); pid != "" {
			pane, ok := a.Store.Pane(pid)
			if !ok || pane.Kind != "secret-input" {
				fail(errors.New("not a secret entry pane"))
				return
			}
			h, label = str(pane.Props["handle"]), str(pane.Props["label"])
			if p, ok := a.projectBySpace(pane.Space); ok {
				agentID = "p:" + p.ID
			}
			a.Store.UpdatePane(pane.ID, you, func(p *store.Pane) error { p.Props["done"] = true; return nil })
		} else if hi, ok := a.Vault.HandleInfo(h); ok {
			label = hi.Label
		}
		if err := a.Vault.Fill(h, str(m["value"])); err != nil {
			fail(err)
			return
		}
		a.Log.Add(actionlog.Entry{Actor: you, Action: "entered secret", Target: label, Detail: "handle " + h})
		if agentID != "" {
			go a.appPrompt(agentID, fmt.Sprintf("The user entered the secret for %q (handle %s).", label, h))
		}
	case "secureDismiss":
		a.Vault.Forget(str(m["id"]))

	// ---- terminal ----
	case "termAttach":
		pane, ok := a.Store.Pane(str(m["pane"]))
		if !ok || pane.Kind != "terminal" {
			return
		}
		tid := str(pane.Props["term"])
		tm := a.Terms.Get(tid)
		if tm == nil {
			dir := a.cfg.DataDir
			if p, ok := a.projectBySpace(pane.Space); ok {
				dir = p.Path
			}
			nt, err := a.Terms.Start(dir)
			if err != nil {
				fail(err)
				return
			}
			tm = nt
			a.Store.UpdatePane(pane.ID, you, func(p *store.Pane) error { p.Props["term"] = nt.ID; return nil })
		}
		reply(map[string]any{"t": "termReady", "pane": pane.ID, "term": tm.ID, "scrollback": string(tm.Scrollback())})
	case "termInput":
		if tm := a.Terms.Get(str(m["term"])); tm != nil {
			tm.Input([]byte(str(m["data"])))
		}
	case "termResize":
		if tm := a.Terms.Get(str(m["term"])); tm != nil {
			tm.Resize(iArg(m, "rows", 0), iArg(m, "cols", 0))
		}
	case "termProposal":
		pane, ok := a.Store.Pane(str(m["pane"]))
		if !ok {
			return
		}
		prop, _ := pane.Props["proposal"].(map[string]any)
		cmd := str(prop["command"])
		action := str(m["action"])
		a.Store.UpdatePane(pane.ID, you, func(p *store.Pane) error { delete(p.Props, "proposal"); return nil })
		if tm := a.Terms.Get(str(pane.Props["term"])); tm != nil && cmd != "" {
			switch action {
			case "run":
				tm.Input([]byte(cmd + "\r"))
			case "edit":
				tm.Input([]byte(cmd))
			}
		}
		logUser("command proposal: "+action, pane.Title, cmd)

	// ---- plugins ----
	case "bridge":
		go a.handleBridge(c, m)
	case "permAnswer":
		a.pendMu.Lock()
		ch := a.pending[str(m["prompt"])]
		delete(a.pending, str(m["prompt"]))
		a.pendMu.Unlock()
		if ch != nil {
			g, _ := m["grant"].(bool)
			ch <- g
		}
	case "pluginLoad":
		pl, ok := a.Plugins.Get(str(m["plugin"]))
		if !ok {
			fail(errors.New("no such plugin"))
			return
		}
		reply(map[string]any{"t": "pluginHtml", "plugin": pl.ID, "html": pl.HTML})
	case "pluginKeep":
		keep, _ := m["keep"].(bool)
		if err := a.Plugins.Keep(str(m["plugin"]), keep); err != nil {
			fail(err)
			return
		}
		logUser(map[bool]string{true: "keep plugin", false: "unkeep plugin"}[keep], str(m["plugin"]), "")
		a.broadcastState()
	case "pluginDelete":
		id := str(m["plugin"])
		for pid, p := range a.Store.Snapshot().Panes {
			if p.Kind == "plugin" && str(p.Props["plugin"]) == id {
				a.Store.ClosePane(pid, you)
			}
		}
		a.Desk.RemoveWidget(id)
		a.Plugins.Delete(id)
		logUser("delete plugin", id, "")
		a.broadcastState()
	case "pluginRevoke":
		a.Plugins.SetGrant(str(m["plugin"]), str(m["perm"]), "")
		logUser("revoke permission", str(m["plugin"]), str(m["perm"]))
		a.broadcastState()

	// ---- misc ----
	case "openExternal":
		u := str(m["url"])
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			openExternal(u)
			logUser("open in your browser", u, "")
		}
	}
}

// askAboutDoc routes a review request to the project agent that owns the doc.
func (a *App) askAboutDoc(space, docPath, text string) {
	p, ok := a.projectBySpace(space)
	if !ok {
		p, ok = a.projectOfPath(docPath)
	}
	if !ok {
		a.toast("No project agent for that document", "warn")
		return
	}
	go a.Chat("p:"+p.ID, text, "", "")
}

// appPrompt sends a message on the user's behalf that the app generated
// (shown in the transcript as coming from the app, not typed by you).
func (a *App) appPrompt(agentID, text string) {
	r, err := a.run(agentID)
	if err != nil {
		return
	}
	ctx := ""
	switch r.Kind {
	case KindHome:
		ctx = a.homeContext("")
	case KindProject:
		ctx = a.projectContext(r.Project, "")
	}
	r.sayAs("app", text, ctx)
}

func defaultTitle(p store.Pane) string {
	switch p.Kind {
	case "terminal":
		return "Terminal"
	case "files":
		return "Files"
	case "log":
		return "Action log"
	case "markdown":
		return "Note"
	}
	if s := str(p.Props["path"]); s != "" {
		return path.Base(s)
	}
	return p.Kind
}

func openExternal(u string) {
	var cmds [][]string
	switch runtime.GOOS {
	case "darwin":
		cmds = [][]string{{"open", u}}
	default:
		cmds = [][]string{{"wslview", u}, {"xdg-open", u}}
	}
	for _, c := range cmds {
		if _, err := exec.LookPath(c[0]); err == nil {
			exec.Command(c[0], c[1:]...).Start()
			return
		}
	}
}

// ---- plugin bridge ----

// handleBridge serves window.nexus calls from a plugin iframe (a pane, or a
// dashboard widget "widget:<id>"). Policy: the plugin must have declared the
// permission, and you must have granted it (asked once).
func (a *App) handleBridge(c *client, m map[string]any) {
	paneID := str(m["pane"])
	reqID := str(m["req"])
	method := str(m["method"])
	params, _ := m["params"].(map[string]any)
	if params == nil {
		params = map[string]any{}
	}
	reply := func(result any, err error) {
		out := map[string]any{"t": "bridgeReply", "pane": paneID, "req": reqID}
		if err != nil {
			out["error"] = err.Error()
		} else {
			out["result"] = result
		}
		a.sendTo(c, out)
	}
	var pluginID, space string
	if strings.HasPrefix(paneID, "widget:") {
		w, ok := a.Desk.Widget(strings.TrimPrefix(paneID, "widget:"))
		if !ok {
			reply(nil, errors.New("no such widget"))
			return
		}
		pluginID = w.Plugin
	} else {
		pane, ok := a.Store.Pane(paneID)
		if !ok || pane.Kind != "plugin" {
			reply(nil, errors.New("not a plugin pane"))
			return
		}
		pluginID, space = str(pane.Props["plugin"]), pane.Space
	}
	pl, ok := a.Plugins.Get(pluginID)
	if !ok {
		reply(nil, errors.New("plugin not found"))
		return
	}
	actor := "plugin:" + pl.ID
	logDeny := func(perm, why string) {
		a.Log.Add(actionlog.Entry{Actor: actor, Action: "blocked: " + perm, Target: pl.Name, Detail: why, Denied: true, Space: space})
	}
	if method == "csp_violation" {
		logDeny("network ("+str(params["directive"])+")", "CSP blocked "+str(params["blocked"]))
		reply(true, nil)
		return
	}
	var perm string
	switch method {
	case "fetch":
		perm = "connect:" + str(params["connection"])
	case "call":
		perm = "tool:" + str(params["tool"])
	case "setContext":
		perm = "context"
	default:
		logDeny(method, "unknown bridge method")
		reply(nil, fmt.Errorf("unknown bridge method %q", method))
		return
	}
	declared, grant := pl.Check(perm)
	if !declared {
		logDeny(perm, "not declared in the plugin's manifest")
		reply(nil, fmt.Errorf("permission %s was not declared by this plugin", perm))
		return
	}
	if grant == "" {
		g := a.askPermission(pl.ID, pl.Name, perm)
		val := map[bool]string{true: "granted", false: "denied"}[g]
		a.Plugins.SetGrant(pl.ID, perm, val)
		a.Log.Add(actionlog.Entry{Actor: you, Action: "permission " + val, Target: pl.Name, Detail: perm})
		a.broadcastState()
		grant = val
	}
	if grant != "granted" {
		logDeny(perm, "you denied this permission")
		reply(nil, fmt.Errorf("permission %s denied by the user", perm))
		return
	}
	switch method {
	case "fetch":
		res, err := a.Broker.Call(str(params["connection"]), str(params["path"]), "")
		reply(res, err)
	case "call":
		tool := str(params["tool"])
		args, _ := params["args"].(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		t, ok := a.toolByName(tool)
		if !ok {
			reply(nil, fmt.Errorf("no tool %s", tool))
			return
		}
		// Plugins act with the reach of where they live: a project pane
		// reads its project; a dashboard widget reads like the home agent.
		ctx := &Ctx{Kind: KindHome, Actor: actor}
		if p, ok := a.projectBySpace(space); ok {
			ctx = &Ctx{Kind: KindProject, Project: p, Space: space, Actor: actor}
		}
		out, err := t.run(a, ctx, args)
		a.Log.Add(actionlog.Entry{Actor: actor, Action: tool, Target: logTarget(args), Space: space})
		reply(out, err)
	case "setContext":
		a.pendMu.Lock()
		a.pluginCtx[paneID] = truncate(str(params["text"]), 4000)
		a.pendMu.Unlock()
		reply(true, nil)
	}
}

func (a *App) askPermission(pluginID, name, perm string) bool {
	b := make([]byte, 4)
	rand.Read(b)
	id := hex.EncodeToString(b)
	ch := make(chan bool, 1)
	a.pendMu.Lock()
	a.pending[id] = ch
	a.pendMu.Unlock()
	a.broadcast(map[string]any{"t": "permPrompt", "prompt": id, "plugin": pluginID, "name": name, "perm": perm, "explain": explainPerm(perm)})
	select {
	case g := <-ch:
		a.broadcast(map[string]any{"t": "permDone", "prompt": id})
		return g
	case <-time.After(5 * time.Minute):
		a.pendMu.Lock()
		delete(a.pending, id)
		a.pendMu.Unlock()
		a.broadcast(map[string]any{"t": "permDone", "prompt": id})
		return false
	}
}

func explainPerm(perm string) string {
	switch {
	case strings.HasPrefix(perm, "connect:"):
		return "Call the " + perm[8:] + " connection through the broker (the plugin never sees credentials)."
	case perm == "tool:projects_summary":
		return "Read a summary of your projects (names, counts, recent activity)."
	case strings.HasPrefix(perm, "tool:"):
		return "Use the app tool " + perm[5:] + "."
	case perm == "context":
		return "Publish text that the agent can see while this pane is open."
	}
	return perm
}
