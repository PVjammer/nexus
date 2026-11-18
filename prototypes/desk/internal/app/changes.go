package app

// "Since your last turn": you and the agents work on the same things, so
// each project and workstream agent starts its turn from what's true now.
// When its turn ends, Nexus notes the project's work (tasks, work items) and
// its folder (HEAD, branch, each changed file's status, size and time). The
// next turn gets a short note of what's different and who did it:
//
//   - a change through Nexus is attributed exactly, from the action log:
//     you, or the agent session that made it;
//   - a change in the project folder is "outside Nexus" (Nexus agents can't
//     write there): your editor, or another tool such as Claude Code. Commits
//     say who made them (author and Co-Authored-By).
//
// Nexus only reads: git runs with GIT_OPTIONAL_LOCKS=0, file contents are
// never read, and secret-looking files appear by name only.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type turnMark struct {
	at   time.Time
	work map[string]workFact
	repo *repoSnap
}

type workFact struct {
	kind, title, item, state, who string
}

type repoSnap struct {
	head, branch string
	files        map[string]string // path -> status code, size, mtime
}

func (a *App) markTurn(r *Run) {
	m := &turnMark{at: time.Now(), work: a.workFacts(r.Project)}
	if p, ok := a.Projects.Get(r.Project); ok {
		m.repo = repoSnapshot(p.Path)
	}
	r.mu.Lock()
	r.mark = m
	r.mu.Unlock()
}

func (a *App) workFacts(pid string) map[string]workFact {
	out := map[string]workFact{}
	snap := a.Desk.Snapshot()
	for _, t := range snap.WorkTasks {
		if t.Project == pid {
			out["t:"+t.ID] = workFact{kind: "task", title: t.Title, item: t.Item, state: t.Status, who: t.Assignee}
		}
	}
	for _, w := range a.workItems() {
		if w.Project == pid {
			out["i:"+w.Ref] = workFact{kind: "work item", title: w.Title, item: w.Ref, state: w.State}
		}
	}
	return out
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0") // read-only: never take the index lock
	b, err := cmd.Output()
	return string(b), err
}

func repoSnapshot(dir string) *repoSnap {
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return nil // not a git repo (or no commits yet): only Nexus changes are noted
	}
	br, _ := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	s := &repoSnap{head: strings.TrimSpace(head), branch: strings.TrimSpace(br), files: map[string]string{}}
	st, _ := git(dir, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	parts := strings.Split(st, "\x00")
	for i := 0; i < len(parts); i++ {
		e := parts[i]
		if len(e) < 4 {
			continue
		}
		code, path := e[:2], e[3:]
		if code[0] == 'R' || code[0] == 'C' {
			i++ // the next field is the old name
		}
		sig := code
		if fi, err := os.Lstat(filepath.Join(dir, path)); err == nil {
			sig += fmt.Sprintf(" %d %d", fi.Size(), fi.ModTime().UnixNano())
		}
		s.files[path] = sig
		if len(s.files) >= 2000 {
			break
		}
	}
	return s
}

// sinceLastTurn is the note for r's next turn; limit caps its lines (0 = all).
func (a *App) sinceLastTurn(r *Run, limit int) string {
	if r.Kind != KindProject && r.Kind != KindWorkstream {
		return ""
	}
	r.mu.Lock()
	m := r.mark
	r.mu.Unlock()
	if m == nil {
		return ""
	}
	type line struct {
		own  bool
		text string
	}
	var lines []line
	// who did it, from the log: newest entry per target since the mark
	who := map[string]string{}
	for _, e := range a.Log.Recent(400) {
		if e.TS.After(m.at) && e.Target != "" {
			who[e.Target] = e.Actor
		}
	}
	by := func(title string) string {
		w := who[truncate(title, 80)]
		switch {
		case w == "":
			return ""
		case w == r.label():
			return "you did this"
		case w == "you":
			return "the user"
		}
		return w
	}
	now := a.workFacts(r.Project)
	keys := map[string]bool{}
	for k := range now {
		keys[k] = true
	}
	for k := range m.work {
		keys[k] = true
	}
	var ks []string
	for k := range keys {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		o, had := m.work[k]
		n, has := now[k]
		var t string
		f := n
		switch {
		case !had:
			t = fmt.Sprintf("%s %q added (%s)", n.kind, n.title, n.state)
		case !has:
			f = o
			t = fmt.Sprintf("%s %q removed", o.kind, o.title)
		case o.state != n.state:
			t = fmt.Sprintf("%s %q: %s → %s", n.kind, n.title, o.state, n.state)
		case o.who != n.who:
			t = fmt.Sprintf("task %q now assigned to %s", n.title, n.who)
		case o.title != n.title:
			t = fmt.Sprintf("%s renamed %q → %q", n.kind, o.title, n.title)
		default:
			continue
		}
		w := by(f.title)
		if w == "you did this" {
			continue // its own change: it knows
		}
		if w != "" {
			t += " · by " + w
		}
		lines = append(lines, line{own: r.Item != "" && f.item == r.Item, text: t})
	}
	if p, ok := a.Projects.Get(r.Project); ok && m.repo != nil {
		for _, t := range repoDiff(p.Path, m.repo, repoSnapshot(p.Path)) {
			lines = append(lines, line{text: t})
		}
	}
	if len(lines) == 0 {
		return ""
	}
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].own && !lines[j].own })
	var b strings.Builder
	fmt.Fprintf(&b, "<since-your-last-turn ended=%q note=\"generated by the app\">\n", ago(m.at))
	for i, l := range lines {
		if limit > 0 && i == limit {
			fmt.Fprintf(&b, "- … %d more (project_changes lists everything)\n", len(lines)-i)
			break
		}
		b.WriteString("- " + l.text + "\n")
	}
	b.WriteString("</since-your-last-turn>")
	return b.String()
}

// repoDiff says what changed in the folder between two snapshots.
func repoDiff(dir string, old, cur *repoSnap) []string {
	if cur == nil {
		return nil
	}
	var out []string
	if old.branch != cur.branch {
		out = append(out, fmt.Sprintf("branch switched: %s → %s (outside Nexus)", old.branch, cur.branch))
	}
	if old.head != cur.head {
		log, err := git(dir, "log", "--format=%h%x1f%an%x1f%s%x1f%(trailers:key=Co-authored-by,valueonly,separator=%x2C )", "-n", "6", old.head+".."+cur.head)
		commits := strings.Split(strings.TrimSpace(log), "\n")
		if err != nil || log == "" {
			out = append(out, fmt.Sprintf("HEAD moved from %s to %s (a reset or rebase, outside Nexus)", short(old.head), short(cur.head)))
		} else {
			for i, c := range commits {
				if i == 5 {
					out = append(out, "… more commits")
					break
				}
				f := strings.Split(c, "\x1f")
				for len(f) < 4 {
					f = append(f, "")
				}
				with := ""
				if co := strings.TrimSpace(f[3]); co != "" {
					with = " with " + strings.TrimSuffix(co, ",")
				}
				out = append(out, fmt.Sprintf("commit %s %q by %s%s (outside Nexus)", f[0], f[2], f[1], with))
			}
		}
	}
	var added, edited, deleted, cleaned []string
	for p, sig := range cur.files {
		o, was := old.files[p]
		switch {
		case !was && strings.HasPrefix(sig, "??"):
			added = append(added, p)
		case !was && strings.Contains(sig[:2], "D"):
			deleted = append(deleted, p)
		case !was || o != sig:
			edited = append(edited, p)
		}
	}
	for p := range old.files {
		if _, still := cur.files[p]; !still && old.head == cur.head {
			cleaned = append(cleaned, p)
		}
	}
	files := func(verb string, ps []string) {
		if len(ps) == 0 {
			return
		}
		sort.Strings(ps)
		shown := ps
		if len(ps) > 6 {
			shown = ps[:6]
		}
		s := fmt.Sprintf("%d file(s) %s: %s", len(ps), verb, strings.Join(shown, ", "))
		if len(ps) > len(shown) {
			s += fmt.Sprintf(", … %d more", len(ps)-len(shown))
		}
		out = append(out, s+" (outside Nexus, not committed)")
	}
	files("edited", edited)
	files("added", added)
	files("deleted", deleted)
	if len(cleaned) > 0 {
		sort.Strings(cleaned)
		out = append(out, fmt.Sprintf("%d file(s) back to their committed version: %s (outside Nexus)", len(cleaned), strings.Join(cleaned[:min(6, len(cleaned))], ", ")))
	}
	return out
}

func short(h string) string { return h[:min(7, len(h))] }
