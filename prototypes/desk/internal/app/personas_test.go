package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/agent"
)

// fakeLoops is an agent factory whose agents answer every prompt with
// reply(spec, prompt) in one turn. It records what each agent was started
// with and sent.
type fakeLoops struct {
	mu      sync.Mutex
	specs   map[string]RunSpec
	prompts map[string][]string
	reply   func(s RunSpec, prompt string) string
}

func (f *fakeLoops) Name() string { return "fake" }
func (f *fakeLoops) Start(_ context.Context, s RunSpec) (agent.Backend, error) {
	f.mu.Lock()
	f.specs[s.ID] = s
	f.mu.Unlock()
	return &fakeBackend{f: f, spec: s, ev: make(chan agent.Event, 16)}, nil
}

func (f *fakeLoops) seen(id string) (RunSpec, []string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.specs[id]
	return s, append([]string(nil), f.prompts[id]...), ok
}

// started: the one agent started with this id prefix.
func (f *fakeLoops) started(prefix string) (RunSpec, []string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.specs {
		if strings.HasPrefix(id, prefix) {
			return s, append([]string(nil), f.prompts[id]...), true
		}
	}
	return RunSpec{}, nil, false
}

type fakeBackend struct {
	f    *fakeLoops
	spec RunSpec
	ev   chan agent.Event
}

func (b *fakeBackend) Start(context.Context) error { return nil }
func (b *fakeBackend) Prompt(_ context.Context, msg string) error {
	b.f.mu.Lock()
	b.f.prompts[b.spec.ID] = append(b.f.prompts[b.spec.ID], msg)
	b.f.mu.Unlock()
	go func() {
		b.ev <- agent.Event{Type: agent.EventDelta, Text: b.f.reply(b.spec, msg)}
		b.ev <- agent.Event{Type: agent.EventTurnEnd}
	}()
	return nil
}
func (b *fakeBackend) Abort() error               { return nil }
func (b *fakeBackend) Events() <-chan agent.Event { return b.ev }
func (b *fakeBackend) Close() error               { return nil }
func (b *fakeBackend) Name() string               { return "fake" }

func withFakeLoops(t *testing.T, a *App) *fakeLoops {
	f := &fakeLoops{specs: map[string]RunSpec{}, prompts: map[string][]string{}, reply: func(s RunSpec, p string) string {
		if s.Kind == KindPersona {
			return "Verdict from " + s.ID[:strings.LastIndex(s.ID, ":")]
		}
		return "ok"
	}}
	a.cfg.Agents = f
	a.cfg.PersonasDir = "../../personas"
	a.cfg.SkillsDir = "../../skills"
	return f
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestPersonasLoadAndMentions(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	ps := a.personas()
	if len(ps) < 2 || ps[0].Handle != "critic" || ps[1].Handle != "pessimist" || !ps[0].Builtin || ps[0].Color == "" || !strings.Contains(ps[0].Brief, "not a contrarian") {
		t.Fatalf("shipped personas: %+v", ps)
	}
	// yours override shipped ones by handle
	os.MkdirAll(a.userPersonasDir(), 0o700)
	os.WriteFile(filepath.Join(a.userPersonasDir(), "critic.md"), []byte("---\nname: My Critic\n---\nbe mean\n"), 0o600)
	if p, _ := a.persona("critic"); p.Name != "My Critic" || p.Builtin {
		t.Fatalf("override: %+v", p)
	}
	for text, want := range map[string]string{
		"@critic what do you think?":         "critic",
		"hey @Critic and @pessimist @critic": "critic pessimist",
		"mail me at nick@critic.com":         "",
		"@nobody thoughts?":                  "",
		"see notes/@critic.md":               "",
		"(@pessimist) run the pre-mortem":    "pessimist",
	} {
		var got []string
		for _, p := range a.mentioned(text) {
			got = append(got, p.Handle)
		}
		if strings.Join(got, " ") != want {
			t.Errorf("%q: got %v, want %q", text, got, want)
		}
	}
}

// TestPersonaInChat: "@critic ..." goes to the critic, not the chat's agent;
// the critic sees the chat so far; its answer lands in the chat under its
// name; and the chat's agent hears about it on its next turn.
func TestPersonaInChat(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	r := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "Plan: ship the CSV export on Friday with no tests", "", "")
	waitFor(t, "the project agent's reply", func() bool { return r.lastAgentText() == "ok" })

	a.Chat("p:alpha", "@critic, what's wrong with this plan?", "", "")
	waitFor(t, "the critic's answer", func() bool {
		msgs, _ := r.history()
		m := msgs[len(msgs)-1]
		return m.Role == "persona" && m.Done
	})
	msgs, _ := r.history()
	m := msgs[len(msgs)-1]
	if m.Author != "critic" || m.Text != "Verdict from persona:critic" || m.Error {
		t.Fatalf("the critic's message: %+v", m)
	}
	if _, ps, _ := f.seen("p:alpha"); len(ps) != 1 {
		t.Fatalf("the project agent mustn't get the @ message: %q", ps)
	}
	spec, ps, ok := f.started("persona:critic:")
	if !ok || spec.Kind != KindPersona || !strings.Contains(spec.Instructions, "You are @critic") || !strings.Contains(spec.Instructions, "not a contrarian") {
		t.Fatalf("the critic's start: %+v", spec)
	}
	if len(ps) != 1 || !strings.Contains(ps[0], "no tests") || !strings.HasSuffix(ps[0], "@critic, what's wrong with this plan?") || !strings.Contains(ps[0], "<what-the-app-knows") {
		t.Fatalf("the critic's prompt has the chat and the call last:\n%s", ps)
	}
	if a.existingRun(spec.ID) != nil {
		t.Fatal("a persona is discarded after it answers")
	}
	if _, err := os.Stat(a.chatPath(spec.ID)); err == nil {
		t.Fatal("nor is its own transcript kept")
	}

	a.Chat("p:alpha", "ok, fix the worst one", "", "")
	waitFor(t, "the project agent's turn", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 2 })
	_, ps, _ = f.seen("p:alpha")
	if !strings.Contains(ps[1], "<aside") || !strings.Contains(ps[1], "[@critic] Verdict from persona:critic") || !strings.Contains(ps[1], "[user] @critic, what's wrong") {
		t.Fatalf("the project agent hears what it missed:\n%s", ps[1])
	}
	a.Chat("p:alpha", "and?", "", "")
	waitFor(t, "the third turn", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 3 })
	if _, ps, _ = f.seen("p:alpha"); strings.Contains(ps[2], "<aside") {
		t.Fatal("only once")
	}
}

// TestFinalText: a local model that answers only in its reasoning after its
// tool calls still gets heard, not its "let me check" preamble.
func TestFinalText(t *testing.T) {
	r := &Run{chat: []ChatMsg{{Role: "app", Text: "q"}, {Role: "thinking", Text: "hmm"}, {Role: "agent", Text: "Let me check."}, {Role: "tool", Tool: "search"}, {Role: "thinking", Text: "...\n\nThis holds up."}}}
	if got := r.finalText(); got != "This holds up." {
		t.Fatalf("%q", got)
	}
	r.chat = append(r.chat, ChatMsg{Role: "agent", Text: "This holds up, said aloud."})
	if got := r.finalText(); got != "This holds up, said aloud." {
		t.Fatalf("%q", got)
	}
}

func TestPersonaReadsOnly(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	for _, project := range []string{"", "alpha"} {
		r := a.mustRun(t, "persona:critic:t"+project)
		r.Project = project
		names := map[string]bool{}
		for _, tl := range a.toolsFor(r) {
			if !personaTools[tl.Name] {
				t.Fatalf("a persona may only read; it has %s", tl.Name)
			}
			names[tl.Name] = true
		}
		if !names["file_read"] || !names["work_items"] {
			t.Fatalf("it can read (project %q): %v", project, names)
		}
	}
}

func TestPersonaWithItsOwnModelRefusesInPrivate(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	os.MkdirAll(a.userPersonasDir(), 0o700)
	os.WriteFile(filepath.Join(a.userPersonasDir(), "big.md"), []byte("---\nmodel: anthropic/opus\n---\nthink hard\n"), 0o600)
	if err := a.setPrivate("beta", true); err != nil {
		t.Fatal(err)
	}
	r := a.mustRun(t, "p:beta")
	a.Chat("p:beta", "@big and @critic, thoughts?", "", "")
	waitFor(t, "both answers", func() bool {
		n := 0
		msgs, _ := r.history()
		for _, m := range msgs {
			if m.Role == "persona" && m.Done {
				n++
			}
		}
		return n == 2
	})
	msgs, _ := r.history()
	for _, m := range msgs {
		switch {
		case m.Author == "big" && (!m.Error || !strings.Contains(m.Text, "private")):
			t.Fatalf("its own model is a way out of a private project: %+v", m)
		case m.Author == "critic" && m.Error:
			t.Fatalf("one on the project's model is fine: %+v", m)
		}
	}
	if s, _, _ := (*a.cfg.Agents.(*fakeLoops)).started("persona:critic:"); !s.Private || s.Model != "" {
		t.Fatalf("the critic runs as a private agent: %+v", s)
	}
}

func TestPersonaWrite(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	if _, err := call(t, a, "home", "persona_write", map[string]any{"handle": "Bad Handle", "name": "x", "about": "x", "brief": "x"}); err == nil {
		t.Fatal("bad handle accepted")
	}
	out := mustCall(t, a, "home", "persona_write", map[string]any{"handle": "@grumpy", "name": "Grumpy", "about": "Hates everything", "brief": "You hate everything.\n\nSay why.", "color": "#333"})
	if !strings.Contains(out, "@grumpy") {
		t.Fatal(out)
	}
	p, ok := a.persona("grumpy")
	if !ok || p.Name != "Grumpy" || p.About != "Hates everything" || p.Color != "#333" || p.Brief != "You hate everything.\n\nSay why." || p.Builtin {
		t.Fatalf("%+v", p)
	}
	if out := mustCall(t, a, "home", "personas_list", map[string]any{}); !strings.Contains(out, "@grumpy · Grumpy") || !strings.Contains(out, "@critic") {
		t.Fatal(out)
	}
	if _, err := call(t, a, customizerID, "persona_write", map[string]any{"handle": "x"}); err == nil {
		t.Fatal("Eitri keeps its layout-only tools")
	}
}
