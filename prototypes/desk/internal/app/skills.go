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
	KindHome: {"home-layout"},
}

// skillVars fills {{name}} placeholders with text generated from the code.
func skillVars() map[string]string {
	return map[string]string{"catalog": strings.TrimSpace(homeCatalog())}
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

// instructionsFor is the skills text appended to an agent's instructions.
func (a *App) instructionsFor(kind string) string {
	var parts []string
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
	return "# Skills\n\nHow to use some of your tools. Follow them.\n\n" + strings.Join(parts, "\n\n")
}
