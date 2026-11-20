package app

// Personas: voices you call into any chat with @handle ("@critic, what do
// you think of this plan?"). A persona is a file: a handle, a name, a colour,
// an optional model, and a brief that says how it reads things, so you never
// type "be brutally honest" again.
//
// A persona answers once, from the chat so far and what the app knows, with
// read-only tools, and keeps no memory between calls. It never sees the
// chat agent's own reasoning, only what was said, so its read is
// independent. Its reply lands in the chat it was called in, under its name.
// The chat's own agent doesn't answer that message; it sees the exchange at
// the start of its next turn (the <aside> note).
//
// Shipped personas are in personas/ next to skills/; yours (persona_write,
// or a file you drop in) are in <data>/personas/ and override shipped ones
// with the same handle. A persona called in a private project runs inside
// its wall, on the project's own model: one set to another model refuses.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const KindPersona = "persona" // a one-off voice called with @handle

type Persona struct {
	Handle  string `json:"handle"`
	Name    string `json:"name"`
	About   string `json:"about,omitempty"` // one line, for the @ menu
	Color   string `json:"color,omitempty"`
	Model   string `json:"model,omitempty"` // "" = the chat's project-agent model
	Brief   string `json:"-"`
	Builtin bool   `json:"builtin,omitempty"`
}

var handleRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,23}$`)

func (a *App) personasDir() string {
	if a.cfg.PersonasDir != "" {
		return a.cfg.PersonasDir
	}
	return "personas"
}

func (a *App) userPersonasDir() string { return filepath.Join(a.cfg.DataDir, "personas") }

// personas: shipped ones, then yours over them, by handle.
func (a *App) personas() []Persona {
	by := map[string]Persona{}
	for i, dir := range []string{a.personasDir(), a.userPersonasDir()} {
		fs, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, f := range fs {
			p, err := readPersona(f)
			if err != nil {
				continue
			}
			p.Builtin = i == 0
			by[p.Handle] = p
		}
	}
	out := make([]Persona, 0, len(by))
	for _, p := range by {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}

func (a *App) persona(handle string) (Persona, bool) {
	for _, p := range a.personas() {
		if p.Handle == strings.ToLower(handle) {
			return p, true
		}
	}
	return Persona{}, false
}

// readPersona parses personas/<handle>.md: "key: value" frontmatter, then the brief.
func readPersona(path string) (Persona, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, err
	}
	p := Persona{Handle: strings.TrimSuffix(filepath.Base(path), ".md")}
	if !handleRe.MatchString(p.Handle) {
		return p, fmt.Errorf("bad handle %q", p.Handle)
	}
	s := string(b)
	if fm := frontmatter.FindString(s); fm != "" {
		for _, l := range strings.Split(strings.Trim(fm, "-\n"), "\n") {
			k, v, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			switch strings.TrimSpace(k) {
			case "name":
				p.Name = v
			case "about":
				p.About = v
			case "color":
				p.Color = v
			case "model":
				p.Model = v
			}
		}
		s = s[len(fm):]
	}
	p.Brief = strings.TrimSpace(s)
	if p.Name == "" {
		p.Name = p.Handle
	}
	if p.Brief == "" {
		return p, fmt.Errorf("persona %s has no brief", p.Handle)
	}
	return p, nil
}

var mentionRe = regexp.MustCompile(`(?:^|[^\w@./])@([A-Za-z][\w-]*)`)

// mentioned: the personas a message calls on, in order, once each.
func (a *App) mentioned(text string) []Persona {
	var out []Persona
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		h := strings.ToLower(m[1])
		if seen[h] {
			continue
		}
		if p, ok := a.persona(h); ok {
			seen[h] = true
			out = append(out, p)
		}
	}
	return out
}

// ---- in a chat ----

// personaAnswer runs the persona once and returns what it said.
func (a *App) personaAnswer(owner *Run, p Persona, text, selection, view string) (string, error) {
	if a.cfg.Agents == nil {
		return "", fmt.Errorf("@%s needs an agent loop (this core has none)", p.Handle)
	}
	if p.Model != "" && a.walled("", owner.Project) {
		return "", fmt.Errorf("@%s runs on its own model (%s), and this project is private, so its agents run only on the project's own model. Clear its model to use it here", p.Handle, p.Model)
	}
	r, err := a.run("persona:" + p.Handle + ":" + newToken()[:8])
	if err != nil {
		return "", err
	}
	r.Project, r.persona = owner.Project, &p
	defer a.forgetRun(r)
	msgs, _ := owner.history()
	var said []ChatMsg // what was said, not tool calls or reasoning
	for _, m := range msgs {
		if (m.Role == "you" || m.Role == "agent" || m.Role == "persona") && strings.TrimSpace(m.Text) != "" {
			if m.Role == "persona" {
				m.Text = "[@" + m.Author + "] " + m.Text
			}
			said = append(said, m)
		}
	}
	if n := len(said); n > 0 && said[n-1].Role == "you" && said[n-1].Text == text {
		said = said[:n-1] // the call itself goes last, on its own
	}
	chat := "(nothing yet: the user's message to you is the whole thing)"
	if len(said) > 0 {
		chat = transcriptText(said, 40000)
	}
	prompt := fmt.Sprintf("<what-the-app-knows note=\"background\">\n%s\n</what-the-app-knows>\n\n<chat with=%q note=\"the conversation you were called into; 'agent' is the %s, not you\">\n%s\n</chat>\n\nThe user's message to you, @%s. Answer it; what to judge is in it or in the chat above:\n\n%s",
		withoutStyle(a.contextFor(owner, selection, view)), owner.label(), owner.label(), chat, p.Handle, text)
	done := r.turnWaiter()
	if err := r.sayAs("app", prompt, ""); err != nil {
		return "", err
	}
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("@%s took too long", p.Handle)
	}
	ans := r.finalText()
	if ans == "" {
		if e := r.lastError(); e != "" {
			return "", fmt.Errorf("@%s couldn't answer: %s", p.Handle, truncate(e, 300))
		}
		return "", fmt.Errorf("@%s had nothing to say", p.Handle)
	}
	return a.scrub(owner.Project, ans), nil
}

// finalText is a one-off run's answer: what it said after its last tool
// call. Some local models put a whole final answer in their reasoning and
// say nothing, which would leave only a "let me check" preamble; then the
// reasoning is the answer.
func (r *Run) finalText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var said, thought string
	for i := len(r.chat) - 1; i >= 0; i-- {
		m := r.chat[i]
		if m.Role == "tool" || m.Role == "app" || m.Role == "you" {
			break
		}
		if m.Role == "agent" && said == "" && strings.TrimSpace(m.Text) != "" {
			said = m.Text
		}
		if m.Role == "thinking" && thought == "" {
			thought = m.Text
		}
	}
	switch {
	case said != "":
		return strings.TrimSpace(said)
	case thought != "":
		return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(thought), "."))
	}
	for i := len(r.chat) - 1; i >= 0; i-- {
		if r.chat[i].Role == "agent" {
			return strings.TrimSpace(r.chat[i].Text)
		}
	}
	return ""
}

func (r *Run) lastError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.chat) - 1; i >= 0; i-- {
		if r.chat[i].Role == "system" && r.chat[i].Error {
			return r.chat[i].Text
		}
	}
	return ""
}

// personaInstructions: the persona's brief, under the shared persona prompt.
func personaInstructions(p *Persona) string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("# You are @%s (%s)\n\n%s", p.Handle, p.Name, p.Brief)
}

func (a *App) instructionsForRun(r *Run) string {
	switch r.Kind {
	case KindPersona:
		return personaInstructions(r.persona) + a.styleInstructions("persona:"+r.persona.Handle)
	case KindFork: // a side session is the agent itself
		return a.instructionsFor(r.trunkKind)
	}
	return a.instructionsFor(r.Kind)
}

// base: a persona reads and runs as the chat it was called into: home's
// in home (and the quick window), a project agent's in a project.
func (r *Run) base() string {
	switch {
	case r.Kind == KindFork && r.trunkKind == KindHome:
		return KindHome
	case r.Kind == KindFork:
		return KindProject
	case r.Kind != KindPersona:
		return ""
	}
	if r.Project == "" {
		return KindHome
	}
	return KindProject
}

func (r *Run) personaModel() string {
	if r.persona == nil {
		return ""
	}
	return r.persona.Model
}

// personaTools: what a persona may read. Nothing that changes anything.
var personaTools = map[string]bool{
	"files_list": true, "file_read": true, "search": true,
	"wiki_list": true, "wiki_read": true, "wiki_search": true,
	"work_items": true, "work_tasks": true, "projects_list": true, "project_get": true, "project_brief": true,
	"code_explore": true, "code_symbols": true, "code_symbol": true, "code_callers": true, "code_impact": true,
	"sheet_read": true, "review_read": true, "ws_transcript": true,
}

// ---- the aside: what the chat's agent missed ----

func (r *Run) addAside(m ChatMsg) {
	r.mu.Lock()
	r.aside = append(r.aside, m)
	r.mu.Unlock()
}

// takeAside is the exchange the agent wasn't part of, for its next turn.
func (r *Run) takeAside() string {
	r.mu.Lock()
	as := r.aside
	r.aside = nil
	r.mu.Unlock()
	if len(as) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<aside note=\"said in this chat, or asked of you, since your last turn; you didn't answer these here, but you can build on them\">\n")
	for _, m := range as {
		switch m.Role {
		case "persona": // a summary: the details are in the chat
			fmt.Fprintf(&b, "[@%s] %s\n", m.Author, summaryOf(m.Text, 1500))
		case "merge":
			fmt.Fprintf(&b, "[side session] %s\n", m.Text)
		default:
			fmt.Fprintf(&b, "[user] %s\n", truncate(m.Text, 4000))
		}
	}
	b.WriteString("</aside>")
	return b.String()
}

// updateMsg replaces a message in the transcript (by id) and tells the shell.
func (r *Run) updateMsg(m ChatMsg) {
	r.mu.Lock()
	for i := range r.chat {
		if r.chat[i].ID == m.ID {
			r.chat[i] = m
		}
	}
	r.mu.Unlock()
	r.a.broadcast(map[string]any{"t": "chatUpdate", "agent": r.ID, "msg": m})
	r.persistSoon()
}

// ---- tools: personas you make by asking ----

func (a *App) personaToolset() []Tool {
	H := []string{KindHome}
	return []Tool{
		{Name: "personas_list", Label: "List personas", Kinds: H,
			Description: "List the personas the user can call into any chat with @handle, with what each is for.",
			Parameters:  obj(nil, map[string]any{}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				var b strings.Builder
				for _, p := range a.personas() {
					src := "yours"
					if p.Builtin {
						src = "shipped"
					}
					fmt.Fprintf(&b, "@%s · %s · %s (%s", p.Handle, p.Name, p.About, src)
					if p.Model != "" {
						b.WriteString(", model " + p.Model)
					}
					b.WriteString(")\n")
				}
				if b.Len() == 0 {
					return "No personas yet.", nil
				}
				return b.String(), nil
			}},
		{Name: "persona_write", Label: "Make a persona", Kinds: H,
			Description: "Create or change a persona: a voice the user calls into any chat with @handle. Follow the personas skill. Writing a shipped persona's handle makes the user's own version of it.",
			Parameters: obj([]string{"handle", "name", "about", "brief"}, map[string]any{
				"handle": sprop("lowercase, 2-24 chars, letters, digits, - or _: what the user types after @"),
				"name":   sprop("display name, e.g. The Critic"),
				"about":  sprop("one line: what it's for, shown in the @ menu"),
				"brief":  sprop("how it reads things and what it checks, in markdown (see the personas skill)"),
				"color":  sprop("optional CSS colour for its name, e.g. #c0392b"),
				"model":  sprop("optional model, e.g. spark/Qwen3.8-27B:high; leave out for the default"),
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				h := strings.ToLower(strings.TrimPrefix(sArg(m, "handle"), "@"))
				if !handleRe.MatchString(h) {
					return "", fixErr("handle %q: use 2-24 lowercase letters, digits, - or _, starting with a letter", h)
				}
				if strings.TrimSpace(sArg(m, "brief")) == "" {
					return "", fixErr("a persona needs a brief: how it reads things and what it checks")
				}
				var b strings.Builder
				b.WriteString("---\n")
				for _, k := range []string{"name", "about", "color", "model"} {
					if v := strings.TrimSpace(strings.ReplaceAll(sArg(m, k), "\n", " ")); v != "" {
						fmt.Fprintf(&b, "%s: %s\n", k, v)
					}
				}
				b.WriteString("---\n\n" + strings.TrimSpace(sArg(m, "brief")) + "\n")
				if err := os.MkdirAll(a.userPersonasDir(), 0o700); err != nil {
					return "", err
				}
				_, existed := a.persona(h)
				if err := os.WriteFile(filepath.Join(a.userPersonasDir(), h+".md"), []byte(b.String()), 0o600); err != nil {
					return "", err
				}
				a.broadcastDesk()
				if existed {
					return fmt.Sprintf("Updated @%s. The user can call it in any chat now.", h), nil
				}
				return fmt.Sprintf("Made @%s. The user can call it in any chat by typing @%s.", h, h), nil
			}},
	}
}
