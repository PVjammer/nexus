package app

import (
	"strings"
	"testing"
	"time"

	"github.com/pvjammer/nexus/prototypes/desk/internal/agent"
)

func lastOf(r *Run, role string) ChatMsg {
	msgs, _ := r.history()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == role {
			return msgs[i]
		}
	}
	return ChatMsg{}
}

func countRole(r *Run, role string) int {
	msgs, _ := r.history()
	n := 0
	for _, m := range msgs {
		if m.Role == role {
			n++
		}
	}
	return n
}

// TestJoinThenDraft: one message asks two agents and the chat's agent; the
// chat's agent waits for both, then gets one turn with both replies in full.
func TestJoinThenDraft(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	r := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "Plan: X. @critic any issues? @pessimist what goes wrong? Then write it up.", "", "")
	waitFor(t, "the draft", func() bool { return r.lastAgentText() == "Here's the draft" && !r.isBusy() })
	_, ps, _ := f.seen("p:alpha")
	if len(ps) != 1 || !strings.Contains(ps[0], "[@critic] Verdict from persona:critic") || !strings.Contains(ps[0], "[@pessimist] Verdict from persona:pessimist") {
		t.Fatalf("one turn, with both replies:\n%s", ps)
	}
	msgs, _ := r.history()
	roles := []string{}
	for _, m := range msgs {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, " ") != "you persona persona agent" {
		t.Fatalf("the message, both replies, then the draft: %v", roles)
	}
	if len(a.joins()) != 0 {
		t.Fatal("the join is over")
	}
}

// TestJoinGoesAheadWithoutStragglers: a slow reply doesn't hold the chat's
// agent forever; it comes late, marked, and reaches the agent next turn.
func TestJoinGoesAheadWithoutStragglers(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	f.delay = func(s RunSpec) time.Duration {
		if strings.HasPrefix(s.ID, "persona:pessimist") {
			return 600 * time.Millisecond
		}
		return 0
	}
	old := joinWait
	joinWait = 100 * time.Millisecond
	defer func() { joinWait = old }()
	r := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "@critic @pessimist thoughts?", "", "")
	waitFor(t, "the owner's turn", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 1 })
	_, ps, _ := f.seen("p:alpha")
	if !strings.Contains(ps[0], "no reply yet from @pessimist") || !strings.Contains(ps[0], "[@critic]") {
		t.Fatalf("it goes ahead and says who's missing:\n%s", ps[0])
	}
	waitFor(t, "the late reply", func() bool { m := lastOf(r, "persona"); return m.Author == "pessimist" && m.Done })
	if m := lastOf(r, "persona"); !m.Late {
		t.Fatalf("marked late: %+v", m)
	}
	waitFor(t, "idle", func() bool { return !r.isBusy() })
	a.Chat("p:alpha", "and now?", "", "")
	waitFor(t, "the next turn", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 2 })
	if _, ps, _ = f.seen("p:alpha"); !strings.Contains(ps[1], "[@pessimist] Verdict from persona:pessimist") {
		t.Fatalf("the late reply reaches it next turn:\n%s", ps[1])
	}
}

func TestJoinGoButton(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	f.delay = func(s RunSpec) time.Duration {
		if s.Kind == KindPersona {
			return 2 * time.Second
		}
		return 0
	}
	a.Chat("p:alpha", "@critic thoughts?", "", "")
	waitFor(t, "the join", func() bool { return len(a.joins()["p:alpha"]) == 1 })
	a.joinGo("p:alpha")
	waitFor(t, "the owner goes ahead", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 1 })
}

// TestTaggedOwnerRunsIt: tagging the chat's agent hands it the asks.
func TestTaggedOwnerRunsIt(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	a.Chat("p:alpha", "@alpha ask @critic first, then @pessimist, then summarize", "", "")
	waitFor(t, "the owner's turn", func() bool { _, ps, _ := f.seen("p:alpha"); return len(ps) == 1 })
	_, ps, _ := f.seen("p:alpha")
	if !strings.Contains(ps[0], "<tagged") || !strings.Contains(ps[0], "@critic (persona), @pessimist (persona)") {
		t.Fatalf("it's told to run it:\n%s", ps[0])
	}
	if _, _, ok := f.started("persona:"); ok {
		t.Fatal("the core doesn't ask them itself")
	}
}

func startedWorkstream(t *testing.T, a *App) (WorkItem, *Run) {
	w, err := a.workItemAdd("you", "alpha", "Add CSV export", "orders", true)
	if err != nil {
		t.Fatal(err)
	}
	ws := a.mustRun(t, workstreamID(w.Ref))
	return w, ws
}

// TestWorkstreamMentionForksAndMerges: @a-workstream in the project chat is
// answered by a side session of the workstream; the workstream's chat gets a
// cross-post and its session gets the exchange at its next turn.
func TestWorkstreamMentionForksAndMerges(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	w, ws := startedWorkstream(t, a)
	a.Chat(ws.ID, "start on the columns", "", "")
	waitFor(t, "the workstream's turn", func() bool { return ws.lastAgentText() == "ok" && !ws.isBusy() })

	p := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "@add-csv-export where are we?", "", "")
	waitFor(t, "the project agent passes", func() bool { return p.lastAgentText() == "PASS" && !p.isBusy() })
	reply := lastOf(p, "persona")
	if reply.Author != "add-csv-export" || reply.From != ws.ID || reply.Text != "From the side: we're on step 2" {
		t.Fatalf("the workstream answers in the project chat: %+v", reply)
	}
	spec, fps, ok := f.started("fork:" + ws.ID)
	if !ok || spec.Kind != KindFork || spec.ForkOf != ws.ID || spec.Base != KindProject || !strings.Contains(spec.Instructions, "You are a workstream agent") {
		t.Fatalf("a side session of the workstream: %+v", spec)
	}
	if !strings.Contains(fps[0], "side session") || !strings.Contains(fps[0], "the alpha project chat") || !strings.Contains(fps[0], "<workstream") {
		t.Fatalf("the fork knows where it was asked, and its work item:\n%s", fps[0])
	}
	if x := lastOf(ws, "xpost"); !strings.Contains(x.Text, "where are we?") || !strings.Contains(x.Text, "step 2") {
		t.Fatalf("cross-posted into the workstream's chat: %+v", x)
	}
	if _, ps, _ := f.seen(ws.ID); len(ps) != 1 {
		t.Fatal("the workstream's own turn isn't started for a question")
	}
	a.Chat(ws.ID, "carry on", "", "")
	waitFor(t, "its next turn", func() bool { _, ps, _ := f.seen(ws.ID); return len(ps) == 2 })
	if _, ps, _ := f.seen(ws.ID); !strings.Contains(ps[1], "[side session]") || !strings.Contains(ps[1], "we're on step 2") {
		t.Fatalf("merged into its session at its next turn:\n%s", ps[1])
	}
	_ = w
}

// TestForkActionGoesToTheTrunk: a fork never acts; work it agrees to is
// delivered to the workstream, which does it.
func TestForkActionGoesToTheTrunk(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	_, ws := startedWorkstream(t, a)
	p := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "@add-csv-export also handle UTF-8", "", "")
	waitFor(t, "the workstream's turn", func() bool { _, ps, _ := f.seen(ws.ID); return len(ps) == 1 })
	if reply := lastOf(p, "persona"); !strings.Contains(reply.Text, "*Will do next:* add a UTF-8 test") || strings.Contains(reply.Text, "ACTION:") {
		t.Fatalf("the reply says what it'll do: %+v", reply)
	}
	if _, ps, _ := f.seen(ws.ID); !strings.Contains(ps[0], "add a UTF-8 test") || !strings.Contains(ps[0], "Do it now") {
		t.Fatalf("the workstream is asked to do it:\n%s", ps[0])
	}
	for _, tl := range a.toolsFor(&Run{Kind: KindFork, Project: "alpha", trunkKind: KindWorkstream}) {
		if !personaTools[tl.Name] {
			t.Fatalf("a side session only reads; it has %s", tl.Name)
		}
	}
}

// TestAskAgentsRounds: rounds run in order, each later round sees the
// earlier replies; everything is posted in the chat; limits hold.
func TestAskAgentsRounds(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	r := a.mustRun(t, "p:alpha")
	out := mustCall(t, a, "p:alpha", "ask_agents", map[string]any{"rounds": []any{
		[]any{map[string]any{"to": "critic", "question": "Verdict on the plan?"}},
		[]any{map[string]any{"to": "@pessimist", "question": "Given that, what fails first?"}},
	}})
	if !strings.Contains(out, "[@critic] Verdict from persona:critic") || !strings.Contains(out, "[@pessimist]") {
		t.Fatal(out)
	}
	_, ps, _ := f.started("persona:pessimist:")
	if !strings.Contains(ps[0], "Earlier replies in this discussion:\n[@critic] Verdict from persona:critic") {
		t.Fatalf("round 2 sees round 1:\n%s", ps[0])
	}
	if countRole(r, "ask") != 2 || countRole(r, "persona") != 2 {
		t.Fatal("the questions and replies are posted in the chat")
	}
	if _, err := call(t, a, "p:alpha", "ask_agents", map[string]any{"rounds": []any{
		[]any{map[string]any{"to": "critic", "question": "a"}}, []any{map[string]any{"to": "critic", "question": "b"}},
	}}); err == nil || !strings.Contains(err.Error(), "3 rounds") {
		t.Fatalf("at most 3 rounds a turn: %v", err)
	}
	if _, err := call(t, a, "home", "ask_agents", map[string]any{"rounds": []any{[]any{map[string]any{"to": "nobody", "question": "a"}}}}); err == nil || !strings.Contains(err.Error(), "critic") {
		t.Fatalf("an unknown handle lists who can be asked: %v", err)
	}
	if _, err := call(t, a, "p:alpha", "ask_agents", map[string]any{"rounds": []any{[]any{map[string]any{"to": "alpha", "question": "a"}}}}); err == nil {
		t.Fatal("not itself")
	}
}

func TestHandles(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	startedWorkstream(t, a)
	a.workItemAdd("you", "beta", "Add CSV export", "", true) // the same title in another project
	hs := map[string]Target{}
	for _, t := range a.handles() {
		hs[t.Handle] = t
	}
	if hs["add-csv-export"].Project != "alpha" || hs["add-csv-export-2"].Project != "beta" || hs["alpha"].Agent != "p:alpha" || hs["critic"].Kind != "persona" || hs["home"].Agent != "home" {
		t.Fatalf("%+v", hs)
	}
	in := func(r *Run, h string) bool {
		for _, t := range a.targetsFor(r) {
			if t.Handle == h {
				return true
			}
		}
		return false
	}
	p := a.mustRun(t, "p:alpha")
	if !in(p, "add-csv-export") || in(p, "add-csv-export-2") || !in(p, "beta") {
		t.Fatal("a project's chat reaches its own workstreams, and other projects' agents")
	}
	if err := a.setPrivate("beta", true); err != nil {
		t.Fatal(err)
	}
	if home := a.mustRun(t, "home"); in(home, "beta") || in(home, "add-csv-export-2") || !in(home, "add-csv-export") {
		t.Fatal("nothing behind a private wall from outside it")
	}
	if !in(a.mustRun(t, "p:beta"), "add-csv-export-2") {
		t.Fatal("but inside, yes")
	}
	if got := slug("Fix the slow startup!", 3); got != "fix-the-slow" {
		t.Fatal(got)
	}
}

func TestStyles(t *testing.T) {
	a, _ := newTestApp(t)
	withFakeLoops(t, a)
	by := map[string]Style{}
	for _, s := range a.styles() {
		by[s.Name] = s
	}
	if by["tldr"].Enabled || by["tldr"].Render != "collapse" || !by["verdict"].Enabled || len(by["verdict"].Verdicts) != 3 || by["verdict"].Verdicts[2] != (Verdict{"This holds up.", "good"}) {
		t.Fatalf("shipped: %+v", by)
	}
	if strings.Contains(a.contextFor(a.mustRun(t, "p:alpha"), "", ""), "<reply-style") {
		t.Fatal("tl;dr ships off")
	}
	mustCall(t, a, "home", "style_write", map[string]any{"name": "tldr", "applies": "project, workstream", "render": "collapse", "instruction": "tl;dr, then ---, then details", "enabled": true})
	if ctx := a.contextFor(a.mustRun(t, "p:alpha"), "", ""); !strings.Contains(ctx, "tl;dr, then ---, then details") {
		t.Fatalf("on from the next message:\n%s", ctx)
	}
	if strings.Contains(a.contextFor(a.mustRun(t, "home"), "", ""), "<reply-style") || a.styleTail(a.mustRun(t, "home")) != "" {
		t.Fatal("only where it applies")
	}
	if !strings.Contains(a.styleTail(a.mustRun(t, "p:alpha")), "tl;dr, then ---") {
		t.Fatal("and repeated after the user's message")
	}
	if withoutStyle(a.contextFor(a.mustRun(t, "p:alpha"), "", "")) == a.contextFor(a.mustRun(t, "p:alpha"), "", "") {
		t.Fatal("a persona reading the context doesn't take the chat's style")
	}
	for _, bad := range []map[string]any{
		{"name": "x1", "applies": "project", "render": "sparkles", "instruction": "x"},
		{"name": "x2", "applies": "persona:nobody", "render": "collapse", "instruction": "x"},
		{"name": "x3", "applies": "everywhere", "render": "collapse", "instruction": "x"},
	} {
		if _, err := call(t, a, "home", "style_write", bad); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	if got := summaryOf("The answer is 4.\n---\nBecause 2+2.", 100); got != "The answer is 4. (details in the chat)" {
		t.Fatal(got)
	}
	if got := summaryOf("No details here.", 100); got != "No details here." {
		t.Fatal(got)
	}
}

// TestBlankThinkingDoesntSplitAnAnswer: local Qwen sends a "\n" reasoning
// chunk right after its first word; the answer stays one message.
func TestBlankThinkingDoesntSplitAnAnswer(t *testing.T) {
	a, _ := newTestApp(t)
	f := withFakeLoops(t, a)
	f.script = func(RunSpec, string) []agent.Event {
		return []agent.Event{{Type: agent.EventThinking, Text: "planning"}, {Type: agent.EventDelta, Text: "Re"},
			{Type: agent.EventThinking, Text: "\n"}, {Type: agent.EventDelta, Text: "vised plan"}}
	}
	r := a.mustRun(t, "p:alpha")
	a.Chat("p:alpha", "go", "", "")
	waitFor(t, "the turn", func() bool { return countRole(r, "agent") > 0 && !r.isBusy() })
	if n, got := countRole(r, "agent"), r.lastAgentText(); n != 1 || got != "Revised plan" {
		t.Fatalf("%d message(s), %q", n, got)
	}
}
