// Package store holds the UI state the core is authoritative for: spaces,
// panes and each space's layout tree. Layout ops are shared by both actors
// (you and the agent); agent ops snapshot first so they can be undone in one
// step, and agent ops never move the user's focus.
//
// Prototype note: persistence is a single JSON file. Not a storage decision.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	ActorUser  = "you"
	ActorAgent = "agent"

	// Visibility: what the agent can see of a pane.
	VisFull  = "full"  // content may enter agent context / be read by tools
	VisTitle = "title" // agent knows it exists (kind + title) only
	VisNone  = "none"  // agent doesn't know it exists
)

var ErrPinned = errors.New("pane is pinned by the user; ask them before closing or moving it")

type Node struct {
	Kind     string    `json:"kind"` // "split" | "group"
	ID       string    `json:"id"`
	Dir      string    `json:"dir,omitempty"` // split: "row" (side by side) | "col" (stacked)
	Sizes    []float64 `json:"sizes,omitempty"`
	Children []*Node   `json:"children,omitempty"`
	Tabs     []string  `json:"tabs,omitempty"`   // group: pane ids
	Active   string    `json:"active,omitempty"` // group: active pane id
}

type Space struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Root    *Node  `json:"root"`
	Target  string `json:"target"`  // agent's target group id
	Focused string `json:"focused"` // group the user last focused
}

type Pane struct {
	ID         string         `json:"id"`
	Space      string         `json:"space"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Props      map[string]any `json:"props,omitempty"`
	Opener     string         `json:"opener"`
	Visibility string         `json:"visibility"`
	VisLocked  bool           `json:"visLocked,omitempty"` // secure panes: only an explicit user action changes it
	Pinned     bool           `json:"pinned,omitempty"`
	Badge      bool           `json:"badge,omitempty"` // agent changed it and you haven't looked yet
}

type snapshot struct {
	root   []byte
	target string
	panes  []byte
	label  string
}

type State struct {
	Spaces      []*Space         `json:"spaces"`
	ActiveSpace string           `json:"activeSpace"`
	Panes       map[string]*Pane `json:"panes"`
	Seq         int              `json:"seq"`
}

type Store struct {
	mu    sync.Mutex
	st    State
	path  string
	undo  map[string][]snapshot // per space, agent ops only
	onChg func()
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, undo: map[string][]snapshot{}}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, fmt.Errorf("store: parse %s: %w", path, err)
		}
	}
	if s.st.Panes == nil {
		s.st.Panes = map[string]*Pane{}
	}
	if len(s.st.Spaces) == 0 {
		s.newSpaceLocked("Home")
	}
	if s.st.ActiveSpace == "" {
		s.st.ActiveSpace = s.st.Spaces[0].ID
	}
	return s, nil
}

func (s *Store) OnChange(f func()) { s.onChg = f }

func (s *Store) id(prefix string) string {
	s.st.Seq++
	return fmt.Sprintf("%s%d", prefix, s.st.Seq)
}

func (s *Store) newSpaceLocked(name string) *Space {
	g := &Node{Kind: "group", ID: s.id("g")}
	sp := &Space{ID: s.id("s"), Name: name, Root: g, Focused: g.ID}
	s.st.Spaces = append(s.st.Spaces, sp)
	return sp
}

// commit persists and notifies. Caller holds mu.
func (s *Store) commitLocked() {
	if s.path != "" {
		b, _ := json.MarshalIndent(s.st, "", "  ")
		tmp := s.path + ".tmp"
		if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err == nil {
			if os.WriteFile(tmp, b, 0o600) == nil {
				_ = os.Rename(tmp, s.path)
			}
		}
	}
	if s.onChg != nil {
		go s.onChg()
	}
}

// Snapshot returns a deep copy of the state for broadcasting.
func (s *Store) Snapshot() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.st)
	var out State
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Store) UndoDepth(space string) (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.undo[space]
	if len(u) == 0 {
		return 0, ""
	}
	return len(u), u[len(u)-1].label
}

func (s *Store) spaceLocked(id string) (*Space, error) {
	if id == "" {
		id = s.st.ActiveSpace
	}
	for _, sp := range s.st.Spaces {
		if sp.ID == id {
			return sp, nil
		}
	}
	return nil, fmt.Errorf("no such space %q", id)
}

func (s *Store) snapLocked(sp *Space, label string) {
	root, _ := json.Marshal(sp.Root)
	var ps []*Pane
	for _, p := range s.st.Panes {
		if p.Space == sp.ID {
			ps = append(ps, p)
		}
	}
	pb, _ := json.Marshal(ps)
	u := append(s.undo[sp.ID], snapshot{root: root, target: sp.Target, panes: pb, label: label})
	if len(u) > 50 {
		u = u[len(u)-50:]
	}
	s.undo[sp.ID] = u
}

// UndoAgent reverts the most recent agent layout op in a space.
func (s *Store) UndoAgent(spaceID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(spaceID)
	if err != nil {
		return "", err
	}
	u := s.undo[sp.ID]
	if len(u) == 0 {
		return "", errors.New("nothing to undo")
	}
	last := u[len(u)-1]
	s.undo[sp.ID] = u[:len(u)-1]
	var root Node
	_ = json.Unmarshal(last.root, &root)
	var ps []*Pane
	_ = json.Unmarshal(last.panes, &ps)
	for id, p := range s.st.Panes {
		if p.Space == sp.ID {
			delete(s.st.Panes, id)
		}
	}
	for _, p := range ps {
		s.st.Panes[p.ID] = p
	}
	sp.Root = &root
	sp.Target = last.target
	if findGroup(sp.Root, sp.Focused) == nil {
		sp.Focused = firstGroup(sp.Root).ID
	}
	s.commitLocked()
	return last.label, nil
}

// ---- spaces ----

func (s *Store) CreateSpace(name string) *Space {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp := s.newSpaceLocked(name)
	s.st.ActiveSpace = sp.ID
	s.commitLocked()
	return sp
}

func (s *Store) SwitchSpace(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(id)
	if err != nil {
		return err
	}
	s.st.ActiveSpace = sp.ID
	s.commitLocked()
	return nil
}

func (s *Store) ActiveSpace() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.ActiveSpace
}

// ---- panes ----

func (s *Store) Pane(id string) (Pane, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.st.Panes[id]
	if !ok {
		return Pane{}, false
	}
	return *p, true
}

// Panes returns copies of the panes in a space ("" = active).
func (s *Store) Panes(space string) []Pane {
	s.mu.Lock()
	defer s.mu.Unlock()
	if space == "" {
		space = s.st.ActiveSpace
	}
	var out []Pane
	sp, _ := s.spaceLocked(space)
	if sp == nil {
		return nil
	}
	walk(sp.Root, func(n *Node) {
		for _, t := range n.Tabs {
			if p := s.st.Panes[t]; p != nil {
				out = append(out, *p)
			}
		}
	})
	return out
}

// FindPane returns the first pane in the active space matching kind and a prop value.
func (s *Store) FindPane(kind, prop, val string) (Pane, bool) {
	for _, p := range s.Panes("") {
		if p.Kind == kind && fmt.Sprint(p.Props[prop]) == val {
			return p, true
		}
	}
	return Pane{}, false
}

// Placement: "target" (agent's group), "focused" (user's group), "right",
// "down" (new split beside the focused group), or a group id. Edge variants:
// "<groupId>:left|right|top|bottom".
func (s *Store) OpenPane(space string, p Pane, placement, actor string) (Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(space)
	if err != nil {
		return Pane{}, err
	}
	if actor == ActorAgent {
		s.snapLocked(sp, "open "+p.Title)
	}
	p.ID = s.id("p")
	p.Space = sp.ID
	p.Opener = actor
	if p.Visibility == "" {
		p.Visibility = VisFull
	}
	if p.Props == nil {
		p.Props = map[string]any{}
	}
	if actor == ActorAgent {
		p.Badge = true
	}
	g, err := s.resolvePlacementLocked(sp, placement, actor)
	if err != nil {
		return Pane{}, err
	}
	s.st.Panes[p.ID] = &p
	g.Tabs = append(g.Tabs, p.ID)
	// Agent never steals focus: in the user's focused group it adds a badged
	// background tab; elsewhere it may make its tab active.
	if actor == ActorUser || g.ID != sp.Focused || g.Active == "" {
		g.Active = p.ID
	}
	if actor == ActorUser {
		sp.Focused = g.ID
	}
	s.commitLocked()
	return p, nil
}

func (s *Store) resolvePlacementLocked(sp *Space, placement, actor string) (*Node, error) {
	if placement == "" {
		if actor == ActorAgent {
			placement = "target"
		} else {
			placement = "focused"
		}
	}
	switch placement {
	case "focused":
		if g := findGroup(sp.Root, sp.Focused); g != nil {
			return g, nil
		}
		return firstGroup(sp.Root), nil
	case "target":
		if g := findGroup(sp.Root, sp.Target); g != nil {
			return g, nil
		}
		// Create the agent's group: reuse an empty root group, else split right of root.
		if root := sp.Root; root.Kind == "group" && len(root.Tabs) == 0 {
			sp.Target = root.ID
			return root, nil
		}
		g := &Node{Kind: "group", ID: s.id("g")}
		sp.Root = &Node{Kind: "split", ID: s.id("n"), Dir: "row", Sizes: []float64{0.5, 0.5}, Children: []*Node{sp.Root, g}}
		sp.Target = g.ID
		return g, nil
	case "right", "down":
		base := findGroup(sp.Root, sp.Focused)
		if base == nil {
			base = firstGroup(sp.Root)
		}
		edge := "right"
		if placement == "down" {
			edge = "bottom"
		}
		return s.splitAtLocked(sp, base.ID, edge)
	}
	// "<group>" or "<group>:<edge>"
	gid, edge := placement, ""
	for i := len(placement) - 1; i >= 0; i-- {
		if placement[i] == ':' {
			gid, edge = placement[:i], placement[i+1:]
			break
		}
	}
	if findGroup(sp.Root, gid) == nil {
		return nil, fmt.Errorf("unknown placement %q (use target, focused, right, down, or a group id)", placement)
	}
	if edge == "" || edge == "center" {
		return findGroup(sp.Root, gid), nil
	}
	return s.splitAtLocked(sp, gid, edge)
}

// splitAtLocked creates a new empty group at an edge of group gid.
func (s *Store) splitAtLocked(sp *Space, gid, edge string) (*Node, error) {
	dir := "row"
	if edge == "top" || edge == "bottom" {
		dir = "col"
	}
	before := edge == "left" || edge == "top"
	ng := &Node{Kind: "group", ID: s.id("g")}
	parent, idx := findParent(sp.Root, gid)
	target := findGroup(sp.Root, gid)
	if target == nil {
		return nil, fmt.Errorf("no group %s", gid)
	}
	kids := []*Node{target, ng}
	if before {
		kids = []*Node{ng, target}
	}
	if parent != nil && parent.Dir == dir {
		// insert into existing split
		i := idx + 1
		if before {
			i = idx
		}
		parent.Children = append(parent.Children[:i], append([]*Node{ng}, parent.Children[i:]...)...)
		parent.Sizes = even(len(parent.Children))
		return ng, nil
	}
	split := &Node{Kind: "split", ID: s.id("n"), Dir: dir, Sizes: []float64{0.5, 0.5}, Children: kids}
	if parent == nil {
		sp.Root = split
	} else {
		parent.Children[idx] = split
	}
	return ng, nil
}

func (s *Store) MovePane(paneID, placement, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.st.Panes[paneID]
	if p == nil {
		return fmt.Errorf("no pane %s", paneID)
	}
	if actor == ActorAgent && p.Pinned {
		return ErrPinned
	}
	sp, _ := s.spaceLocked(p.Space)
	if actor == ActorAgent {
		s.snapLocked(sp, "move "+p.Title)
	}
	src := groupOf(sp.Root, paneID)
	// Moving the only tab out of a group to its own edge is a no-op split; handle by
	// detaching first, resolving placement, then cleaning up.
	if src != nil {
		src.Tabs = remove(src.Tabs, paneID)
		if src.Active == paneID {
			src.Active = last(src.Tabs)
		}
	}
	g, err := s.resolvePlacementLocked(sp, placement, actor)
	if err != nil {
		if src != nil {
			src.Tabs = append(src.Tabs, paneID)
		}
		return err
	}
	g.Tabs = append(g.Tabs, paneID)
	if actor == ActorUser || g.ID != sp.Focused {
		g.Active = paneID
	}
	if actor == ActorAgent {
		p.Badge = true
	} else {
		sp.Focused = g.ID
	}
	s.cleanupLocked(sp)
	s.commitLocked()
	return nil
}

func (s *Store) ClosePane(paneID, actor string) (Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.st.Panes[paneID]
	if p == nil {
		return Pane{}, fmt.Errorf("no pane %s", paneID)
	}
	if actor == ActorAgent && p.Pinned {
		return Pane{}, ErrPinned
	}
	sp, _ := s.spaceLocked(p.Space)
	if actor == ActorAgent {
		s.snapLocked(sp, "close "+p.Title)
	}
	if g := groupOf(sp.Root, paneID); g != nil {
		g.Tabs = remove(g.Tabs, paneID)
		if g.Active == paneID {
			g.Active = last(g.Tabs)
		}
	}
	out := *p
	delete(s.st.Panes, paneID)
	s.cleanupLocked(sp)
	s.commitLocked()
	return out, nil
}

// Activate is a user action: focus a tab (clears its badge).
func (s *Store) Activate(paneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.st.Panes[paneID]
	if p == nil {
		return
	}
	sp, _ := s.spaceLocked(p.Space)
	if g := groupOf(sp.Root, paneID); g != nil {
		g.Active = paneID
		sp.Focused = g.ID
	}
	p.Badge = false
	s.commitLocked()
}

func (s *Store) FocusGroup(space, gid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, _ := s.spaceLocked(space)
	if sp == nil || findGroup(sp.Root, gid) == nil {
		return
	}
	sp.Focused = gid
	if g := findGroup(sp.Root, gid); g != nil && g.Active != "" {
		if p := s.st.Panes[g.Active]; p != nil {
			p.Badge = false
		}
	}
	s.commitLocked()
}

// FocusedPane is the active tab of the user's focused group in the active space.
func (s *Store) FocusedPane() (Pane, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, _ := s.spaceLocked("")
	if sp == nil {
		return Pane{}, false
	}
	g := findGroup(sp.Root, sp.Focused)
	if g == nil || g.Active == "" {
		return Pane{}, false
	}
	p := s.st.Panes[g.Active]
	if p == nil {
		return Pane{}, false
	}
	return *p, true
}

func (s *Store) SetTarget(space, gid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(space)
	if err != nil {
		return err
	}
	if findGroup(sp.Root, gid) == nil {
		return fmt.Errorf("no group %s", gid)
	}
	sp.Target = gid
	s.commitLocked()
	return nil
}

func (s *Store) Resize(space, splitID string, sizes []float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, _ := s.spaceLocked(space)
	if sp == nil {
		return
	}
	walk(sp.Root, func(n *Node) {
		if n.ID == splitID && n.Kind == "split" && len(sizes) == len(n.Children) {
			n.Sizes = sizes
		}
	})
	s.commitLocked()
}

// UpdatePane applies a mutation (user or agent) to a pane's fields.
func (s *Store) UpdatePane(id string, actor string, f func(p *Pane) error) (Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.st.Panes[id]
	if p == nil {
		return Pane{}, fmt.Errorf("no pane %s", id)
	}
	if err := f(p); err != nil {
		return Pane{}, err
	}
	if actor == ActorAgent {
		p.Badge = true
	}
	s.commitLocked()
	return *p, nil
}

// Preset rearranges the panes of a space into a named layout (agent or user).
func (s *Store) Preset(space, name, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(space)
	if err != nil {
		return err
	}
	var ids []string
	walk(sp.Root, func(n *Node) { ids = append(ids, n.Tabs...) })
	if actor == ActorAgent {
		for _, id := range ids {
			if s.st.Panes[id].Pinned {
				return fmt.Errorf("pane %q: %w (rearranging the whole layout would move it)", s.st.Panes[id].Title, ErrPinned)
			}
		}
		s.snapLocked(sp, "layout "+name)
	}
	mk := func(tabs []string) *Node {
		g := &Node{Kind: "group", ID: s.id("g"), Tabs: tabs}
		g.Active = first(tabs)
		return g
	}
	var groups []*Node
	switch name {
	case "single", "focus":
		groups = []*Node{mk(ids)}
		sp.Root = groups[0]
	case "side-by-side", "columns":
		half := (len(ids) + 1) / 2
		if name == "columns" {
			for _, id := range ids {
				groups = append(groups, mk([]string{id}))
			}
		} else {
			groups = []*Node{mk(ids[:half]), mk(ids[half:])}
		}
		sp.Root = &Node{Kind: "split", ID: s.id("n"), Dir: "row", Sizes: even(len(groups)), Children: groups}
	case "grid":
		n := len(ids)
		if n < 2 {
			return s.presetFallback(sp, ids)
		}
		for _, id := range ids {
			groups = append(groups, mk([]string{id}))
		}
		top := groups[:(n+1)/2]
		bot := groups[(n+1)/2:]
		row := func(gs []*Node) *Node {
			if len(gs) == 1 {
				return gs[0]
			}
			return &Node{Kind: "split", ID: s.id("n"), Dir: "row", Sizes: even(len(gs)), Children: gs}
		}
		sp.Root = &Node{Kind: "split", ID: s.id("n"), Dir: "col", Sizes: []float64{0.5, 0.5}, Children: []*Node{row(top), row(bot)}}
	default:
		return fmt.Errorf("unknown preset %q (single, side-by-side, columns, grid)", name)
	}
	s.cleanupLocked(sp)
	sp.Target = lastGroup(sp.Root).ID
	sp.Focused = firstGroup(sp.Root).ID
	s.commitLocked()
	return nil
}

func (s *Store) presetFallback(sp *Space, ids []string) error {
	g := &Node{Kind: "group", ID: s.id("g"), Tabs: ids, Active: first(ids)}
	sp.Root = g
	sp.Target, sp.Focused = g.ID, g.ID
	s.commitLocked()
	return nil
}

// cleanupLocked removes empty groups (keeping at least one) and collapses
// single-child splits. The agent's target group survives if empty.
func (s *Store) cleanupLocked(sp *Space) {
	var prune func(n *Node) *Node
	prune = func(n *Node) *Node {
		if n.Kind == "group" {
			if len(n.Tabs) == 0 && n.ID != sp.Target {
				return nil
			}
			return n
		}
		var kids []*Node
		var sizes []float64
		for i, c := range n.Children {
			if pc := prune(c); pc != nil {
				kids = append(kids, pc)
				if i < len(n.Sizes) {
					sizes = append(sizes, n.Sizes[i])
				}
			}
		}
		switch len(kids) {
		case 0:
			return nil
		case 1:
			return kids[0]
		}
		n.Children = kids
		n.Sizes = normalize(sizes, len(kids))
		return n
	}
	r := prune(sp.Root)
	if r == nil {
		r = &Node{Kind: "group", ID: s.id("g")}
	}
	sp.Root = r
	if findGroup(sp.Root, sp.Focused) == nil {
		sp.Focused = firstGroup(sp.Root).ID
	}
}

// ---- tree helpers ----

func walk(n *Node, f func(*Node)) {
	if n == nil {
		return
	}
	f(n)
	for _, c := range n.Children {
		walk(c, f)
	}
}

func findGroup(n *Node, id string) *Node {
	var out *Node
	walk(n, func(x *Node) {
		if x.Kind == "group" && x.ID == id {
			out = x
		}
	})
	return out
}

func groupOf(n *Node, pane string) *Node {
	var out *Node
	walk(n, func(x *Node) {
		for _, t := range x.Tabs {
			if t == pane {
				out = x
			}
		}
	})
	return out
}

func findParent(n *Node, id string) (*Node, int) {
	for i, c := range n.Children {
		if c.ID == id {
			return n, i
		}
		if p, j := findParent(c, id); p != nil {
			return p, j
		}
	}
	return nil, -1
}

func firstGroup(n *Node) *Node {
	if n.Kind == "group" {
		return n
	}
	return firstGroup(n.Children[0])
}

func lastGroup(n *Node) *Node {
	if n.Kind == "group" {
		return n
	}
	return lastGroup(n.Children[len(n.Children)-1])
}

// Groups lists group ids with their tabs for the active space (agent-facing).
func (s *Store) Layout(space string) (*Space, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, err := s.spaceLocked(space)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(sp)
	var out Space
	_ = json.Unmarshal(b, &out)
	return &out, nil
}

func remove(xs []string, x string) []string {
	out := xs[:0:0]
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func last(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[len(xs)-1]
}

func even(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1 / float64(n)
	}
	return out
}

func normalize(xs []float64, n int) []float64 {
	if len(xs) != n {
		return even(n)
	}
	sum := 0.0
	for _, x := range xs {
		sum += x
	}
	if sum <= 0 {
		return even(n)
	}
	out := make([]float64, n)
	for i, x := range xs {
		out[i] = x / sum
	}
	return out
}

// Now is overridable in tests.
var Now = time.Now
