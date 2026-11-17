// Package scan looks through a project folder for signs of work: TODO
// comments, unchecked checklist items, plan sections in markdown, design
// docs, unmerged branches and uncommitted changes. It's plain code with no
// model, so its findings are the same every time and each one says where it
// came from. An agent then groups the findings and proposes work items.
//
// It only reads. Nested git repositories (submodules, worktrees, vendored
// checkouts) are skipped: they're their own projects.
package scan

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Finding is one sign of work.
type Finding struct {
	Kind    string    `json:"kind"` // todo | checklist | plan | doc | branch | changes
	Text    string    `json:"text"`
	Where   string    `json:"where"`             // "src/x.go:12", "docs/plan.md:4", "branch feat/y"
	Section string    `json:"section,omitempty"` // plan: the heading it's under
	When    time.Time `json:"when,omitempty"`
}

// Result is everything the scan found, by kind.
type Result struct {
	Root     string    `json:"root"`
	Files    int       `json:"files"` // files looked at
	Todos    []Finding `json:"todos,omitempty"`
	Checks   []Finding `json:"checklist,omitempty"`
	Plans    []Finding `json:"plans,omitempty"`
	Docs     []Finding `json:"docs,omitempty"`
	Branches []Finding `json:"branches,omitempty"`
	Changes  *Finding  `json:"changes,omitempty"`
	Capped   bool      `json:"capped,omitempty"` // stopped early: a very big folder
}

// Limits keep a scan quick on big folders.
const (
	maxFiles    = 20000
	maxFileSize = 512 << 10
	maxDepth    = 10
	maxTodos    = 60
	maxChecks   = 80
	maxPlans    = 60
	maxDocs     = 40
	// one long notes file shouldn't crowd out the rest
	maxPlansPerFile = 12
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	".venv": true, "venv": true, "env": true, "__pycache__": true, ".next": true, ".cache": true,
	".idea": true, ".vscode": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true, "coverage": true,
	"third_party": true, ".terraform": true, "bin": true, "obj": true, ".gradle": true, "testdata": true, "fixtures": true,
}

var codeExt = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".rs": true,
	".java": true, ".kt": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true, ".cs": true,
	".rb": true, ".php": true, ".swift": true, ".scala": true, ".sh": true, ".bash": true, ".zsh": true,
	".lua": true, ".sql": true, ".yaml": true, ".yml": true, ".toml": true, ".css": true, ".scss": true,
	".html": true, ".vue": true, ".svelte": true, ".ex": true, ".exs": true, ".zig": true, ".nix": true, ".tf": true,
}

var (
	todoRe    = regexp.MustCompile(`(?:^|[^A-Za-z])(TODO|FIXME|HACK|XXX)\b[:(\s]?(.*)`)
	commentRe = regexp.MustCompile(`(//|#|/\*|\*|--|<!--|;)`)
	checkRe   = regexp.MustCompile(`^\s*[-*+]\s+\[ \]\s+(.+)`)
	bulletRe  = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?(.+)`)
	doneRe    = regexp.MustCompile(`^\s*[-*+]\s+\[[xX]\]`)
	headRe    = regexp.MustCompile(`^(#{1,4})\s+(.+?)\s*#*\s*$`)
	planHead  = regexp.MustCompile(`(?i)\b(todo|to-do|to do|roadmap|next steps?|ideas?|backlog|future( work)?|planned|plans?|open questions?|follow[- ]?ups?|wishlist|known issues|unreleased|not yet|coming soon|milestones?)\b`)
	docName   = regexp.MustCompile(`(?i)(design|spec|plan|rfc|adr|proposal|architecture|roadmap|todo|backlog|notes|ideas)`)
)

// Project scans one project folder.
func Project(root string) Result {
	r := Result{Root: root}
	root = filepath.Clean(root)
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			if p == root {
				return nil
			}
			name := d.Name()
			if skipDirs[name] || strings.Count(rel, string(filepath.Separator)) >= maxDepth {
				return filepath.SkipDir
			}
			// A nested git repo or worktree is its own project.
			if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if r.Files >= maxFiles {
			r.Capped = true
			return filepath.SkipAll
		}
		ext := strings.ToLower(filepath.Ext(p))
		isMD := ext == ".md" || ext == ".markdown" || ext == ".txt" && docName.MatchString(d.Name())
		if !isMD && !codeExt[ext] {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".env") || strings.Contains(strings.ToLower(d.Name()), "secret") {
			return nil // never read secret-looking files
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxFileSize {
			return nil
		}
		r.Files++
		b, err := os.ReadFile(p)
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isMD {
			r.markdown(rel, b, info.ModTime())
		} else {
			r.code(rel, b)
		}
		return nil
	})
	r.git(root)
	sort.SliceStable(r.Docs, func(i, j int) bool { return r.Docs[i].When.After(r.Docs[j].When) })
	if len(r.Docs) > maxDocs {
		r.Docs = r.Docs[:maxDocs]
	}
	return r
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(strings.TrimSpace(s), "*/->")
	if len(s) > n {
		s = s[:n] + "…"
	}
	return strings.TrimSpace(s)
}

func (r *Result) code(rel string, b []byte) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		m := todoRe.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		// Only in comments: the marker must come after a comment opener.
		if c := commentRe.FindStringIndex(line); c == nil || c[0] > m[2] {
			continue
		}
		if len(r.Todos) >= maxTodos {
			r.Capped = true
			return
		}
		text := clip(line[m[4]:m[5]], 160)
		if text == "" {
			text = "(no description)"
		}
		r.Todos = append(r.Todos, Finding{Kind: "todo", Text: line[m[2]:m[3]] + ": " + text, Where: rel + ":" + itoa(n)})
	}
}

func (r *Result) markdown(rel string, b []byte, mod time.Time) {
	lines := strings.Split(string(b), "\n")
	title, section, inPlan, inFence := "", "", false, false
	planLevel, plans := 0, 0
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		where := rel + ":" + itoa(i+1)
		if m := headRe.FindStringSubmatch(line); m != nil {
			level := len(m[1])
			if title == "" {
				title = m[2]
			}
			if inPlan && level > planLevel {
				continue // a sub-heading inside a plan section
			}
			section = m[2]
			inPlan, planLevel = planHead.MatchString(m[2]), level
			continue
		}
		if m := checkRe.FindStringSubmatch(line); m != nil {
			if len(r.Checks) < maxChecks {
				r.Checks = append(r.Checks, Finding{Kind: "checklist", Text: clip(m[1], 200), Where: where, Section: section})
			} else {
				r.Capped = true
			}
			continue
		}
		if inPlan && !doneRe.MatchString(line) {
			if m := bulletRe.FindStringSubmatch(line); m != nil && !strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "\t") {
				if strings.Contains(m[1], "~~") {
					continue
				}
				if plans++; plans > maxPlansPerFile {
					continue
				}
				if len(r.Plans) < maxPlans {
					r.Plans = append(r.Plans, Finding{Kind: "plan", Text: clip(m[1], 200), Where: where, Section: section})
				} else {
					r.Capped = true
				}
			}
		}
	}
	base := strings.ToLower(filepath.Base(rel))
	inDocs := strings.HasPrefix(rel, "docs/") || strings.Contains(rel, "/docs/") || strings.Contains(rel, "design/")
	if base != "readme.md" && base != "changelog.md" && base != "license.md" && (inDocs || docName.MatchString(base)) {
		if title == "" {
			title = filepath.Base(rel)
		}
		r.Docs = append(r.Docs, Finding{Kind: "doc", Text: clip(title, 120), Where: rel, When: mod})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func git(dir string, args ...string) string {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0") // read-only: don't touch the index
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// git: branches not merged into the main branch, and uncommitted changes.
func (r *Result) git(root string) {
	if git(root, "rev-parse", "--is-inside-work-tree") != "true" {
		return
	}
	main := ""
	for _, b := range []string{"main", "master", "trunk", "develop"} {
		if git(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+b) != "" {
			main = b
			break
		}
	}
	if main != "" {
		out := git(root, "for-each-ref", "--no-merged="+main, "--sort=-committerdate", "--format=%(refname:short)\t%(committerdate:iso-strict)\t%(subject)", "refs/heads")
		for _, line := range strings.Split(out, "\n") {
			f := strings.SplitN(line, "\t", 3)
			if len(f) < 3 {
				continue
			}
			when, _ := time.Parse(time.RFC3339, f[1])
			ahead := git(root, "rev-list", "--count", main+".."+f[0])
			r.Branches = append(r.Branches, Finding{Kind: "branch", Text: f[0] + ": " + clip(f[2], 120) + " (" + ahead + " commits not on " + main + ")", Where: "branch " + f[0], When: when})
			if len(r.Branches) >= 15 {
				break
			}
		}
	}
	if st := git(root, "status", "--porcelain"); st != "" {
		files := strings.Split(st, "\n")
		names := []string{}
		for i, f := range files {
			if i == 5 {
				names = append(names, "…")
				break
			}
			names = append(names, strings.TrimSpace(f[min(3, len(f)):]))
		}
		r.Changes = &Finding{Kind: "changes", Text: itoa(len(files)) + " uncommitted changes: " + strings.Join(names, ", "), Where: "working tree (" + git(root, "branch", "--show-current") + ")"}
	}
}
