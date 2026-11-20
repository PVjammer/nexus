package app

// Response styles (docs/design/response-styles.md): how agents write, and
// how the chat shows it. A style is a file: where it applies, an instruction
// for the agent, and a render rule the shell knows (collapse: show the
// summary, click for the details; verdict: the first line as a badge).
// Shipped styles are in styles/; yours (style_write) are in <data>/styles
// and override them by name. A reply that ignores its style renders as
// usual: nothing is ever hidden by a broken format.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Verdict struct {
	Text string `json:"text"`
	Tone string `json:"tone"` // good | warn | bad
}

type Style struct {
	Name        string    `json:"name"`
	Title       string    `json:"title,omitempty"`
	Applies     []string  `json:"applies"` // home, project, workstream, task, customizer, personas, persona:<handle>, all
	Render      string    `json:"render"`  // collapse | verdict
	Verdicts    []Verdict `json:"verdicts,omitempty"`
	Enabled     bool      `json:"enabled"`
	Builtin     bool      `json:"builtin,omitempty"`
	Instruction string    `json:"-"`
}

var renderRules = map[string]bool{"collapse": true, "verdict": true}

func (a *App) stylesDir() string {
	if a.cfg.StylesDir != "" {
		return a.cfg.StylesDir
	}
	return "styles"
}

func (a *App) userStylesDir() string { return filepath.Join(a.cfg.DataDir, "styles") }

func (a *App) styles() []Style {
	by := map[string]Style{}
	for i, dir := range []string{a.stylesDir(), a.userStylesDir()} {
		fs, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		for _, f := range fs {
			if s, err := readStyle(f); err == nil {
				s.Builtin = i == 0
				by[s.Name] = s
			}
		}
	}
	out := make([]Style, 0, len(by))
	for _, s := range by {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func readStyle(path string) (Style, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Style{}, err
	}
	s := Style{Name: strings.TrimSuffix(filepath.Base(path), ".md"), Enabled: true}
	if !handleRe.MatchString(s.Name) {
		return s, fmt.Errorf("bad style name %q", s.Name)
	}
	text := string(b)
	if fm := frontmatter.FindString(text); fm != "" {
		for _, l := range strings.Split(strings.Trim(fm, "-\n"), "\n") {
			k, v, ok := strings.Cut(l, ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "title":
				s.Title = v
			case "applies":
				for _, x := range strings.Split(v, ",") {
					if x = strings.TrimSpace(x); x != "" {
						s.Applies = append(s.Applies, x)
					}
				}
			case "render":
				s.Render = v
			case "enabled":
				s.Enabled = v != "false" && v != "no"
			case "verdicts": // "Don't do this.=bad | This holds up.=good"
				for _, x := range strings.Split(v, "|") {
					t, tone, _ := strings.Cut(strings.TrimSpace(x), "=")
					if t = strings.TrimSpace(t); t != "" {
						s.Verdicts = append(s.Verdicts, Verdict{Text: t, Tone: strings.TrimSpace(tone)})
					}
				}
			}
		}
		text = text[len(fm):]
	}
	s.Instruction = strings.TrimSpace(text)
	if !renderRules[s.Render] {
		return s, fmt.Errorf("style %s: render %q isn't one the app knows (collapse, verdict)", s.Name, s.Render)
	}
	if s.Instruction == "" {
		return s, fmt.Errorf("style %s has no instruction", s.Name)
	}
	return s, nil
}

func (s Style) appliesTo(key string) bool {
	for _, x := range s.Applies {
		if x == key || x == "all" || (x == "personas" && strings.HasPrefix(key, "persona:")) {
			return true
		}
	}
	return false
}

// styleKey: what styles call this agent.
func styleKey(r *Run) string {
	switch r.Kind {
	case KindPersona:
		if r.persona != nil {
			return "persona:" + r.persona.Handle
		}
	case KindFork:
		return r.trunkKind
	}
	return r.Kind
}

// styleInstructions: the instructions of every style on for this agent.
func (a *App) styleInstructions(key string) string {
	var parts []string
	for _, s := range a.styles() {
		if s.Enabled && s.appliesTo(key) {
			parts = append(parts, s.Instruction)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n<reply-style note=\"how the user wants your replies written; the app renders them this way\">\n" + strings.Join(parts, "\n\n") + "\n</reply-style>"
}

// styleNote goes in the turn context, so a style switched on applies from
// the next message, without restarting the agent.
func (a *App) styleNote(r *Run) string {
	if r.Kind == KindPersona {
		return "" // in its instructions
	}
	return strings.TrimSpace(a.styleInstructions(styleKey(r)))
}

// styleTail repeats the reply style after the user's message: small models
// keep to what they read last, and drift from a note above it after a few
// tool calls.
func (a *App) styleTail(r *Run) string {
	var parts []string
	for _, s := range a.styles() {
		if s.Enabled && s.appliesTo(styleKey(r)) {
			parts = append(parts, s.Instruction)
		}
	}
	if len(parts) == 0 || r.Kind == KindPersona {
		return ""
	}
	return "(Reply style, as the user set it: " + strings.Join(parts, " ") + ")"
}

var replyStyleBlock = regexp.MustCompile(`(?s)\n?<reply-style note=.*?</reply-style>`)

// withoutStyle: a context without its reply style (a persona reading
// another agent's context keeps its own style).
func withoutStyle(ctx string) string { return replyStyleBlock.ReplaceAllString(ctx, "") }

var detailsBreak = regexp.MustCompile(`(?m)^\s*---\s*$`)

// summaryOf: a reply's summary (what comes before a "---" line, the
// collapse convention), else the reply cut to limit.
func summaryOf(text string, limit int) string {
	if loc := detailsBreak.FindStringIndex(text); loc != nil && loc[0] > 0 {
		return strings.TrimSpace(text[:loc[0]]) + " (details in the chat)"
	}
	return truncate(strings.TrimSpace(text), limit)
}

// ---- tools ----

func (a *App) styleTools() []Tool {
	H := []string{KindHome}
	return []Tool{
		{Name: "styles_list", Label: "List reply styles", Kinds: H,
			Description: "List the reply styles: how agents write, and how the chat shows it (e.g. tl;dr first, details collapsed). Shows which are on and where they apply.",
			Parameters:  obj(nil, map[string]any{}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				var b strings.Builder
				for _, s := range a.styles() {
					on := "off"
					if s.Enabled {
						on = "on"
					}
					fmt.Fprintf(&b, "%s (%s) · %s · render %s · applies to %s\n  %s\n", s.Name, on, s.Title, s.Render, strings.Join(s.Applies, ", "), oneLine(s.Instruction, 160))
				}
				if b.Len() == 0 {
					return "No styles yet.", nil
				}
				return b.String(), nil
			}},
		{Name: "style_write", Label: "Set a reply style", Kinds: H,
			Description: "Make, change, switch on or switch off a reply style. Follow the styles skill. Writing a shipped style's name makes your own version of it.",
			Parameters: obj([]string{"name", "applies", "render", "instruction"}, map[string]any{
				"name":        sprop("short lowercase name, e.g. tldr"),
				"title":       sprop("display name, e.g. tl;dr first"),
				"applies":     sprop("comma-separated: home, project, workstream, task, customizer, personas, persona:<handle>, or all"),
				"render":      sprop("collapse (summary shown, details on click: the reply needs a line with only --- between them) or verdict (the first line, from a fixed list, shown as a badge)"),
				"verdicts":    sprop("for render verdict: the allowed first lines with a tone, e.g. Don't do this.=bad | This holds up.=good"),
				"instruction": sprop("what the agent is told about writing its replies"),
				"enabled":     map[string]any{"type": "boolean", "description": "on (default true) or off"},
			}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				name := strings.ToLower(sArg(m, "name"))
				if !handleRe.MatchString(name) {
					return "", fixErr("name %q: use 2-24 lowercase letters, digits, - or _, starting with a letter", name)
				}
				if !renderRules[sArg(m, "render")] {
					return "", fixErr("render must be collapse or verdict")
				}
				var applies []string
				for _, x := range strings.Split(sArg(m, "applies"), ",") {
					x = strings.TrimSpace(strings.ToLower(x))
					switch {
					case x == "":
					case x == "home" || x == "project" || x == "workstream" || x == "task" || x == "customizer" || x == "personas" || x == "all":
						applies = append(applies, x)
					case strings.HasPrefix(x, "persona:") || strings.HasPrefix(x, "@"):
						h := strings.TrimPrefix(strings.TrimPrefix(x, "persona:"), "@")
						if _, ok := a.persona(h); !ok {
							return "", fixErr("no persona @%s (personas_list)", h)
						}
						applies = append(applies, "persona:"+h)
					default:
						return "", fixErr("applies: %q isn't home, project, workstream, task, customizer, personas, persona:<handle> or all", x)
					}
				}
				if len(applies) == 0 {
					return "", fixErr("applies: say where the style is on, e.g. project, workstream")
				}
				enabled := true
				if v, ok := m["enabled"].(bool); ok {
					enabled = v
				}
				var b strings.Builder
				b.WriteString("---\n")
				if t := oneLine(sArg(m, "title"), 60); t != "" {
					fmt.Fprintf(&b, "title: %s\n", t)
				}
				fmt.Fprintf(&b, "applies: %s\nrender: %s\nenabled: %v\n", strings.Join(applies, ", "), sArg(m, "render"), enabled)
				if v := oneLine(sArg(m, "verdicts"), 400); v != "" {
					fmt.Fprintf(&b, "verdicts: %s\n", v)
				}
				b.WriteString("---\n\n" + strings.TrimSpace(sArg(m, "instruction")) + "\n")
				if err := os.MkdirAll(a.userStylesDir(), 0o700); err != nil {
					return "", err
				}
				p := filepath.Join(a.userStylesDir(), name+".md")
				if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
					return "", err
				}
				if _, err := readStyle(p); err != nil {
					os.Remove(p)
					return "", fixErr("%v", err)
				}
				a.broadcastDesk()
				state := "on"
				if !enabled {
					state = "off"
				}
				return fmt.Sprintf("Style %s is %s for %s. It applies from the agents' next message.", name, state, strings.Join(applies, ", ")), nil
			}},
	}
}
