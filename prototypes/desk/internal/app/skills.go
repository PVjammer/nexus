package app

// Skills are short instruction files (agentskills.io SKILL.md format) that
// teach an agent one capability. Each capability that agents use to change
// Nexus ships with its skill. Parts that must match the code (the widget
// catalog) are generated into the text when an agent starts, so the skill
// can't drift from the tools; TestSkillExamples runs every example in every
// skill against the real tools.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skillsByKind says which skills each agent gets.
var skillsByKind = map[string][]string{
	KindHome:       {"home-layout", "tasks", "personas", "styles", "mentions"},
	KindProject:    {"onboarding", "tasks", "workstreams", "mentions"},
	KindWorkstream: {"onboarding", "tasks", "mentions"},
	KindCustomizer: {"home-layout"},
}

// skillVars fills {{name}} placeholders with text generated from the code.
func skillVars() map[string]string {
	return map[string]string{"catalog": strings.TrimSpace(homeCatalog()), "statuses": strings.Join(taskStatuses, ", ")}
}

var frontmatter = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)

// renderSkill reads a skill and fills its placeholders.
func (a *App) renderSkill(name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(a.skillsDir(), name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	s := string(b)
	for k, v := range skillVars() {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	if i := strings.Index(s, "{{"); i >= 0 {
		return "", fmt.Errorf("skill %s: unknown placeholder near %q", name, s[i:min(len(s), i+30)])
	}
	return s, nil
}

func (a *App) skillsDir() string {
	if a.cfg.SkillsDir != "" {
		return a.cfg.SkillsDir
	}
	return "skills"
}

// workstreamPreamble turns the project agent's prompt into a workstream
// agent's (it has the same tools).
const workstreamPreamble = `# You are a workstream agent

Everything below about project agents applies to you, with one difference: **you work on one work item**, in its own conversation with the user. Your turn context has a <workstream> block with it: the brief, what done looks like, its tasks. Keep to it. Other work in the project belongs to other workstreams; the project agent (the orchestrator) may message you about them, and you answer it in your reply.

- **Tasks are your plan.** Work through the work item's tasks; add the next concrete ones with work_task_add, and move them on the board as they happen.
- **A task the user hands you** (assignee agent): do it, then move it to review with a note saying what you did and where to look. The user marks it done.
- **When every done-when check is met,** say so with work_item_set state done and evidence, one line per check. Only the user can say it's done.
- **Since your last turn:** when the user or other agents changed things while you weren't looking, a <since-your-last-turn> note says what. Start from it.`

// instructionsFor is the skills text appended to an agent's instructions.
func (a *App) instructionsFor(kind string) string {
	var parts []string
	if kind == KindWorkstream {
		parts = append(parts, workstreamPreamble)
	}
	for _, name := range skillsByKind[kind] {
		s, err := a.renderSkill(name)
		if err != nil {
			parts = append(parts, fmt.Sprintf("(skill %s is unavailable: %v)", name, err))
			continue
		}
		parts = append(parts, "<skill name=\""+name+"\">\n"+strings.TrimSpace(frontmatter.ReplaceAllString(s, ""))+"\n</skill>")
	}
	if len(parts) == 0 {
		return ""
	}
	if kind == KindWorkstream {
		return parts[0] + "\n\n# Skills\n\nHow to use some of your tools. Follow them.\n\n" + strings.Join(parts[1:], "\n\n")
	}
	return "# Skills\n\nHow to use some of your tools. Follow them.\n\n" + strings.Join(parts, "\n\n")
}
