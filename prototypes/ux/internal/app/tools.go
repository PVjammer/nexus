package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/pvjammer/nexus/prototypes/ux/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/ux/internal/broker"
	"github.com/pvjammer/nexus/prototypes/ux/internal/plugins"
	"github.com/pvjammer/nexus/prototypes/ux/internal/review"
	"github.com/pvjammer/nexus/prototypes/ux/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/ux/internal/store"
)

// Tool is one capability the agent can call. Definitions live in the core so
// any agent loop (pi today, our own later) gets the same tools and policy.
type Tool struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	run         func(a *App, args map[string]any) (string, error)
}

// SecureAck is the fixed protocol string a secure tool returns to the model.
const SecureAck = "DATA_DELIVERED_OUT_OF_BAND: the value was shown directly to the user in pane %s. You did not receive it and cannot see, repeat, summarize or guess it. Tell the user it is in the pane."

func obj(required []string, props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}
func sprop(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func iprop(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func bprop(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }
func eprop(desc string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "description": desc, "enum": vals}
}
func aprop(desc string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}

const placementDesc = "Where to put it: 'target' (your own group, default; never steals the user's focus), 'right' or 'down' (new split beside the user's focused group), a group id, or '<groupId>:left|right|top|bottom' for a new split at that edge."

var paneKinds = []string{"markdown", "html", "sheet", "review", "terminal", "browser", "plugin", "files", "log"}

func (a *App) tools() []Tool {
	return []Tool{
		{Name: "layout_get", Label: "Layout",
			Description: "See the current space's layout: groups (with ids), their tabs, which group the user is focused on, and which group is your target. Content of panes is not included.",
			Parameters:  obj(nil, map[string]any{}), run: toolLayoutGet},
		{Name: "pane_open", Label: "Open pane",
			Description: "Open a pane. Kinds: markdown/html (you supply content: notes, tables, diagrams as inline SVG; html is sandboxed with no scripts or network), sheet (a CSV file; use path), review (a .md/.html/.txt document; path), terminal (the user's shell), browser (url, for following along), plugin (plugin_id), files (file browser at path), log (action log). Opens in your target group in the background with a badge.",
			Parameters: obj([]string{"kind"}, map[string]any{
				"kind": eprop("pane kind", paneKinds...), "title": sprop("tab title"),
				"path":    sprop("file path like 'workspace/pantry.csv' (sheet, review, files)"),
				"content": sprop("markdown or html body (markdown, html)"), "url": sprop("browser url"),
				"plugin_id": sprop("plugin id (plugin)"), "placement": sprop(placementDesc),
			}), run: toolPaneOpen},
		{Name: "pane_update", Label: "Update pane",
			Description: "Replace the title and/or content of a markdown or html pane you opened.",
			Parameters: obj([]string{"pane_id"}, map[string]any{
				"pane_id": sprop("pane id"), "title": sprop("new title"), "content": sprop("new content"),
			}), run: toolPaneUpdate},
		{Name: "pane_move", Label: "Move pane",
			Description: "Move a pane to another group or a new split. Pinned panes can't be moved without asking the user.",
			Parameters:  obj([]string{"pane_id", "placement"}, map[string]any{"pane_id": sprop("pane id"), "placement": sprop(placementDesc)}), run: toolPaneMove},
		{Name: "pane_close", Label: "Close pane",
			Description: "Close a pane. Pinned panes can't be closed without asking the user first.",
			Parameters:  obj([]string{"pane_id"}, map[string]any{"pane_id": sprop("pane id")}), run: toolPaneClose},
		{Name: "layout_preset", Label: "Arrange",
			Description: "Rearrange all panes in the space: single (one tab group), side-by-side (two groups), columns (one per pane), grid (2 rows). The user can undo it in one click.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": eprop("preset", "single", "side-by-side", "columns", "grid")}), run: toolPreset},
		{Name: "space_create", Label: "New space",
			Description: "Create a new space (a context like a project, trip or the homelab) without switching the user to it.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("space name")}), run: toolSpaceCreate},
		{Name: "files_list", Label: "List files",
			Description: "List files under a path. Path '' lists the roots.",
			Parameters:  obj(nil, map[string]any{"path": sprop("directory path, e.g. 'workspace'")}), run: toolFilesList},
		{Name: "sheet_read", Label: "Read sheet",
			Description: "Read a CSV sheet: headers and rows (0-based row numbers). Refused if the user hid it from you.",
			Parameters:  obj([]string{"path"}, map[string]any{"path": sprop("csv path"), "max_rows": iprop("default 200")}), run: toolSheetRead},
		{Name: "sheet_create", Label: "New sheet",
			Description: "Create a new CSV sheet and open it.",
			Parameters: obj([]string{"path", "headers"}, map[string]any{
				"path": sprop("e.g. 'workspace/trip-packing.csv'"), "headers": aprop("column names", sprop("")),
				"rows": aprop("initial rows", aprop("", sprop(""))), "placement": sprop(placementDesc),
			}), run: toolSheetCreate},
		{Name: "sheet_edit", Label: "Edit sheet",
			Description: "Apply edits to a CSV sheet atomically. ops: {op:'set',row,col,value} | {op:'addRow',cells:[...],at?} | {op:'deleteRow',row} | {op:'addCol',value:<header>} | {op:'renameCol',col,value} | {op:'deleteCol',col}. Rows/cols are 0-based; read first. Delete rows from the highest index down.",
			Parameters: obj([]string{"path", "ops"}, map[string]any{
				"path": sprop("csv path"),
				"ops": aprop("edit operations", map[string]any{"type": "object", "properties": map[string]any{
					"op":  eprop("", "set", "addRow", "deleteRow", "addCol", "renameCol", "deleteCol"),
					"row": iprop(""), "col": iprop(""), "value": sprop(""), "cells": aprop("", sprop("")), "at": iprop(""),
				}, "required": []string{"op"}}),
			}), run: toolSheetEdit},
		{Name: "sheet_view", Label: "Sort/filter",
			Description: "Set how a sheet pane is displayed (sort and filter). This changes the view only, never the file.",
			Parameters: obj([]string{"pane_id"}, map[string]any{
				"pane_id": sprop("sheet pane id"), "sort_by": sprop("column name to sort by (preferred over sort_col)"),
				"sort_col": iprop("column index, -1 for none"), "desc": bprop("sort descending"),
				"filter": sprop("show rows containing this text ('' clears)"), "filter_col": iprop("restrict filter to a column, -1 any"),
			}), run: toolSheetView},
		{Name: "review_read", Label: "Read document",
			Description: "Read a document and its comment threads (ids, anchors, comments, suggestions).",
			Parameters:  obj([]string{"path"}, map[string]any{"path": sprop("document path")}), run: toolReviewRead},
		{Name: "review_comment", Label: "Comment",
			Description: "Start a comment thread on a document, anchored on an exact quote from it (use this to walk the user through a document or ask a question about a passage).",
			Parameters: obj([]string{"path", "quote", "text"}, map[string]any{
				"path": sprop("document path"), "quote": sprop("exact text from the rendered document, a few words to a sentence"), "text": sprop("your comment"),
			}), run: toolReviewComment},
		{Name: "review_reply", Label: "Reply",
			Description: "Reply in a comment thread.",
			Parameters: obj([]string{"path", "thread_id", "text"}, map[string]any{
				"path": sprop("document path"), "thread_id": sprop("thread id"), "text": sprop("reply"),
			}), run: toolReviewReply},
		{Name: "review_suggest", Label: "Suggest edit",
			Description: "Propose an edit to a document's source. The user accepts or rejects it; you never edit reviewed documents directly. find must occur exactly once in the source.",
			Parameters: obj([]string{"path", "find", "replace"}, map[string]any{
				"path": sprop("document path"), "thread_id": sprop("attach to this thread (optional)"),
				"find": sprop("exact source text to replace"), "replace": sprop("replacement"), "note": sprop("why"),
			}), run: toolReviewSuggest},
		{Name: "vault_list", Label: "Vault items",
			Description: "List the names and kinds of items in the user's vault. Values are never returned.",
			Parameters:  obj(nil, map[string]any{}), run: toolVaultList},
		{Name: "vault_reveal", Label: "Secure: show to user",
			Description: "SECURE TOOL. Show a vault item's value to the user. The value goes straight to a secure pane on the user's screen and is NEVER returned to you. You get back a fixed DATA_DELIVERED_OUT_OF_BAND acknowledgement; when you see it, tell the user the value is in the pane. Never claim to know the value.",
			Parameters: obj([]string{"name"}, map[string]any{
				"name": sprop("vault item name (from vault_list)"), "reason": sprop("one line shown to the user"),
			}), run: toolVaultReveal},
		{Name: "secret_request", Label: "Secure: ask for a secret",
			Description: "Ask the user to enter a secret (token, password) in a secure entry pane. Returns a handle like 'secret:ab12cd34' immediately; the value is never shown to you. Pass the handle to broker_call as auth_handle once the user has entered it.",
			Parameters: obj([]string{"label"}, map[string]any{
				"label": sprop("what you need, e.g. 'Homelab API token'"), "purpose": sprop("what you'll use it for"),
			}), run: toolSecretRequest},
		{Name: "broker_list", Label: "Connections",
			Description: "List the connections the credential broker can call for you.",
			Parameters:  obj(nil, map[string]any{}), run: toolBrokerList},
		{Name: "broker_call", Label: "Call connection",
			Description: "Call an allowlisted path on a broker connection. The broker attaches credentials; you never hold them. Optionally pass auth_handle (from secret_request) to use a secret the user entered.",
			Parameters: obj([]string{"connection", "path"}, map[string]any{
				"connection": sprop("connection name"), "path": sprop("allowlisted path, e.g. /disks"), "auth_handle": sprop("secret handle"),
			}), run: toolBrokerCall},
		{Name: "term_propose", Label: "Propose command",
			Description: "Propose a shell command in one of the user's terminals. It appears as a suggestion they can Run, Edit or Dismiss; you can't run it yourself. Opens a terminal if none is given.",
			Parameters: obj([]string{"command"}, map[string]any{
				"pane_id": sprop("terminal pane id (optional)"), "command": sprop("the command"), "why": sprop("one line explanation"),
			}), run: toolTermPropose},
		{Name: "term_read", Label: "Read terminal",
			Description: "Read the last lines of a terminal's output (refused if the user hid that terminal from you).",
			Parameters:  obj([]string{"pane_id"}, map[string]any{"pane_id": sprop("terminal pane id"), "lines": iprop("default 60")}), run: toolTermRead},
		{Name: "plugin_write", Label: "Write plugin",
			Description: "Create or update (hot-reload) a plugin: a pane made of one self-contained HTML file. Read the plugin guide in your instructions first. It runs sandboxed with no network; it reaches data only through window.nexus and only for permissions it declares and the user grants. Opens it in your target group.",
			Parameters: obj([]string{"id", "name", "html"}, map[string]any{
				"id": sprop("lowercase-dashed id; reuse it to update"), "name": sprop("display name"), "description": sprop("one line"),
				"permissions": aprop("e.g. ['connect:homelab-mock','tool:sheet_read','context']", sprop("")),
				"html":        sprop("complete HTML document with inline CSS and JS"), "placement": sprop(placementDesc),
			}), run: toolPluginWrite},
		{Name: "plugin_list", Label: "Plugins",
			Description: "List plugins with their permissions and grant status.",
			Parameters:  obj(nil, map[string]any{}), run: toolPluginList},
	}
}

func (a *App) toolByName(name string) (Tool, bool) {
	for _, t := range a.tools() {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// CallTool runs a tool as the agent and logs it.
func (a *App) CallTool(name string, args map[string]any) (string, error) {
	t, ok := a.toolByName(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	out, err := t.run(a, args)
	e := actionlog.Entry{Actor: store.ActorAgent, Action: name, Target: logTarget(args), Space: a.Store.ActiveSpace()}
	if err != nil {
		e.Detail = err.Error()
		e.Denied = isDenial(err)
	}
	if name != "layout_get" && name != "plugin_list" && name != "vault_list" && name != "broker_list" {
		a.Log.Add(e)
	}
	return out, err
}

type denial struct{ msg string }

func (d denial) Error() string              { return d.msg }
func deny(format string, args ...any) error { return denial{fmt.Sprintf(format, args...)} }
func isDenial(err error) bool {
	var d denial
	return errors.As(err, &d) || errors.Is(err, store.ErrPinned)
}

// logTarget picks a short, non-sensitive target for the action log.
func logTarget(args map[string]any) string {
	for _, k := range []string{"pane_id", "path", "name", "id", "connection", "label", "kind"} {
		if v := str(args[k]); v != "" {
			return v
		}
	}
	return ""
}

func (a *App) serveTools(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Nexus-Token") != a.cfg.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.tools())
}

func (a *App) serveCall(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Nexus-Token") != a.cfg.Token || r.Method != http.MethodPost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out, err := a.CallTool(req.Name, req.Args)
	resp := map[string]any{"text": out}
	if err != nil {
		resp = map[string]any{"text": "error: " + err.Error(), "isError": true}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// ---- arg helpers ----

func sArg(m map[string]any, k string) string { return strings.TrimSpace(str(m[k])) }
func iArg(m map[string]any, k string, def int) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		var n int
		if _, err := fmt.Sscan(v, &n); err == nil {
			return n
		}
	}
	return def
}
func bArg(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }
func strs(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, x := range arr {
		out = append(out, str(x))
	}
	return out
}

func jsonOut(v any) string { return broker.JSON(v) }

// ---- visibility policy ----

// canRead refuses when the user has hidden a pane showing this path.
func (a *App) canReadPath(p string) error {
	for _, pane := range a.Store.Snapshot().Panes {
		if str(pane.Props["path"]) == p && pane.Visibility != store.VisFull {
			return deny("the user has hidden %s from you (pane visibility: %s). Ask them to share it if you need it.", p, pane.Visibility)
		}
	}
	return nil
}

func (a *App) agentPane(id string) (store.Pane, error) {
	p, ok := a.Store.Pane(id)
	if !ok || p.Visibility == store.VisNone {
		return store.Pane{}, fmt.Errorf("no pane %s", id)
	}
	return p, nil
}

// ---- layout tools ----

func toolLayoutGet(a *App, _ map[string]any) (string, error) {
	sp, err := a.Store.Layout("")
	if err != nil {
		return "", err
	}
	snap := a.Store.Snapshot()
	var desc func(n *store.Node, depth int) string
	desc = func(n *store.Node, depth int) string {
		ind := strings.Repeat("  ", depth)
		if n.Kind == "split" {
			dir := "side by side"
			if n.Dir == "col" {
				dir = "stacked"
			}
			s := fmt.Sprintf("%ssplit %s (%s)\n", ind, n.ID, dir)
			for _, c := range n.Children {
				s += desc(c, depth+1)
			}
			return s
		}
		tags := []string{}
		if n.ID == sp.Focused {
			tags = append(tags, "USER FOCUSED")
		}
		if n.ID == sp.Target {
			tags = append(tags, "YOUR TARGET")
		}
		s := fmt.Sprintf("%sgroup %s %v\n", ind, n.ID, tags)
		for _, t := range n.Tabs {
			p := snap.Panes[t]
			if p == nil || p.Visibility == store.VisNone {
				continue
			}
			act := ""
			if t == n.Active {
				act = " (active)"
			}
			pin := ""
			if p.Pinned {
				pin = " PINNED"
			}
			s += fmt.Sprintf("%s  - %s [%s] %q opened-by:%s%s%s\n", ind, p.ID, p.Kind, p.Title, p.Opener, pin, act)
		}
		return s
	}
	var spaces []string
	for _, s := range snap.Spaces {
		spaces = append(spaces, fmt.Sprintf("%s=%q", s.ID, s.Name))
	}
	return fmt.Sprintf("space %s %q (all spaces: %s)\n%s", sp.ID, sp.Name, strings.Join(spaces, ", "), desc(sp.Root, 0)), nil
}

func (a *App) openPane(p store.Pane, placement, actor string) (store.Pane, error) {
	np, err := a.Store.OpenPane("", p, placement, actor)
	if err != nil {
		return np, err
	}
	if path := str(np.Props["path"]); path != "" {
		a.watchFile(path)
	}
	return np, nil
}

func toolPaneOpen(a *App, m map[string]any) (string, error) {
	kind := sArg(m, "kind")
	title := sArg(m, "title")
	p := store.Pane{Kind: kind, Props: map[string]any{}}
	switch kind {
	case "markdown", "html":
		p.Props["content"] = str(m["content"])
		if title == "" {
			title = "Note"
		}
	case "sheet", "review":
		path := sArg(m, "path")
		if path == "" {
			return "", errors.New("path is required")
		}
		if err := a.canReadPath(path); err != nil {
			return "", err
		}
		abs, err := a.Roots.Resolve(path)
		if err != nil {
			return "", err
		}
		_ = abs
		if kind == "sheet" {
			if _, err := a.Sheets.Load(path); err != nil {
				return "", err
			}
		} else if _, err := a.Review.Content(path); err != nil {
			return "", err
		}
		if existing, ok := a.Store.FindPane(kind, "path", path); ok {
			return fmt.Sprintf("already open as %s", existing.ID), nil
		}
		p.Props["path"] = path
		if title == "" {
			title = path[strings.LastIndex(path, "/")+1:]
		}
	case "terminal":
		if title == "" {
			title = "Terminal"
		}
	case "browser":
		u := sArg(m, "url")
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return "", errors.New("url must start with http:// or https://")
		}
		p.Props["url"] = u
		if title == "" {
			title = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
			if len(title) > 30 {
				title = title[:30]
			}
		}
	case "plugin":
		id := sArg(m, "plugin_id")
		pl, ok := a.Plugins.Get(id)
		if !ok {
			return "", fmt.Errorf("no plugin %s", id)
		}
		p.Props["plugin"] = id
		if title == "" {
			title = pl.Name
		}
	case "files":
		p.Props["path"] = sArg(m, "path")
		if title == "" {
			title = "Files"
		}
	case "log":
		if title == "" {
			title = "Action log"
		}
	default:
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	p.Title = title
	np, err := a.openPane(p, sArg(m, "placement"), store.ActorAgent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("opened %s (%s) in group %s", np.ID, np.Title, a.groupOf(np.ID)), nil
}

func (a *App) groupOf(pane string) string {
	sp, _ := a.Store.Layout("")
	var gid string
	var walk func(n *store.Node)
	walk = func(n *store.Node) {
		for _, t := range n.Tabs {
			if t == pane {
				gid = n.ID
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	if sp != nil {
		walk(sp.Root)
	}
	return gid
}

func toolPaneUpdate(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	p, err := a.agentPane(id)
	if err != nil {
		return "", err
	}
	if p.Kind != "markdown" && p.Kind != "html" {
		return "", errors.New("only markdown/html panes can be updated this way")
	}
	_, err = a.Store.UpdatePane(id, store.ActorAgent, func(p *store.Pane) error {
		if t := sArg(m, "title"); t != "" {
			p.Title = t
		}
		if c, ok := m["content"]; ok {
			p.Props["content"] = str(c)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return "updated " + id, nil
}

func toolPaneMove(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if _, err := a.agentPane(id); err != nil {
		return "", err
	}
	if err := a.Store.MovePane(id, sArg(m, "placement"), store.ActorAgent); err != nil {
		return "", err
	}
	return fmt.Sprintf("moved %s to group %s", id, a.groupOf(id)), nil
}

func toolPaneClose(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if _, err := a.agentPane(id); err != nil {
		return "", err
	}
	p, err := a.Store.ClosePane(id, store.ActorAgent)
	if err != nil {
		return "", err
	}
	a.cleanupPane(p)
	return "closed " + id, nil
}

func (a *App) cleanupPane(p store.Pane) {
	switch p.Kind {
	case "terminal":
		if t := str(p.Props["term"]); t != "" {
			a.Terms.Close(t)
		}
	case "secure-view":
		a.Vault.Forget(p.ID)
	}
	a.pendMu.Lock()
	delete(a.pluginCtx, p.ID)
	a.pendMu.Unlock()
}

func toolPreset(a *App, m map[string]any) (string, error) {
	if err := a.Store.Preset("", sArg(m, "name"), store.ActorAgent); err != nil {
		return "", err
	}
	return "arranged as " + sArg(m, "name") + " (the user can undo this)", nil
}

func toolSpaceCreate(a *App, m map[string]any) (string, error) {
	cur := a.Store.ActiveSpace()
	sp := a.Store.CreateSpace(sArg(m, "name"))
	a.Store.SwitchSpace(cur) // never move the user
	return fmt.Sprintf("created space %s %q (the user is still in their current space)", sp.ID, sp.Name), nil
}

func toolFilesList(a *App, m map[string]any) (string, error) {
	ents, err := a.Roots.List(sArg(m, "path"))
	if err != nil {
		return "", err
	}
	var lines []string
	for _, e := range ents {
		if e.Dir {
			lines = append(lines, e.Path+"/")
		} else {
			lines = append(lines, fmt.Sprintf("%s (%d bytes)", e.Path, e.Size))
		}
	}
	if len(lines) == 0 {
		return "(empty)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// ---- sheets ----

func toolSheetRead(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canReadPath(p); err != nil {
		return "", err
	}
	sh, err := a.Sheets.Load(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (%d rows, rev %s)\n%s", p, len(sh.Rows), sh.Rev, csvExcerpt(sh.Headers, sh.Rows, iArg(m, "max_rows", 200))), nil
}

func toolSheetCreate(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if !strings.HasSuffix(p, ".csv") {
		return "", errors.New("path must end in .csv")
	}
	var rows [][]string
	if arr, ok := m["rows"].([]any); ok {
		for _, r := range arr {
			rows = append(rows, strs(r))
		}
	}
	if _, err := a.Sheets.Create(p, strs(m["headers"]), rows); err != nil {
		return "", err
	}
	np, err := a.openPane(store.Pane{Kind: "sheet", Title: path.Base(p), Props: map[string]any{"path": p}}, sArg(m, "placement"), store.ActorAgent)
	if err != nil {
		return "", err
	}
	a.broadcast(map[string]any{"t": "fileChanged", "path": p})
	return fmt.Sprintf("created %s and opened it as %s", p, np.ID), nil
}

func toolSheetEdit(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canReadPath(p); err != nil {
		return "", err
	}
	b, _ := json.Marshal(m["ops"])
	var ops []sheets.Op
	if err := json.Unmarshal(b, &ops); err != nil {
		return "", fmt.Errorf("bad ops: %w", err)
	}
	sh, err := a.Sheets.Apply(p, "", ops)
	if err != nil {
		return "", err
	}
	if pane, ok := a.Store.FindPane("sheet", "path", p); ok {
		a.Store.UpdatePane(pane.ID, store.ActorAgent, func(*store.Pane) error { return nil })
	}
	a.broadcast(map[string]any{"t": "sheet", "sheet": sh, "by": "agent"})
	return fmt.Sprintf("applied %d edit(s); %s now has %d rows (rev %s)", len(ops), p, len(sh.Rows), sh.Rev), nil
}

func toolSheetView(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	p, err := a.agentPane(id)
	if err != nil {
		return "", err
	}
	if p.Kind != "sheet" {
		return "", errors.New("not a sheet pane")
	}
	v := sheets.View{SortCol: iArg(m, "sort_col", -1), SortDesc: bArg(m, "desc"), Filter: sArg(m, "filter"), FilterCol: iArg(m, "filter_col", -1)}
	if name := sArg(m, "sort_by"); name != "" {
		sh, err := a.Sheets.Load(str(p.Props["path"]))
		if err != nil {
			return "", err
		}
		v.SortCol = -1
		for i, h := range sh.Headers {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				v.SortCol = i
			}
		}
		if v.SortCol < 0 {
			return "", fmt.Errorf("no column named %q (columns: %s)", name, strings.Join(sh.Headers, ", "))
		}
	}
	_, err = a.Store.UpdatePane(id, store.ActorAgent, func(p *store.Pane) error { p.Props["view"] = v; return nil })
	if err != nil {
		return "", err
	}
	return "view updated (the file is unchanged)", nil
}

// ---- review ----

func toolReviewRead(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canReadPath(p); err != nil {
		return "", err
	}
	c, err := a.Review.Content(p)
	if err != nil {
		return "", err
	}
	d, err := a.Review.Get(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("=== %s ===\n%s\n=== threads ===\n%s", p, truncate(c, 40000), jsonOut(d.Threads)), nil
}

func (a *App) reviewChanged(p string, d *review.Doc) {
	if pane, ok := a.Store.FindPane("review", "path", p); ok {
		a.Store.UpdatePane(pane.ID, store.ActorAgent, func(*store.Pane) error { return nil })
	}
	a.broadcast(map[string]any{"t": "review", "path": p, "doc": d})
}

func toolReviewComment(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canReadPath(p); err != nil {
		return "", err
	}
	d, th, err := a.Review.AddThread(p, review.Anchor{Exact: sArg(m, "quote")}, store.ActorAgent, sArg(m, "text"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(p, d)
	return "started thread " + th.ID, nil
}

func toolReviewReply(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	d, err := a.Review.Reply(p, sArg(m, "thread_id"), store.ActorAgent, sArg(m, "text"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(p, d)
	return "replied", nil
}

func toolReviewSuggest(a *App, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canReadPath(p); err != nil {
		return "", err
	}
	d, err := a.Review.Suggest(p, sArg(m, "thread_id"), store.ActorAgent, str(m["find"]), str(m["replace"]), sArg(m, "note"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(p, d)
	return "suggestion added; waiting for the user to accept or reject it", nil
}

// ---- secure ----

func toolVaultList(a *App, _ map[string]any) (string, error) {
	return jsonOut(a.Vault.Names()), nil
}

func toolVaultReveal(a *App, m map[string]any) (string, error) {
	name := sArg(m, "name")
	it, ok := a.Vault.Get(name)
	if !ok {
		return "", fmt.Errorf("no vault item %q (use vault_list)", name)
	}
	// The pane is private (visibility none) and locked: only you can change that.
	p, err := a.openPane(store.Pane{Kind: "secure-view", Title: it.Name, Visibility: store.VisNone, VisLocked: true,
		Props: map[string]any{"item": it.Name, "kind": it.Kind, "reason": sArg(m, "reason")}}, "right", store.ActorAgent)
	if err != nil {
		return "", err
	}
	a.Vault.Deliver(p.ID, it.Value)
	// Value goes to the shell only: not the log, not the wire, not pane props.
	a.broadcast(map[string]any{"t": "secureValue", "pane": p.ID, "value": it.Value})
	a.Store.Activate(p.ID) // a secure delivery is the one thing that takes focus: you asked for it
	return fmt.Sprintf(SecureAck, p.ID), nil
}

func toolSecretRequest(a *App, m map[string]any) (string, error) {
	h := a.Vault.NewHandle(sArg(m, "label"), sArg(m, "purpose"))
	_, err := a.openPane(store.Pane{Kind: "secret-input", Title: h.Label, Visibility: store.VisNone, VisLocked: true,
		Props: map[string]any{"handle": h.ID, "label": h.Label, "purpose": h.Purpose}}, "right", store.ActorAgent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("handle %s created; a secure entry pane is open for the user. The value will never be shown to you. Use the handle as auth_handle once they confirm it's entered.", h.ID), nil
}

func toolBrokerList(a *App, _ map[string]any) (string, error) { return jsonOut(a.Broker.List()), nil }

func toolBrokerCall(a *App, m map[string]any) (string, error) {
	token := ""
	if h := sArg(m, "auth_handle"); h != "" {
		v, err := a.Vault.Use(h)
		if err != nil {
			return "", err
		}
		token = v
	}
	res, err := a.Broker.Call(sArg(m, "connection"), sArg(m, "path"), token)
	if err != nil {
		return "", err
	}
	return jsonOut(res), nil
}

// ---- terminal ----

func toolTermPropose(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if id == "" {
		for _, p := range a.Store.Panes("") {
			if p.Kind == "terminal" && p.Visibility != store.VisNone {
				id = p.ID
				break
			}
		}
	}
	if id == "" {
		np, err := a.openPane(store.Pane{Kind: "terminal", Title: "Terminal"}, "", store.ActorAgent)
		if err != nil {
			return "", err
		}
		id = np.ID
	}
	if _, err := a.agentPane(id); err != nil {
		return "", err
	}
	cmd := str(m["command"])
	_, err := a.Store.UpdatePane(id, store.ActorAgent, func(p *store.Pane) error {
		if p.Kind != "terminal" {
			return errors.New("not a terminal pane")
		}
		p.Props["proposal"] = map[string]any{"command": cmd, "why": sArg(m, "why")}
		return nil
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("proposed in %s; the user decides whether to run it. Use term_read afterwards to see the output.", id), nil
}

func toolTermRead(a *App, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	p, err := a.agentPane(id)
	if err != nil {
		return "", err
	}
	if p.Visibility != store.VisFull {
		return "", deny("the user has hidden this terminal's output from you")
	}
	t := a.Terms.Get(str(p.Props["term"]))
	if t == nil {
		return "", errors.New("that terminal isn't running yet (the user hasn't opened it)")
	}
	return t.Tail(iArg(m, "lines", 60)), nil
}

// ---- plugins ----

func toolPluginWrite(a *App, m map[string]any) (string, error) {
	man := plugins.Manifest{ID: sArg(m, "id"), Name: sArg(m, "name"), Description: sArg(m, "description"), Permissions: strs(m["permissions"])}
	sort.Strings(man.Permissions)
	pl, err := a.Plugins.Write(man, str(m["html"]))
	if err != nil {
		return "", err
	}
	if _, ok := a.Store.FindPane("plugin", "plugin", pl.ID); !ok {
		if _, err := a.openPane(store.Pane{Kind: "plugin", Title: pl.Name, Props: map[string]any{"plugin": pl.ID}}, sArg(m, "placement"), store.ActorAgent); err != nil {
			return "", err
		}
	}
	a.broadcastState()
	var need []string
	for _, perm := range pl.Permissions {
		if pl.Grants[perm] == "" {
			need = append(need, perm)
		}
	}
	msg := fmt.Sprintf("plugin %s v%d loaded (draft until the user clicks Keep).", pl.ID, pl.Version)
	if len(need) > 0 {
		msg += " The user will be asked to grant on first use: " + strings.Join(need, ", ")
	}
	return msg, nil
}

func toolPluginList(a *App, _ map[string]any) (string, error) {
	var out []map[string]any
	for _, p := range a.Plugins.List() {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "permissions": p.Permissions, "grants": p.Grants, "kept": p.Kept, "version": p.Version})
	}
	if len(out) == 0 {
		return "no plugins yet", nil
	}
	return jsonOut(out), nil
}
