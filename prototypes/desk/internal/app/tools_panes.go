package app

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/pvjammer/nexus/prototypes/desk/internal/desk"
	"github.com/pvjammer/nexus/prototypes/desk/internal/files"
	"github.com/pvjammer/nexus/prototypes/desk/internal/plugins"
	"github.com/pvjammer/nexus/prototypes/desk/internal/review"
	"github.com/pvjammer/nexus/prototypes/desk/internal/sheets"
	"github.com/pvjammer/nexus/prototypes/desk/internal/store"
)

type deskItemT = desk.Item
type deskTaskT = desk.Task

func deskProposal(project, kind, title, reason string) desk.Proposal {
	return desk.Proposal{Project: project, Kind: kind, Title: title, Reason: reason}
}
func deskItem(project, kind, title, body, source string) desk.Item {
	return desk.Item{Project: project, Kind: kind, Title: title, Body: body, Source: source}
}
func deskTask(project, title, brief, from string) desk.Task {
	return desk.Task{Project: project, Title: title, Brief: brief, From: from}
}

// ---- text helpers ----

func readText(r *files.Roots, p string, max int64) (string, error) {
	abs, err := r.Resolve(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory (use files_list)", p)
	}
	if st.Size() > max {
		return "", fmt.Errorf("%s is too large (%d bytes)", p, st.Size())
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return "", fmt.Errorf("%s is not a text file", p)
	}
	return string(b), nil
}

var searchSkip = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "bin": true, "target": true, ".venv": true, "__pycache__": true, "browsers": true}

func searchDir(root, dir, q string, max int) []string {
	if max <= 0 {
		return nil
	}
	ql := strings.ToLower(q)
	var hits []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || len(hits) >= max {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != dir && (searchSkip[d.Name()] || (strings.HasPrefix(d.Name(), ".") && d.Name() != ".foreman")) {
				return filepath.SkipDir
			}
			return nil
		}
		if files.LooksSecret(p) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 1<<20 {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Bytes()
			if n == 1 && bytes.IndexByte(line, 0) >= 0 {
				return nil
			}
			if strings.Contains(strings.ToLower(string(line)), ql) {
				rel, _ := filepath.Rel(dir, p)
				hits = append(hits, fmt.Sprintf("%s/%s:%d: %s", root, filepath.ToSlash(rel), n, truncate(strings.TrimSpace(string(line)), 200)))
				if len(hits) >= max {
					return nil
				}
			}
		}
		return nil
	})
	return hits
}

// ---- layout tools ----

func toolLayoutGet(a *App, c *Ctx, _ map[string]any) (string, error) {
	sp, err := a.Store.Layout(c.Space)
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
			for _, ch := range n.Children {
				s += desc(ch, depth+1)
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
			s += fmt.Sprintf("%s  - %s [%s] %q opened-by:%s%s\n", ind, p.ID, p.Kind, p.Title, p.Opener, act)
		}
		return s
	}
	return fmt.Sprintf("project %s layout\n%s", c.Project.ID, desc(sp.Root, 0)), nil
}

func (a *App) openPane(space string, p store.Pane, placement, actor string) (store.Pane, error) {
	np, err := a.Store.OpenPane(space, p, placement, actor)
	if err != nil {
		return np, err
	}
	if path := str(np.Props["path"]); path != "" {
		a.watchFile(path)
	}
	return np, nil
}

func toolPaneOpen(a *App, c *Ctx, m map[string]any) (string, error) {
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
		pth := a.agentPath(c, sArg(m, "path"))
		if pth == "" {
			return "", errors.New("path is required")
		}
		if err := a.canRead(c, pth); err != nil {
			return "", err
		}
		if kind == "sheet" {
			if _, err := a.Sheets.Load(pth); err != nil {
				return "", err
			}
		} else if _, err := a.Review.Content(pth); err != nil {
			return "", err
		}
		if ex, ok := a.Store.FindPaneIn(c.Space, kind, "path", pth); ok {
			return fmt.Sprintf("already open as %s", ex.ID), nil
		}
		p.Props["path"] = pth
		if title == "" {
			title = path.Base(pth)
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
			title = truncate(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), 30)
		}
	case "workitem":
		w, err := a.checkItemRef(sArg(m, "item"))
		if err != nil {
			return "", err
		}
		if w.Project != c.Project.ID {
			return "", deny("work item %s is in project %s, not yours", w.Ref, w.Project)
		}
		p.Props["ref"] = w.Ref
		if t := sArg(m, "task"); t != "" {
			p.Props["task"] = t
		}
		if title == "" {
			title = w.Title
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
		pth := sArg(m, "path")
		if pth == "" {
			pth = c.Project.ID
		}
		if err := a.canRead(c, pth); err != nil {
			return "", err
		}
		p.Props["path"] = pth
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
	np, err := a.openPane(c.Space, p, sArg(m, "placement"), store.ActorAgent)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("opened %s (%s)", np.ID, np.Title)
	if kind == "html" {
		out += "." + truncationNote(str(p.Props["content"]))
	}
	return out, nil
}

func toolPaneUpdate(a *App, c *Ctx, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	p, err := a.agentPane(c, id)
	if err != nil {
		return "", err
	}
	if p.Kind != "markdown" && p.Kind != "html" {
		return "", errors.New("only markdown/html panes can be updated this way")
	}
	var content string
	np, err := a.Store.UpdatePane(id, store.ActorAgent, func(p *store.Pane) error {
		if t := sArg(m, "title"); t != "" {
			p.Title = t
		}
		if ct, ok := m["content"]; ok {
			if bArg(m, "append") {
				p.Props["content"] = str(p.Props["content"]) + str(ct)
			} else {
				p.Props["content"] = str(ct)
			}
		}
		content = str(p.Props["content"])
		return nil
	})
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("updated %s (%d chars).", id, len(content))
	if np.Kind == "html" {
		msg += truncationNote(content)
	}
	return msg, nil
}

// truncationNote warns the agent when an HTML page looks cut off (usually
// the model's output limit), so it continues instead of rewriting it whole.
func truncationNote(html string) string {
	low := strings.ToLower(html)
	started := strings.Contains(low, "<html") || strings.Contains(low, "<!doctype") || strings.Contains(low, "<body")
	if !started || strings.Contains(low, "</html>") || strings.Contains(low, "</body>") {
		return ""
	}
	tail := html
	if len(tail) > 80 {
		tail = tail[len(tail)-80:]
	}
	return fmt.Sprintf(" WARNING: the page looks cut off: it has no closing </html> (it ends with %q). That's usually your output limit. Do NOT rewrite it from the start: continue it with pane_update append=true, adding the rest in parts of under 10 KB each, ending with </html>.", tail)
}

func toolPaneMove(a *App, c *Ctx, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if _, err := a.agentPane(c, id); err != nil {
		return "", err
	}
	if err := a.Store.MovePane(id, sArg(m, "placement"), store.ActorAgent); err != nil {
		return "", err
	}
	return "moved " + id, nil
}

func toolPaneClose(a *App, c *Ctx, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if _, err := a.agentPane(c, id); err != nil {
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

func toolPreset(a *App, c *Ctx, m map[string]any) (string, error) {
	if err := a.Store.Preset(c.Space, sArg(m, "name"), store.ActorAgent); err != nil {
		return "", err
	}
	return "arranged as " + sArg(m, "name") + " (the user can undo this)", nil
}

// ---- sheets ----

func toolSheetRead(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	sh, err := a.Sheets.Load(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s (%d rows, rev %s)\n%s", p, len(sh.Rows), sh.Rev, csvExcerpt(sh.Headers, sh.Rows, iArg(m, "max_rows", 200))), nil
}

func toolSheetCreate(a *App, c *Ctx, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if !strings.HasSuffix(p, ".csv") {
		return "", errors.New("path must end in .csv")
	}
	if err := a.canWrite(c, p); err != nil {
		return "", err
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
	np, err := a.openPane(c.Space, store.Pane{Kind: "sheet", Title: path.Base(p), Props: map[string]any{"path": p}}, sArg(m, "placement"), store.ActorAgent)
	if err != nil {
		return "", err
	}
	a.broadcast(map[string]any{"t": "fileChanged", "path": p})
	return fmt.Sprintf("created %s and opened it as %s", p, np.ID), nil
}

func toolSheetEdit(a *App, c *Ctx, m map[string]any) (string, error) {
	p := sArg(m, "path")
	if err := a.canWrite(c, p); err != nil {
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
	if pane, ok := a.Store.FindPaneIn(c.Space, "sheet", "path", p); ok {
		a.Store.UpdatePane(pane.ID, store.ActorAgent, func(*store.Pane) error { return nil })
	}
	a.broadcast(map[string]any{"t": "sheet", "sheet": sh, "by": "agent"})
	return fmt.Sprintf("applied %d edit(s); %s now has %d rows (rev %s)", len(ops), p, len(sh.Rows), sh.Rev), nil
}

func toolSheetView(a *App, c *Ctx, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	p, err := a.agentPane(c, id)
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

func toolReviewRead(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	ct, err := a.Review.Content(p)
	if err != nil {
		return "", err
	}
	d, err := a.Review.Get(p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("=== %s ===\n%s\n=== threads ===\n%s", p, truncate(ct, 40000), jsonOut(d.Threads)), nil
}

func (a *App) reviewChanged(space, p string, d *review.Doc) {
	if pane, ok := a.Store.FindPaneIn(space, "review", "path", p); ok {
		a.Store.UpdatePane(pane.ID, store.ActorAgent, func(*store.Pane) error { return nil })
	}
	a.broadcast(map[string]any{"t": "review", "path": p, "doc": d})
}

func toolReviewComment(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	d, th, err := a.Review.AddThread(p, review.Anchor{Exact: sArg(m, "quote")}, store.ActorAgent, sArg(m, "text"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(c.Space, p, d)
	return "started thread " + th.ID, nil
}

func toolReviewReply(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	d, err := a.Review.Reply(p, sArg(m, "thread_id"), store.ActorAgent, sArg(m, "text"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(c.Space, p, d)
	return "replied", nil
}

func toolReviewSuggest(a *App, c *Ctx, m map[string]any) (string, error) {
	p := a.agentPath(c, sArg(m, "path"))
	if err := a.canRead(c, p); err != nil {
		return "", err
	}
	d, err := a.Review.Suggest(p, sArg(m, "thread_id"), store.ActorAgent, str(m["find"]), str(m["replace"]), sArg(m, "note"))
	if err != nil {
		return "", err
	}
	a.reviewChanged(c.Space, p, d)
	return "suggestion added; waiting for the user to accept or reject it", nil
}

// ---- secure ----

func toolVaultList(a *App, _ *Ctx, _ map[string]any) (string, error) {
	return jsonOut(a.Vault.Names()), nil
}

func toolVaultReveal(a *App, c *Ctx, m map[string]any) (string, error) {
	name := sArg(m, "name")
	it, ok := a.Vault.Get(name)
	if !ok {
		return "", fmt.Errorf("no vault item %q (use vault_list)", name)
	}
	if !c.inProject() {
		// Home has no panes: a secure card on the dashboard.
		b := make([]byte, 4)
		rand.Read(b)
		id := "home-" + hex.EncodeToString(b)
		a.Vault.Deliver(id, it.Value)
		a.broadcast(map[string]any{"t": "secureCard", "id": id, "item": it.Name, "kind": it.Kind, "reason": sArg(m, "reason"), "value": it.Value})
		return fmt.Sprintf(SecureAck, "a secure card on the dashboard"), nil
	}
	p, err := a.openPane(c.Space, store.Pane{Kind: "secure-view", Title: it.Name, Visibility: store.VisNone, VisLocked: true,
		Props: map[string]any{"item": it.Name, "kind": it.Kind, "reason": sArg(m, "reason")}}, "", store.ActorAgent)
	if err != nil {
		return "", err
	}
	a.Vault.Deliver(p.ID, it.Value)
	a.broadcast(map[string]any{"t": "secureValue", "pane": p.ID, "value": it.Value})
	a.Store.Activate(p.ID)
	return fmt.Sprintf(SecureAck, "pane "+p.ID), nil
}

func toolSecretRequest(a *App, c *Ctx, m map[string]any) (string, error) {
	h := a.Vault.NewHandle(sArg(m, "label"), sArg(m, "purpose"))
	if !c.inProject() {
		a.broadcast(map[string]any{"t": "secretCard", "handle": h.ID, "label": h.Label, "purpose": h.Purpose, "agent": c.Run.ID})
	} else if _, err := a.openPane(c.Space, store.Pane{Kind: "secret-input", Title: h.Label, Visibility: store.VisNone, VisLocked: true,
		Props: map[string]any{"handle": h.ID, "label": h.Label, "purpose": h.Purpose}}, "", store.ActorAgent); err != nil {
		return "", err
	}
	return fmt.Sprintf("handle %s created; a secure entry box is open for the user. You'll never see the value. Use the handle as auth_handle once they confirm.", h.ID), nil
}

func toolBrokerList(a *App, _ *Ctx, _ map[string]any) (string, error) { return jsonOut(a.Broker.List()), nil }

func toolBrokerCall(a *App, _ *Ctx, m map[string]any) (string, error) {
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

func toolTermPropose(a *App, c *Ctx, m map[string]any) (string, error) {
	id := sArg(m, "pane_id")
	if id == "" {
		for _, p := range a.Store.Panes(c.Space) {
			if p.Kind == "terminal" {
				id = p.ID
				break
			}
		}
	}
	if id == "" {
		np, err := a.openPane(c.Space, store.Pane{Kind: "terminal", Title: "Terminal"}, "", store.ActorAgent)
		if err != nil {
			return "", err
		}
		id = np.ID
	}
	if _, err := a.agentPane(c, id); err != nil {
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
	return fmt.Sprintf("proposed in %s; the user decides whether to run it. You can't see the output: ask the user what happened if you need to know.", id), nil
}

// ---- plugins ----

func (a *App) writePlugin(m map[string]any) (*plugins.Plugin, []string, error) {
	man := plugins.Manifest{ID: sArg(m, "id"), Name: sArg(m, "name"), Description: sArg(m, "description"), Permissions: sortedPerms(m["permissions"])}
	pl, err := a.Plugins.Write(man, str(m["html"]))
	if err != nil {
		return nil, nil, err
	}
	a.broadcastState()
	a.broadcast(map[string]any{"t": "pluginChanged", "plugin": pl.ID})
	var need []string
	for _, perm := range pl.Permissions {
		if pl.Grants[perm] == "" {
			need = append(need, perm)
		}
	}
	return pl, need, nil
}

func toolPluginWrite(a *App, c *Ctx, m map[string]any) (string, error) {
	pl, need, err := a.writePlugin(m)
	if err != nil {
		return "", err
	}
	if _, ok := a.Store.FindPaneIn(c.Space, "plugin", "plugin", pl.ID); !ok {
		if _, err := a.openPane(c.Space, store.Pane{Kind: "plugin", Title: pl.Name, Props: map[string]any{"plugin": pl.ID}}, sArg(m, "placement"), store.ActorAgent); err != nil {
			return "", err
		}
	}
	msg := fmt.Sprintf("plugin %s v%d loaded (draft until the user clicks Keep).", pl.ID, pl.Version)
	if len(need) > 0 {
		msg += " The user will be asked to grant on first use: " + strings.Join(need, ", ")
	}
	return msg, nil
}
