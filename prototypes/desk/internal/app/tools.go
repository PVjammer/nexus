package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/actionlog"
	"github.com/pvjammer/nexus/prototypes/desk/internal/broker"
	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
	"github.com/pvjammer/nexus/prototypes/desk/internal/projects"
	"github.com/pvjammer/nexus/prototypes/desk/internal/store"
)

// Tool is one capability an agent can call. Definitions and policy live in
// the core, so any agent loop gets the same tools. Kinds says which agents
// get it (home, project, task).
type Tool struct {
	Name        string         `json:"name"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Kinds       []string       `json:"-"`
	Addon       string         `json:"-"` // only when this add-on is on for the agent's project
	run         func(a *App, c *Ctx, args map[string]any) (string, error)
}

// Ctx is who is calling: which agent, for which project, in which space.
type Ctx struct {
	Run     *Run
	Kind    string
	Project projects.Project // zero for the home agent
	Space   string           // the project's layout space ("" for home)
	Actor   string           // for the action log
}

func (c *Ctx) inProject() bool { return c.Project.ID != "" }

// SecureAck is the fixed protocol string a secure tool returns to the model.
const SecureAck = "DATA_DELIVERED_OUT_OF_BAND: the value was shown directly to the user (%s). You did not receive it and cannot see, repeat, summarize or guess it. Tell the user it is on their screen."

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

const placementDesc = "Where to put it: 'target' (your own group, default; never steals the user's focus), 'right' or 'down' (new split beside the user's focused group), a group id, or '<groupId>:left|right|top|bottom'."

var paneKinds = []string{"markdown", "html", "sheet", "review", "terminal", "browser", "plugin", "files", "log", "workitem"}

var (
	H   = []string{KindHome}
	P   = []string{KindProject}
	T   = []string{KindTask}
	HP  = []string{KindHome, KindProject}
	PT  = []string{KindProject, KindTask}
	HPT = []string{KindHome, KindProject, KindTask}
)

func (a *App) tools() []Tool {
	return []Tool{
		// ---- projects (overview) ----
		{Name: "projects_list", Label: "Projects", Kinds: HP,
			Description: "List the user's projects with counts (open workstreams, needs-you, backlog, items) and activity per day for the last 14 days.",
			Parameters:  obj(nil, map[string]any{}), run: toolProjectsList},
		{Name: "project_get", Label: "Project detail", Kinds: HP,
			Description: "Everything Nexus knows about a project: brief, foreman workstreams (with report paths), backlog, foreman inbox, recent events, git state, docs, Nexus items and tasks.",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (default: your own project)")}), run: toolProjectGet},
		{Name: "project_discover", Label: "Find projects", Kinds: H,
			Description: "Scan the user's workspace for folders that look like projects (git repos with a README). Read-only.",
			Parameters:  obj(nil, map[string]any{"path": sprop("folder to scan (default: the configured workspace roots)")}), run: toolProjectDiscover},
		{Name: "project_add", Label: "Add project", Kinds: H,
			Description: "Add a folder as a project in Nexus. Nexus only reads the folder; it never writes to it. Give a short brief (one or two sentences) in your own words from what you read.",
			Parameters: obj([]string{"path"}, map[string]any{
				"path": sprop("absolute folder path"), "name": sprop("display name"), "brief": sprop("one or two sentences: what this project is"),
			}), run: toolProjectAdd},
		{Name: "project_brief", Label: "Set brief", Kinds: HP,
			Description: "Set a project's brief (what it is, in one or two sentences). Nexus stores it; the project folder is untouched.",
			Parameters:  obj([]string{"brief"}, map[string]any{"project": sprop("project id (default: yours)"), "brief": sprop("the brief")}), run: toolProjectBrief},

		// ---- files (read-only on your projects) ----
		{Name: "files_list", Label: "List files", Kinds: HPT,
			Description: "List files under a path like '<project>/docs'. Path '' lists the roots (one per project, plus 'desk' for Nexus's own files).",
			Parameters:  obj(nil, map[string]any{"path": sprop("directory path")}), run: toolFilesList},
		{Name: "file_read", Label: "Read file", Kinds: HPT,
			Description: "Read a text file, with line numbers. Files that look like secrets (.env, keys, credentials) are refused.",
			Parameters: obj([]string{"path"}, map[string]any{
				"path": sprop("e.g. 'homelab/design/gitlab-server.md'"), "offset": iprop("first line, 1-based (default 1)"), "limit": iprop("lines (default 400)"),
			}), run: toolFileRead},
		{Name: "search", Label: "Search", Kinds: HPT,
			Description: "Search a project's text files for a phrase (case-insensitive). Returns path:line: text. Skips .git, node_modules, binaries and secret-looking files.",
			Parameters: obj([]string{"query"}, map[string]any{
				"project": sprop("project id (default: yours; the home agent must give one, or 'all')"), "query": sprop("text to find"), "max": iprop("max matches (default 60)"),
			}), run: toolSearch},

		// ---- captures and items ----
		{Name: "capture_list", Label: "Captures", Kinds: H,
			Description: "List captures (quick notes the user dumped) that still need filing, with their ids.",
			Parameters:  obj(nil, map[string]any{}), run: toolCaptureList},
		{Name: "capture_propose", Label: "Propose filing", Kinds: H,
			Description: "Propose where a capture belongs. The user accepts, changes or dismisses it; you never file directly.",
			Parameters: obj([]string{"capture_id", "project", "kind"}, map[string]any{
				"capture_id": sprop("capture id"), "project": sprop("project id"), "kind": eprop("what it is", "task", "note", "question"),
				"title": sprop("a clean one-line title (keep the user's words where you can)"), "reason": sprop("why this project, in a few words"),
			}), run: toolCapturePropose},
		{Name: "item_add", Label: "Add item", Kinds: HP,
			Description: "Add an item (task, note or question) to a project's Nexus list. Use sparingly: propose the next few steps, not a whole tree.",
			Parameters: obj([]string{"kind", "title"}, map[string]any{
				"project": sprop("project id (default: yours)"), "kind": eprop("", "task", "note", "question"), "title": sprop("one line"), "body": sprop("details"),
			}), run: toolItemAdd},
		{Name: "item_update", Label: "Update item", Kinds: HP,
			Description: "Change an item's title or mark it done/open.",
			Parameters: obj([]string{"item_id"}, map[string]any{
				"item_id": sprop("item id"), "title": sprop("new title"), "state": eprop("", "open", "done"),
			}), run: toolItemUpdate},

		// ---- dispatch ----
		{Name: "task_draft", Label: "Draft task", Kinds: P,
			Description: "Draft a task for a task agent: a short title and a brief (goal, what done looks like, constraints, where to look). It opens as a draft the user edits and starts; you can't start it yourself. Task agents can read the project and write a report; they can't change files.",
			Parameters: obj([]string{"title", "brief"}, map[string]any{
				"title": sprop("short title"), "brief": sprop("the brief, in markdown"), "from": sprop("what it came from: an item id or backlog text (optional)"),
			}), run: toolTaskDraft},
		{Name: "task_list", Label: "Tasks", Kinds: HP,
			Description: "List dispatched tasks (drafts, running, finished) with status, summary and report path.",
			Parameters:  obj(nil, map[string]any{"project": sprop("project id (default: yours; home: all)")}), run: toolTaskList},
		{Name: "report_write", Label: "Write report", Kinds: T,
			Description: "Write (or replace) your report as markdown. The user reviews it in Nexus and can comment on it.",
			Parameters:  obj([]string{"content"}, map[string]any{"content": sprop("the full report in markdown")}), run: toolReportWrite},
		{Name: "task_finish", Label: "Finish task", Kinds: T,
			Description: "End the task. Call once, after report_write. status: done, or failed if you couldn't do it.",
			Parameters: obj([]string{"status", "summary"}, map[string]any{
				"status": eprop("", "done", "failed"), "summary": sprop("one or two sentences for the user's needs-you list"),
			}), run: toolTaskFinish},

		// ---- dashboard widgets ----
		{Name: "widget_write", Label: "Write widget", Kinds: H,
			Description: "Create or update a custom widget: a plugin (one self-contained HTML file, see the plugin guide) placed on the home screen. Only for things no built-in widget kind shows (see home_add first). It runs sandboxed with no network and gets data only through window.nexus for permissions the user grants.",
			Parameters: obj([]string{"id", "name", "html"}, map[string]any{
				"id": sprop("lowercase-dashed id; reuse it to update"), "name": sprop("display name"), "description": sprop("one line"),
				"permissions": aprop("e.g. ['tool:projects_summary']", sprop("")), "html": sprop("complete HTML document"), "wide": bprop("span two columns"),
			}), run: toolWidgetWrite},
		{Name: "widget_remove", Label: "Remove widget", Kinds: H,
			Description: "Remove a widget from the dashboard (the plugin itself is kept).",
			Parameters:  obj([]string{"id"}, map[string]any{"id": sprop("widget or plugin id")}), run: toolWidgetRemove},
		{Name: "plugin_list", Label: "Plugins", Kinds: HP,
			Description: "List plugins with their permissions and grant status.",
			Parameters:  obj(nil, map[string]any{}), run: toolPluginList},

		// ---- layout (project agents, in their project's space) ----
		{Name: "layout_get", Label: "Layout", Kinds: P,
			Description: "See your project's layout: groups (with ids), their tabs, which group the user is focused on, and which is your target.",
			Parameters:  obj(nil, map[string]any{}), run: toolLayoutGet},
		{Name: "pane_open", Label: "Open pane", Kinds: P,
			Description: "Open a pane in the project. Kinds: markdown/html (you supply content; html is sandboxed with no scripts), sheet (CSV path), review (.md/.txt/.html path: the user can comment), terminal (the user's shell in the project folder; you can't read it), browser (url, to follow along), plugin (plugin_id), files (path), log, workitem (item ref: its tasks and done-when, optionally with one task docked). Opens in your group in the background with a badge.",
			Parameters: obj([]string{"kind"}, map[string]any{
				"kind": eprop("pane kind", paneKinds...), "title": sprop("tab title"),
				"path": sprop("file path like '<project>/README.md'"), "content": sprop("markdown or html body"), "url": sprop("browser url"),
				"plugin_id": sprop("plugin id"), "item": sprop("workitem: the work item ref (from work_items)"), "task": sprop("workitem: a task id to show docked"), "placement": sprop(placementDesc),
			}), run: toolPaneOpen},
		{Name: "pane_update", Label: "Update pane", Kinds: P,
			Description: "Replace (or, with append=true, extend) the title and/or content of a markdown or html pane. For a long page, write it in parts: pane_open with the first part, then pane_update append=true for each next part.",
			Parameters: obj([]string{"pane_id"}, map[string]any{"pane_id": sprop("pane id"), "title": sprop("new title"), "content": sprop("new content, or the next part when append is true"),
				"append": bprop("add content to the end instead of replacing it")}), run: toolPaneUpdate},
		{Name: "pane_move", Label: "Move pane", Kinds: P,
			Description: "Move a pane to another group or a new split.",
			Parameters:  obj([]string{"pane_id", "placement"}, map[string]any{"pane_id": sprop("pane id"), "placement": sprop(placementDesc)}), run: toolPaneMove},
		{Name: "pane_close", Label: "Close pane", Kinds: P,
			Description: "Close a pane.",
			Parameters:  obj([]string{"pane_id"}, map[string]any{"pane_id": sprop("pane id")}), run: toolPaneClose},
		{Name: "layout_preset", Label: "Arrange", Kinds: P,
			Description: "Rearrange the project's panes: single, side-by-side, columns or grid. The user can undo it in one click.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": eprop("preset", "single", "side-by-side", "columns", "grid")}), run: toolPreset},

		// ---- sheets ----
		{Name: "sheet_read", Label: "Read sheet", Kinds: HPT,
			Description: "Read a CSV sheet: headers and rows (0-based row numbers).",
			Parameters:  obj([]string{"path"}, map[string]any{"path": sprop("csv path"), "max_rows": iprop("default 200")}), run: toolSheetRead},
		{Name: "sheet_create", Label: "New sheet", Kinds: P,
			Description: "Create a new CSV sheet under desk/<project>/ and open it.",
			Parameters: obj([]string{"path", "headers"}, map[string]any{
				"path": sprop("e.g. 'desk/<project>/options.csv'"), "headers": aprop("column names", sprop("")),
				"rows": aprop("initial rows", aprop("", sprop(""))), "placement": sprop(placementDesc),
			}), run: toolSheetCreate},
		{Name: "sheet_edit", Label: "Edit sheet", Kinds: P,
			Description: "Apply edits to a CSV sheet you can write (under desk/). ops: {op:'set',row,col,value} | {op:'addRow',cells:[...],at?} | {op:'deleteRow',row} | {op:'addCol',value} | {op:'renameCol',col,value} | {op:'deleteCol',col}. 0-based; read first.",
			Parameters: obj([]string{"path", "ops"}, map[string]any{
				"path": sprop("csv path"),
				"ops": aprop("edit operations", map[string]any{"type": "object", "properties": map[string]any{
					"op":  eprop("", "set", "addRow", "deleteRow", "addCol", "renameCol", "deleteCol"),
					"row": iprop(""), "col": iprop(""), "value": sprop(""), "cells": aprop("", sprop("")), "at": iprop(""),
				}, "required": []string{"op"}}),
			}), run: toolSheetEdit},
		{Name: "sheet_view", Label: "Sort/filter", Kinds: P,
			Description: "Set how a sheet pane is displayed (sort and filter). The file is unchanged.",
			Parameters: obj([]string{"pane_id"}, map[string]any{
				"pane_id": sprop("sheet pane id"), "sort_by": sprop("column name"), "sort_col": iprop("column index, -1 none"), "desc": bprop("descending"),
				"filter": sprop("show rows containing this text ('' clears)"), "filter_col": iprop("restrict to a column, -1 any"),
			}), run: toolSheetView},

		// ---- review ----
		{Name: "review_read", Label: "Read document", Kinds: HPT,
			Description: "Read a document and its comment threads.",
			Parameters:  obj([]string{"path"}, map[string]any{"path": sprop("document path")}), run: toolReviewRead},
		{Name: "review_comment", Label: "Comment", Kinds: P,
			Description: "Start a comment thread on a document, anchored on an exact quote from it.",
			Parameters: obj([]string{"path", "quote", "text"}, map[string]any{
				"path": sprop("document path"), "quote": sprop("exact text from the document"), "text": sprop("your comment"),
			}), run: toolReviewComment},
		{Name: "review_reply", Label: "Reply", Kinds: P,
			Description: "Reply in a comment thread.",
			Parameters:  obj([]string{"path", "thread_id", "text"}, map[string]any{"path": sprop("document path"), "thread_id": sprop("thread id"), "text": sprop("reply")}), run: toolReviewReply},
		{Name: "review_suggest", Label: "Suggest edit", Kinds: P,
			Description: "Propose an edit to a document. The user accepts or rejects it; you never edit it directly. find must occur exactly once.",
			Parameters: obj([]string{"path", "find", "replace"}, map[string]any{
				"path": sprop("document path"), "thread_id": sprop("attach to this thread"), "find": sprop("exact source text"), "replace": sprop("replacement"), "note": sprop("why"),
			}), run: toolReviewSuggest},

		// ---- secure ----
		{Name: "vault_list", Label: "Vault items", Kinds: HP,
			Description: "List the names and kinds of items in the user's vault. Values are never returned.",
			Parameters:  obj(nil, map[string]any{}), run: toolVaultList},
		{Name: "vault_reveal", Label: "Secure: show to user", Kinds: HP,
			Description: "SECURE TOOL. Show a vault item's value to the user. It goes straight to their screen and is NEVER returned to you; you get a fixed DATA_DELIVERED_OUT_OF_BAND acknowledgement. Never claim to know the value.",
			Parameters:  obj([]string{"name"}, map[string]any{"name": sprop("vault item name"), "reason": sprop("one line shown to the user")}), run: toolVaultReveal},
		{Name: "secret_request", Label: "Secure: ask for a secret", Kinds: HP,
			Description: "Ask the user to enter a secret in a secure entry box. Returns a handle like 'secret:ab12cd34'; the value is never shown to you. Use it with broker_call.",
			Parameters:  obj([]string{"label"}, map[string]any{"label": sprop("what you need"), "purpose": sprop("what you'll use it for")}), run: toolSecretRequest},
		{Name: "broker_list", Label: "Connections", Kinds: HP,
			Description: "List the connections the credential broker can call for you.",
			Parameters:  obj(nil, map[string]any{}), run: toolBrokerList},
		{Name: "broker_call", Label: "Call connection", Kinds: HP,
			Description: "Call an allowlisted path on a broker connection. The broker attaches credentials; you never hold them.",
			Parameters:  obj([]string{"connection", "path"}, map[string]any{"connection": sprop("connection name"), "path": sprop("allowlisted path"), "auth_handle": sprop("secret handle")}), run: toolBrokerCall},

		// ---- terminal (propose only) ----
		{Name: "term_propose", Label: "Propose command", Kinds: P,
			Description: "Propose a shell command in the user's terminal (in the project folder). It appears as a suggestion they Run, Edit or Dismiss. You can't run it and you can't read the terminal; ask the user to tell you or paste what matters.",
			Parameters:  obj([]string{"command"}, map[string]any{"pane_id": sprop("terminal pane id (optional)"), "command": sprop("the command"), "why": sprop("one line")}), run: toolTermPropose},

		// ---- plugins in a project ----
		{Name: "plugin_write", Label: "Write plugin", Kinds: P,
			Description: "Create or update (hot-reload) a plugin pane: one self-contained HTML file, sandboxed, no network, data only through window.nexus for declared and granted permissions.",
			Parameters: obj([]string{"id", "name", "html"}, map[string]any{
				"id": sprop("lowercase-dashed id"), "name": sprop("display name"), "description": sprop("one line"),
				"permissions": aprop("e.g. ['connect:homelab-mock','tool:sheet_read','context']", sprop("")),
				"html":        sprop("complete HTML document"), "placement": sprop(placementDesc),
			}), run: toolPluginWrite},
	}
}

func (a *App) allTools() []Tool {
	return append(append(append(append(append(append(append(append(append(append(a.tools(), a.wikiTools()...), a.codeTools()...), a.homeTools()...), a.workTools()...), a.onboardTools()...), a.workstreamTools()...), a.episodeTools()...), a.personaToolset()...), a.mentionTools()...), a.styleTools()...)
}

// toolsFor is the tool set of one agent: by its kind, plus add-on tools
// switched on for its project.
func (a *App) toolsFor(r *Run) []Tool {
	var out []Tool
	kind := toolKind(r.Kind)
	if r.Kind == KindPersona || r.Kind == KindFork { // a persona or side session reads, and changes nothing
		kind = r.base()
	}
	for _, t := range a.allTools() {
		if t.Addon != "" && (r.Project == "" || !a.addonOn(r.Project, t.Addon)) {
			continue
		}
		if (r.Kind == KindPersona || r.Kind == KindFork) && !personaTools[t.Name] {
			continue
		}
		for _, k := range t.Kinds {
			if k == kind {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

func (a *App) toolByName(name string) (Tool, bool) {
	for _, t := range a.allTools() {
		if t.Name == name {
			return t, true
		}
	}
	if name == "projects_summary" {
		return Tool{Name: name, run: func(a *App, _ *Ctx, _ map[string]any) (string, error) { return jsonOut(a.projectsSummaryFor("")), nil }}, true
	}
	return Tool{}, false
}

func (a *App) ctxFor(r *Run) *Ctx {
	c := &Ctx{Run: r, Kind: toolKind(r.Kind), Actor: r.label()}
	if r.Kind == KindPersona || r.Kind == KindFork {
		c.Kind = r.base()
	}
	if p, ok := a.Projects.Get(r.Project); ok {
		c.Project, c.Space = p, p.Space
	}
	return c
}

// homeChanges are the home agent's tools that carry out a request; after one,
// it's reminded that a finished request ends the episode (episodes.go).
var homeChanges = map[string]bool{"work_task_add": true, "work_task_update": true, "work_item_set": true, "work_item_add": true, "item_add": true, "item_update": true, "capture_propose": true}

// CallTool runs a tool for an agent and logs it.
func (a *App) CallTool(r *Run, name string, args map[string]any) (string, error) {
	c := a.ctxFor(r)
	var t Tool
	ok := false
	for _, x := range a.toolsFor(r) {
		if x.Name == name {
			t, ok = x, true
		}
	}
	if !ok {
		return "", fmt.Errorf("tool %s isn't available to the %s", name, r.label())
	}
	if args == nil {
		args = map[string]any{}
	}
	// One question per turn. Small models fire several ask_user calls (or
	// keep working) after asking, and the user gets a pile of cards and a
	// moving target. Once it has asked, the turn is over.
	r.mu.Lock()
	asked := r.asked
	r.mu.Unlock()
	if asked != "" {
		return "", fixErr("you already asked %q this turn. End your turn now and write nothing more: the user's answer is your next message, and you can ask your next question then", asked)
	}
	out, err := "", a.guardArgs(r, args) // a private project's things don't exist for outsiders
	guarded := err != nil                // (its answer only echoes what was asked)
	if err == nil {
		out, err = t.run(a, c, args)
	}
	if a.anyPrivate() && !guarded {
		out = a.scrub(runProject(r), out)
		if err != nil {
			if m := a.scrub(runProject(r), err.Error()); m != err.Error() {
				err = fixErr("%s", m)
			}
		}
	}
	if name == "ask_user" && err == nil {
		r.mu.Lock()
		r.asked = oneLine(sArg(args, "question"), 120)
		r.mu.Unlock()
		go func() { // after the tool message is in the transcript
			time.Sleep(300 * time.Millisecond)
			a.broadcastDesk()
		}()
	}
	e := actionlog.Entry{Actor: c.Actor, Action: name, Target: logTarget(args), Space: c.Space}
	if err != nil {
		e.Detail = err.Error()
		e.Denied = isDenial(err)
	}
	switch name {
	case "layout_get", "plugin_list", "vault_list", "broker_list", "projects_list", "capture_list", "task_list", "wiki_list", "plan_update",
		"home_show", "home_add", "home_change", "home_remove", "home_grid", "home_reset", "home_undo", // home_* log through homeChanged
		"work_items", "work_tasks", "work_task_add", "work_task_update", "work_item_set", "needs_you", "work_limit": // work_* log through workChanged
	default:
		a.Log.Add(e)
	}
	if err == nil && r.Kind == KindHome && homeChanges[name] {
		out += "\n(If that's everything the user asked for, end your turn with episode_close and a one-line summary: the next conversation starts fresh.)"
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
	for _, k := range []string{"pane_id", "path", "project", "title", "name", "id", "capture_id", "item_id", "connection", "label", "query", "kind"} {
		if v := str(args[k]); v != "" {
			return truncate(v, 80)
		}
	}
	return ""
}

func (a *App) serveTools(w http.ResponseWriter, r *http.Request) {
	run := a.runByToken(r.Header.Get("X-Nexus-Token"))
	if run == nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(a.toolsFor(run))
}

func (a *App) serveCall(w http.ResponseWriter, r *http.Request) {
	run := a.runByToken(r.Header.Get("X-Nexus-Token"))
	if run == nil || r.Method != http.MethodPost {
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
	out, err := a.CallTool(run, req.Name, req.Args)
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

// ---- path policy ----

func rootOf(p string) string {
	r, _, _ := strings.Cut(strings.TrimPrefix(p, "/"), "/")
	return r
}

// appPath turns an absolute path inside a project into its app path
// ("<project>/rel"); anything else is returned unchanged.
func (a *App) appPath(p string) string {
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~/") {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
	}
	best, out := "", p
	for _, n := range a.Roots.Names() {
		dir, _ := a.Roots.Dir(n)
		if (p == dir || strings.HasPrefix(p, dir+string(filepath.Separator))) && len(dir) > len(best) {
			best = dir // the most specific root wins (repos nested in repos)
			rel, _ := filepath.Rel(dir, p)
			out = n
			if rel != "." {
				out = n + "/" + filepath.ToSlash(rel)
			}
		}
	}
	return out
}

// agentPath normalizes a path an agent gave: absolute paths map to their
// project, and a project or task agent's bare relative path ("docs/x.md")
// means its own project.
func (a *App) agentPath(c *Ctx, p string) string {
	p = strings.TrimPrefix(a.appPath(strings.TrimSpace(p)), "./")
	if c != nil && c.inProject() && p != "" {
		if _, ok := a.Roots.Dir(rootOf(p)); !ok {
			p = c.Project.ID + "/" + p
		}
	}
	return p
}

// notAProjectYet explains an absolute path that isn't inside any project
// (appPath already maps those that are): during onboarding the home agent
// tries to read a candidate's README before adding it.
func notAProjectYet(p string) error {
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "~") {
		return nil
	}
	return fixErr("%s isn't in a project, so it can't be read yet. project_discover's readme field has the start of each candidate's README; after project_add, read it as '<project id>/README.md' and fix the brief with project_brief if needed", p)
}

// canRead: the home agent reads any project; project and task agents read
// their own project and its desk folder. Secret-looking files are refused.
func (a *App) canRead(c *Ctx, p string) error {
	if files.LooksSecret(p) {
		return deny("%s looks like a secret file; agents can't read those. Share secrets through the vault instead.", p)
	}
	if a.pathWalled(c.Project.ID, p) {
		return fixErr("no such file or folder: %s", p)
	}
	if c.Kind == KindHome || p == "" {
		return nil
	}
	r := rootOf(p)
	if r == c.Project.ID || p == "desk" || strings.HasPrefix(p, "desk/"+c.Project.ID) || strings.HasPrefix(p, "wiki/"+c.Project.ID) {
		return nil
	}
	return deny("you work in project %s; %s is outside it (ask the user, or the home agent)", c.Project.ID, p)
}

// canWrite: agents write only under desk/ (their project's folder there).
// Your project folders are read-only for every agent in this prototype.
func (a *App) canWrite(c *Ctx, p string) error {
	if !a.Roots.AgentWritable(p) {
		return deny("%s is in your project folder, which is read-only for agents. Write under desk/%s/ instead.", p, c.Project.ID)
	}
	if c.inProject() && !strings.HasPrefix(p, "desk/"+c.Project.ID+"/") {
		return deny("write under desk/%s/", c.Project.ID)
	}
	return nil
}

func (a *App) agentPane(c *Ctx, id string) (store.Pane, error) {
	p, ok := a.Store.Pane(id)
	if !ok || p.Visibility == store.VisNone || p.Space != c.Space {
		return store.Pane{}, fmt.Errorf("no pane %s in your project", id)
	}
	return p, nil
}

func (a *App) projectArg(c *Ctx, m map[string]any) (projects.Project, error) {
	id := sArg(m, "project")
	if id == "" {
		if c.inProject() {
			return c.Project, nil
		}
		return projects.Project{}, errors.New("which project? pass project (see projects_list)")
	}
	if c.inProject() && id != c.Project.ID && c.Kind != KindHome {
		return projects.Project{}, deny("you work in project %s", c.Project.ID)
	}
	p, ok := a.Projects.Get(id)
	if !ok || a.walled(c.Project.ID, id) {
		return projects.Project{}, fmt.Errorf("no project %q (projects: %s)", id, a.visibleIDs(c.Project.ID))
	}
	return p, nil
}

// ---- project tools ----

func toolProjectsList(a *App, c *Ctx, _ map[string]any) (string, error) {
	s := a.projectsSummaryFor(c.Project.ID)
	busy := a.busyNote(c.Project.ID)
	if len(s) == 0 {
		return firstNonEmpty(busy, "no projects yet"), nil
	}
	return jsonOut(s) + "\n" + busy, nil
}

func toolProjectGet(a *App, c *Ctx, m map[string]any) (string, error) {
	p, err := a.projectArg(c, m)
	if err != nil {
		return "", err
	}
	d, _ := a.detail(p.ID)
	snap := a.Desk.Snapshot()
	var items, tasks []any
	for _, it := range snap.Items {
		if it.Project == p.ID {
			items = append(items, it)
		}
	}
	for _, t := range snap.Tasks {
		if t.Project == p.ID {
			tasks = append(tasks, map[string]any{"id": t.ID, "title": t.Title, "status": t.Status, "summary": t.Summary, "report": t.Report})
		}
	}
	var ws []map[string]any
	for _, w := range d.Workstreams {
		e := map[string]any{"name": w.Name, "kind": w.Kind, "agent": w.AgentStatus, "report_status": w.ReportStatus, "summary": w.Summary, "updated": w.Updated, "closed": w.Closed, "goal": truncate(w.Goal, 400)}
		if w.Report != "" {
			e["report"] = p.ID + "/" + w.Report
		}
		ws = append(ws, e)
	}
	var events []string
	for i, e := range d.Events {
		if i >= 25 {
			break
		}
		events = append(events, fmt.Sprintf("%s %s %s: %s", e.Time.Format("01-02 15:04"), e.WS, e.Kind, oneLine(e.Summary, 160)))
	}
	out := map[string]any{
		"id": p.ID, "name": p.Name, "path": p.Path, "root": p.ID + "/", "brief": briefOf(d), "foreman": d.Foreman,
		"workstreams": ws, "workstreams_archived": d.Archived, "backlog": d.Backlog, "foreman_inbox": d.Inbox,
		"recent_events": events, "git": d.Git, "docs": d.Docs, "nexus_items": items, "nexus_tasks": tasks,
	}
	return jsonOut(out), nil
}

func toolProjectDiscover(a *App, cx *Ctx, m map[string]any) (string, error) {
	roots := a.cfg.ScanRoots
	if p := sArg(m, "path"); p != "" {
		roots = []string{p}
	}
	cands := projects.Discover(roots, 6, a.cfg.Foreman)
	have := map[string]string{}
	walledPath := map[string]bool{}
	for _, p := range a.Projects.List() {
		have[p.Path] = p.ID
		if a.walled(cx.Project.ID, p.ID) {
			walledPath[p.Path] = true
		}
	}
	var out []map[string]any
	for _, c := range cands {
		if walledPath[c.Path] {
			continue // a private project isn't a candidate either
		}
		e := map[string]any{"path": c.Path, "name": c.Name, "foreman": c.Foreman, "git": c.Git, "readme": c.Readme}
		if id := have[c.Path]; id != "" {
			e["already_added_as"] = id
		}
		out = append(out, e)
		if len(out) >= 60 {
			break
		}
	}
	if len(out) == 0 {
		return "nothing that looks like a project under " + strings.Join(roots, ", "), nil
	}
	return jsonOut(out), nil
}

func toolProjectAdd(a *App, c *Ctx, m map[string]any) (string, error) {
	p, err := a.addProject(sArg(m, "name"), sArg(m, "path"), sArg(m, "brief"), c.Actor)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added %q as project %s (root %s/). Its card is on the dashboard.", p.Name, p.ID, p.ID), nil
}

func (a *App) addProject(name, dir, brief, actor string) (projects.Project, error) {
	p, err := a.Projects.Add(name, dir, brief, func(n string) string { return a.Store.NewSpace(n) })
	if err != nil {
		return p, err
	}
	a.mountProject(p)
	a.Log.Add(actionlog.Entry{Actor: actor, Action: "add project", Target: p.Name, Detail: p.Path})
	a.refreshDetails()
	a.broadcastState()
	a.broadcastDesk()
	return p, nil
}

func toolProjectBrief(a *App, c *Ctx, m map[string]any) (string, error) {
	p, err := a.projectArg(c, m)
	if err != nil {
		return "", err
	}
	a.Projects.Update(p.ID, func(q *projects.Project) { q.Brief = sArg(m, "brief") })
	a.refreshDetails()
	a.broadcastDesk()
	return "brief updated for " + p.ID, nil
}

// ---- files ----

func toolFilesList(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := notAProjectYet(p); err != nil {
		return "", err
	}
	if p == "" && c.inProject() {
		p = c.Project.ID
	}
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	ents, err := a.Roots.List(p)
	if err != nil {
		return "", err
	}
	var lines []string
	for _, e := range ents {
		if a.pathWalled(c.Project.ID, e.Path) {
			continue
		}
		switch {
		case e.Dir:
			lines = append(lines, e.Path+"/")
		case files.LooksSecret(e.Path):
			lines = append(lines, e.Path+" (secret-looking: agents can't read it)")
		default:
			lines = append(lines, fmt.Sprintf("%s (%d bytes)", e.Path, e.Size))
		}
	}
	if len(lines) == 0 {
		return "(empty)", nil
	}
	return strings.Join(lines, "\n"), nil
}

func toolFileRead(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := notAProjectYet(p); err != nil {
		return "", err
	}
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	text, err := readText(a.Roots, p, 2<<20)
	if err != nil {
		return "", err
	}
	lines := strings.Split(text, "\n")
	off := iArg(m, "offset", 1)
	if off < 1 {
		off = 1
	}
	lim := iArg(m, "limit", 400)
	var b strings.Builder
	end := off - 1 + lim
	if end > len(lines) {
		end = len(lines)
	}
	for i := off - 1; i < end; i++ {
		fmt.Fprintf(&b, "%5d  %s\n", i+1, truncate(lines[i], 600))
	}
	if end < len(lines) {
		fmt.Fprintf(&b, "… %d more lines (offset=%d)\n", len(lines)-end, end+1)
	}
	return b.String(), nil
}

func toolSearch(a *App, c *Ctx, m map[string]any) (string, error) {
	q := sArg(m, "query")
	if q == "" {
		return "", errors.New("query is required")
	}
	var targets []projects.Project
	if c.Kind == KindHome && (sArg(m, "project") == "all" || sArg(m, "project") == "") {
		targets = a.visibleProjects(c.Project.ID)
	} else {
		p, err := a.projectArg(c, m)
		if err != nil {
			return "", err
		}
		targets = []projects.Project{p}
	}
	max := iArg(m, "max", 60)
	var hits []string
	for _, p := range targets {
		hits = append(hits, searchDir(p.ID, p.Path, q, max-len(hits))...)
		if len(hits) >= max {
			break
		}
	}
	if len(hits) == 0 {
		return "no matches for " + q, nil
	}
	return strings.Join(hits, "\n"), nil
}

// ---- captures, items ----

func toolCaptureList(a *App, _ *Ctx, _ map[string]any) (string, error) {
	var out []map[string]any
	for _, c := range a.Desk.Snapshot().Captures {
		if c.Status == "new" || c.Status == "proposed" {
			e := map[string]any{"id": c.ID, "text": c.Text, "status": c.Status, "created": c.Created}
			if c.Proposal != nil {
				e["proposal"] = c.Proposal
			}
			out = append(out, e)
		}
	}
	if len(out) == 0 {
		return "no captures waiting", nil
	}
	return jsonOut(out), nil
}

func toolCapturePropose(a *App, c *Ctx, m map[string]any) (string, error) {
	p, err := a.projectArg(c, m)
	if err != nil {
		return "", err
	}
	err = a.Desk.Propose(sArg(m, "capture_id"), deskProposal(p.ID, sArg(m, "kind"), sArg(m, "title"), sArg(m, "reason")))
	if err != nil {
		return "", err
	}
	return "proposed; the user accepts or changes it on the dashboard", nil
}

func toolItemAdd(a *App, c *Ctx, m map[string]any) (string, error) {
	p, err := a.projectArg(c, m)
	if err != nil {
		return "", err
	}
	it := a.Desk.AddItem(deskItem(p.ID, sArg(m, "kind"), sArg(m, "title"), str(m["body"]), "agent"))
	return fmt.Sprintf("added %s to %s", it.ID, p.ID), nil
}

func toolItemUpdate(a *App, c *Ctx, m map[string]any) (string, error) {
	it, err := a.Desk.UpdateItem(sArg(m, "item_id"), func(it *deskItemT) {
		if c.inProject() && it.Project != c.Project.ID {
			return
		}
		if t := sArg(m, "title"); t != "" {
			it.Title = t
		}
		if s := sArg(m, "state"); s == "open" || s == "done" {
			it.State = s
		}
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s is now %s: %s", it.ID, it.State, it.Title), nil
}

// ---- dispatch ----

func toolTaskDraft(a *App, c *Ctx, m map[string]any) (string, error) {
	t := a.Desk.AddTask(deskTask(c.Project.ID, sArg(m, "title"), str(m["brief"]), sArg(m, "from")))
	np, err := a.openPane(c.Space, store.Pane{Kind: "task", Title: "Task: " + truncate(t.Title, 40), Props: map[string]any{"task": t.ID}}, "", store.ActorAgent)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("drafted %s (pane %s). The user edits the brief and clicks Start; you can't start it.", t.ID, np.ID), nil
}

func toolTaskList(a *App, c *Ctx, m map[string]any) (string, error) {
	want := ""
	if c.inProject() {
		want = c.Project.ID
	} else {
		want = sArg(m, "project")
	}
	var out []map[string]any
	for _, t := range a.Desk.Snapshot().Tasks {
		if (want != "" && t.Project != want) || a.walled(c.Project.ID, t.Project) {
			continue
		}
		out = append(out, map[string]any{"id": t.ID, "project": t.Project, "title": t.Title, "status": t.Status, "summary": t.Summary, "report": t.Report, "reviewed": t.Reviewed})
	}
	if len(out) == 0 {
		return "no tasks", nil
	}
	return jsonOut(out), nil
}

func toolReportWrite(a *App, c *Ctx, m map[string]any) (string, error) {
	t, ok := a.Desk.Task(c.Run.Task)
	if !ok {
		return "", errors.New("no task")
	}
	// Every write is a new version; nothing an agent writes replaces an
	// earlier report you may already have read.
	base := fmt.Sprintf("desk/%s/tasks/%s-%s", t.Project, t.ID, projects.Slug(t.Title))
	p := base + ".md"
	for n := 2; ; n++ {
		abs, err := a.Roots.Resolve(p)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			if err := files.WriteAtomic(abs, []byte(str(m["content"]))); err != nil {
				return "", err
			}
			break
		}
		p = fmt.Sprintf("%s-v%d.md", base, n)
	}
	a.Desk.UpdateTask(t.ID, func(t *deskTaskT) error {
		if t.Report != "" {
			t.Reports = append(t.Reports, t.Report)
		}
		t.Report = p
		return nil
	})
	a.broadcast(map[string]any{"t": "fileChanged", "path": p})
	if t.Report != "" {
		return fmt.Sprintf("saved as a new version: %s (the earlier one, %s, is kept)", p, t.Report), nil
	}
	return "report saved to " + p, nil
}

func toolTaskFinish(a *App, c *Ctx, m map[string]any) (string, error) {
	status := sArg(m, "status")
	if status != "done" && status != "failed" {
		status = "done"
	}
	t, err := a.Desk.UpdateTask(c.Run.Task, func(t *deskTaskT) error {
		t.Status, t.Summary = status, oneLine(sArg(m, "summary"), 400)
		return nil
	})
	if err != nil {
		return "", err
	}
	c.Run.finishing = true
	a.notifyTask(t.ID)
	return "task closed; the user has been told. Stop here.", nil
}

// ---- widgets ----

func toolWidgetWrite(a *App, c *Ctx, m map[string]any) (string, error) {
	pl, need, err := a.writePlugin(m)
	if err != nil {
		return "", err
	}
	a.Desk.AddWidget(pl.ID, bArg(m, "wide"))
	msg := fmt.Sprintf("widget %s v%d is on the dashboard (draft until the user clicks Keep).", pl.ID, pl.Version)
	if len(need) > 0 {
		msg += " The user will be asked to grant on first use: " + strings.Join(need, ", ")
	}
	return msg, nil
}

func toolWidgetRemove(a *App, _ *Ctx, m map[string]any) (string, error) {
	a.Desk.RemoveWidget(sArg(m, "id"))
	return "removed from the dashboard", nil
}

func toolPluginList(a *App, _ *Ctx, _ map[string]any) (string, error) {
	var out []map[string]any
	for _, p := range a.Plugins.List() {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "permissions": p.Permissions, "grants": p.Grants, "kept": p.Kept, "version": p.Version})
	}
	if len(out) == 0 {
		return "no plugins yet", nil
	}
	return jsonOut(out), nil
}

func sortedPerms(v any) []string {
	p := strs(v)
	sort.Strings(p)
	return p
}

var _ = path.Base

// AddProject adds a project as you (used by the -project flag).
func (a *App) AddProject(name, dir string) (projects.Project, error) {
	return a.addProject(name, dir, "", you)
}
