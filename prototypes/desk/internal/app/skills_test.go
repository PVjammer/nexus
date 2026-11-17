package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var jsonBlock = regexp.MustCompile("(?s)```json\n(.*?)```")

// exampleAgent: which agent a skill's examples run as (default home).
var exampleAgent = map[string]string{"onboarding": "p:alpha"}

// TestSkillExamples runs every example tool call in every skill against the
// real tools, so a skill can't teach a call the code no longer accepts.
// Each example block starts from the dashboard preset.
func TestSkillExamples(t *testing.T) {
	dirs, _ := filepath.Glob("../../skills/*/SKILL.md")
	if len(dirs) == 0 {
		t.Fatal("no skills found")
	}
	for _, path := range dirs {
		b, _ := os.ReadFile(path)
		name := filepath.Base(filepath.Dir(path))
		blocks := jsonBlock.FindAllStringSubmatch(string(b), -1)
		if len(blocks) == 0 {
			t.Fatalf("%s has no examples", name)
		}
		for i, blk := range blocks {
			var calls []struct {
				Tool string         `json:"tool"`
				Args map[string]any `json:"args"`
			}
			if err := json.Unmarshal([]byte(blk[1]), &calls); err != nil {
				t.Fatalf("%s example %d isn't a JSON list of calls: %v", name, i+1, err)
			}
			a, _ := newTestApp(t)
			if err := a.homeReset("you", "dashboard", ""); err != nil {
				t.Fatal(err)
			}
			// Examples name a work item by title; it exists in the fixture.
			if _, err := a.workItemAdd("you", "alpha", "Add CSV export", "", true); err != nil {
				t.Fatal(err)
			}
			agent := exampleAgent[name]
			if agent == "" {
				agent = "home"
			}
			for _, c := range calls {
				if _, err := call(t, a, agent, c.Tool, c.Args); err != nil {
					t.Fatalf("%s example %d: %s %v failed: %v", name, i+1, c.Tool, c.Args, err)
				}
			}
		}
	}
}

func TestSkillsRenderWithTheCatalog(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.SkillsDir = "../../skills"
	s := a.instructionsFor(KindHome)
	if !strings.Contains(s, `<skill name="home-layout">`) || strings.Contains(s, "{{") || strings.Contains(s, "unavailable") {
		t.Fatalf("home skill didn't render:\n%s", s)
	}
	for _, k := range kindNames(false) {
		if !strings.Contains(s, "`"+k+"`") {
			t.Fatalf("catalog in the skill is missing %s", k)
		}
	}
	if strings.Contains(s, "name: home-layout") {
		t.Fatal("frontmatter should be stripped")
	}
	if a.instructionsFor(KindTask) != "" {
		t.Fatal("task agents don't get the home skill")
	}
}
