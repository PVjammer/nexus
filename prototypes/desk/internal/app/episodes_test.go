package app

import (
	"strings"
	"testing"
	"time"
)

func talk(r *Run, you, agent string) {
	r.add(ChatMsg{Role: "you", Text: you})
	r.add(ChatMsg{Role: "agent", Text: agent})
}

func TestEpisodesSaveAndStartFresh(t *testing.T) {
	a, _ := newTestApp(t)
	r, _ := a.run("home")
	talk(r, "What needs me in alpha?", "Two tasks: the hotel and the API access.")
	ep, ok := a.endEpisode(r, "")
	if !ok || ep.Summary != "What needs me in alpha?" || !r.fresh {
		t.Fatalf("ended with a summary, and the next start is fresh: %+v %v", ep, r.fresh)
	}
	if len(r.current()) != 0 {
		t.Fatal("the new episode starts empty")
	}
	if h, _ := r.history(); h[len(h)-1].Role != "episode" || len(h) != 3 {
		t.Fatal("the old messages stay on screen, behind a divider")
	}
	if _, ok := a.endEpisode(r, ""); ok {
		t.Fatal("an empty episode isn't saved")
	}
	ctx := a.contextFor(r, "", "home")
	if !strings.Contains(ctx, "What needs me in alpha?") || !strings.Contains(ctx, ep.ID) {
		t.Fatalf("the summary carries over:\n%s", ctx)
	}
	out := mustCall(t, a, "home", "history_search", map[string]any{"query": "hotel"})
	if !strings.Contains(out, ep.ID) || !strings.Contains(out, "the hotel") {
		t.Fatalf("history_search finds it: %s", out)
	}
	out = mustCall(t, a, "home", "history_read", map[string]any{"episode": ep.ID})
	if !strings.Contains(out, "What needs me in alpha?") {
		t.Fatalf("history_read reads it: %s", out)
	}
}

func TestEpisodeEndsOnlyWhenSettled(t *testing.T) {
	a, _ := newTestApp(t)
	r, _ := a.run("home")
	talk(r, "Find work in alpha", "I suggested two work items.")
	mustCall(t, a, "home", "work_propose", map[string]any{"project": "alpha", "proposals": []any{map[string]any{"title": "Write the docs"}}, "reason": "found"})
	out := mustCall(t, a, "home", "episode_close", map[string]any{"summary": "Suggested work for alpha"})
	if !strings.Contains(out, "suggestions") {
		t.Fatalf("it says what it waits on: %s", out)
	}
	a.maybeEndEpisode(r, false)
	if len(r.current()) == 0 {
		t.Fatal("suggestions waiting on the user keep the episode open")
	}
	for _, p := range a.openProposals() {
		a.resolveProposals([]string{p.ID}, nil, false)
	}
	a.maybeEndEpisode(r, false)
	if len(r.current()) != 0 || a.episodes("home")[0].Summary != "Suggested work for alpha" {
		t.Fatal("once settled, it ends with the agent's summary")
	}

	// coming back after a while to a settled conversation starts fresh
	talk(r, "Anything due?", "Nothing this week.")
	a.maybeEndEpisode(r, true)
	if len(r.current()) == 0 {
		t.Fatal("not after a moment")
	}
	old := episodeIdle
	episodeIdle = 0
	defer func() { episodeIdle = old }()
	time.Sleep(5 * time.Millisecond)
	a.maybeEndEpisode(r, true)
	if len(r.current()) != 0 {
		t.Fatal("after a while, yes")
	}
}

func TestEitriEpisodeEndsWithKeep(t *testing.T) {
	a, _ := newTestApp(t)
	r, _ := a.run(customizerID)
	r.add(ChatMsg{Role: "you", Text: "Add my board"})
	mustCall(t, a, customizerID, "home_add", map[string]any{"kind": "board", "reason": "board"})
	a.customizerSettle()
	if err := a.customizerKeep("test"); err != nil {
		t.Fatal(err)
	}
	eps := a.episodes(customizerID)
	if len(eps) != 1 || !strings.HasPrefix(eps[0].Summary, "Kept: ") || len(r.current()) != 0 {
		t.Fatalf("Keep ends Eitri's episode: %+v", eps)
	}
}
