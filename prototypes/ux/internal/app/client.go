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

	"github.com/pvjammer/nexus/prototypes/ux/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/ux/internal/review"
	"github.com/pvjammer/nexus/prototypes/ux/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/ux/internal/store"
)

const you = store.ActorUser

// handleClient handles one message from the shell (always the user).
func (a *App) handleClient(c *client, m map[string]any, raw []byte) {
	t := str(m["t"])
	reply := func(v map[string]any) {
		if rid := str(m["rid"]); rid != "" {
			v["rid"] = rid
		}
		a.sendTo(c, v)
	}
	fail := func(err error) {
		reply(map[string]any{"t": "error", "error": err.Error()})
	}
	logUser := func(action, target, detail string) {
		a.Log.Add(actionlog.Entry{Actor: you, Action: action, Target: target, Detail: detail, Space: a.Store.ActiveSpace()})
	}
	switch t {
	// ---- chat ----
	case "chat":
		go a.UserChat(str(m["text"]), str(m["selection"]))
	case "abort":
		if a.Agent != nil {
			a.Agent.Abort()
		}

	// ---- layout ----
	case "activate":
		a.Store.Activate(str(m["pane"]))
	case "focusGroup":
		a.Store.FocusGroup("", str(m["group"]))
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
		for _, v := range m["sizes"].([]any) {
			f, _ := v.(float64)
			sizes = append(sizes, f)
		}
		a.Store.Resize("", str(m["split"]), sizes)
	case "pin":
		pinned, _ := m["pinned"].(bool)
		p, err := a.Store.UpdatePane(str(m["pane"]), you, func(p *store.Pane) error { p.Pinned = pinned; return nil })
		if err == nil {
			logUser(map[bool]string{true: "pin", false: "unpin"}[pinned], p.Title, "")
		}
	case "setVis":
		vis := str(m["vis"])
		if vis != store.VisFull && vis != store.VisTitle && vis != store.VisNone {
			fail(errors.New("bad visibility"))
			return
		}
		p, err := a.Store.UpdatePane(str(m["pane"]), you, func(p *store.Pane) error {
			if p.VisLocked && vis != store.VisNone {
				if b, _ := m["confirm"].(bool); !b {
					return errors.New("secure pane: confirm required")
				}
			}
			p.Visibility = vis
			return nil
		})
		if err != nil {
			fail(err)
			return
		}
		detail := ""
		if p.VisLocked {
			detail = "secure pane"
		}
		logUser("visibility → "+vis, p.Title, detail)
	case "setTarget":
		a.Store.SetTarget("", str(m["group"]))
	case "preset":
		if err := a.Store.Preset("", str(m["name"]), you); err != nil {
			fail(err)
		}
	case "undo":
		label, err := a.Store.UndoAgent("")
		if err != nil {
			fail(err)
			return
		}
		logUser("undo agent layout", label, "")
	case "spaceCreate":
		sp := a.Store.CreateSpace(str(m["name"]))
		logUser("new space", sp.Name, "")
	case "spaceSwitch":
		a.Store.SwitchSpace(str(m["space"]))
	case "open":
		p := store.Pane{Kind: str(m["kind"]), Title: str(m["title"]), Props: map[string]any{}}
		if props, ok := m["props"].(map[string]any); ok {
			p.Props = props
		}
		if p.Title == "" {
			p.Title = defaultTitle(p)
		}
		if path := str(p.Props["path"]); path != "" && (p.Kind == "sheet" || p.Kind == "review") {
			if ex, ok := a.Store.FindPane(p.Kind, "path", path); ok {
				a.Store.Activate(ex.ID)
				return
			}
		}
		np, err := a.openPane(p, str(m["placement"]), you)
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
		np, err := a.openPane(store.Pane{Kind: "sheet", Title: path.Base(p), Props: map[string]any{"path": p}}, "", you)
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
			go a.UserChat(fmt.Sprintf("I left a comment (thread %s) on %s, on the passage %q: %s\nPlease reply in the thread (review_reply) and, if an edit is warranted, propose it with review_suggest.", th.ID, req.Path, truncate(req.Anchor.Exact, 300), req.Text), "")
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
			go a.UserChat(fmt.Sprintf("I replied in thread %s on %s: %s\nPlease respond in the thread.", str(m["thread"]), p, str(m["text"])), "")
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
		pane, ok := a.Store.Pane(str(m["pane"]))
		if !ok || pane.Kind != "secret-input" {
			fail(errors.New("not a secret entry pane"))
			return
		}
		h := str(pane.Props["handle"])
		if err := a.Vault.Fill(h, str(m["value"])); err != nil {
			fail(err)
			return
		}
		a.Store.UpdatePane(pane.ID, you, func(p *store.Pane) error { p.Props["done"] = true; return nil })
		a.Log.Add(actionlog.Entry{Actor: you, Action: "entered secret", Target: str(pane.Props["label"]), Detail: "handle " + h})
		go a.UserChat(fmt.Sprintf("I entered the secret for %q (handle %s).", str(pane.Props["label"]), h), "")

	// ---- terminal ----
	case "termAttach":
		pane, ok := a.Store.Pane(str(m["pane"]))
		if !ok || pane.Kind != "terminal" {
			return
		}
		tid := str(pane.Props["term"])
		tm := a.Terms.Get(tid)
		if tm == nil {
			nt, err := a.Terms.Start(a.cfg.TermDir)
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
		// run | dismiss | edit (edit = type it without Enter, you finish it)
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
		for _, p := range a.Store.Panes("") {
			if p.Kind == "plugin" && str(p.Props["plugin"]) == id {
				a.Store.ClosePane(p.ID, you)
			}
		}
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

// handleBridge serves window.nexus calls from a plugin iframe. Policy:
// 1) the plugin must have declared the permission (else refused, logged);
// 2) you must have granted it (asked once; denial is remembered).
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
	pane, ok := a.Store.Pane(paneID)
	if !ok || pane.Kind != "plugin" {
		reply(nil, errors.New("not a plugin pane"))
		return
	}
	pl, ok := a.Plugins.Get(str(pane.Props["plugin"]))
	if !ok {
		reply(nil, errors.New("plugin not found"))
		return
	}
	actor := "plugin:" + pl.ID
	logDeny := func(perm, why string) {
		a.Log.Add(actionlog.Entry{Actor: actor, Action: "blocked: " + perm, Target: pl.Name, Detail: why, Denied: true})
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
		t, ok := a.toolByName(tool)
		if !ok {
			reply(nil, fmt.Errorf("no tool %s", tool))
			return
		}
		out, err := t.run(a, args)
		a.Log.Add(actionlog.Entry{Actor: actor, Action: tool, Target: logTarget(args)})
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
	case strings.HasPrefix(perm, "tool:"):
		return "Use the app tool " + perm[5:] + "."
	case perm == "context":
		return "Publish text that the agent can see while this pane is open."
	}
	return perm
}
