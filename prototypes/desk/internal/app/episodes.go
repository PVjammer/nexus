package app

// Episodes: the home agent (also the quick window, the same agent opened from
// anywhere) and the customizer work in episodes, so their context never piles
// up. An episode ends when its work is settled. Nexus decides, not the model:
//
//   - the customizer's when you Keep or Roll back;
//   - the home agent's when nothing it raised is still waiting on you (a
//     question, suggestions, filings) and either it said it's done
//     (episode_close) or you come back after a while;
//   - any of them when you press New conversation.
//
// An ended episode is saved to disk with a one-line summary; the agent's
// model session starts fresh, with the last few summaries in its context and
// history_search to pull anything back. On screen the old messages fold
// behind a divider with the summary: you never lose what was said.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// episodeIdle: coming back after this long, to a settled conversation,
// starts a new episode.
var episodeIdle = 20 * time.Minute

type Episode struct {
	ID       string    `json:"id"`
	Agent    string    `json:"agent"`
	Summary  string    `json:"summary"`
	Started  time.Time `json:"started"`
	Ended    time.Time `json:"ended"`
	Messages []ChatMsg `json:"messages"`
}

func episodic(kind string) bool { return kind == KindHome || kind == KindCustomizer }

func (a *App) episodeDir(agent string) string {
	return filepath.Join(a.cfg.DataDir, "episodes", strings.NewReplacer(":", "_", "/", "_").Replace(agent))
}

// current is the conversation since the last episode ended.
func (r *Run) current() []ChatMsg {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := len(r.chat) - 1
	for i >= 0 && r.chat[i].Role != "episode" {
		i--
	}
	return append([]ChatMsg(nil), r.chat[i+1:]...)
}

func defaultSummary(msgs []ChatMsg) string {
	for _, m := range msgs {
		if (m.Role == "you" || m.Role == "app") && strings.TrimSpace(m.Text) != "" {
			return oneLine(m.Text, 90)
		}
	}
	return "a conversation"
}

// episodePending is what the agent raised that still waits on you ("" if
// nothing): an episode doesn't end on its own while something does.
func (a *App) episodePending(r *Run) string {
	if r.isBusy() {
		return "it's working"
	}
	if _, ok := r.pendingQuestion(); ok {
		return "a question for you"
	}
	for _, p := range a.openProposals() {
		if p.Agent == r.ID {
			return "suggestions to tick"
		}
	}
	snap := a.Desk.Snapshot()
	if r.Kind == KindHome {
		for _, c := range snap.Captures {
			if c.Status == "proposed" {
				return "filings to confirm"
			}
		}
	}
	if r.Kind == KindCustomizer && a.Desk.HasLayout(homeDraft) {
		return "a draft to Keep or Roll back"
	}
	return ""
}

// endEpisode saves the current conversation and starts the agent fresh.
func (a *App) endEpisode(r *Run, summary string) (Episode, bool) {
	msgs := r.current()
	real := false
	for _, m := range msgs {
		real = real || m.Role == "you" || m.Role == "app"
	}
	if !real {
		return Episode{}, false
	}
	ep := Episode{ID: "e" + time.Now().Format("20060102-150405.000"), Agent: r.ID, Summary: oneLine(firstNonEmpty(summary, defaultSummary(msgs)), 140),
		Started: time.UnixMilli(msgs[0].TS), Ended: time.Now(), Messages: msgs}
	ep.ID = strings.ReplaceAll(ep.ID, ".", "")
	dir := a.episodeDir(r.ID)
	if err := os.MkdirAll(dir, 0o700); err == nil {
		b, _ := json.Marshal(ep)
		os.WriteFile(filepath.Join(dir, ep.ID+".json"), b, 0o600)
	}
	r.mu.Lock()
	r.wantEnd = ""
	r.mu.Unlock()
	r.add(ChatMsg{Role: "episode", Text: ep.Summary, Args: ep.ID})
	r.resetLoop()
	return ep, true
}

// resetLoop stops the agent's loop; the next message starts it with a fresh
// model session (the old one is kept on disk by the factory).
func (r *Run) resetLoop() {
	r.startMu.Lock()
	be := r.be
	r.be, r.fresh = nil, true
	r.startMu.Unlock()
	if be != nil {
		go be.Close()
	}
}

// maybeEndEpisode ends a settled episode: when the agent asked to end it, or
// when you come back after a while (idle is how long since the last message).
func (a *App) maybeEndEpisode(r *Run, comingBack bool) {
	if !episodic(r.Kind) || r.Kind == KindCustomizer || a.episodePending(r) != "" {
		return
	}
	r.mu.Lock()
	want := r.wantEnd
	last := time.Time{}
	if n := len(r.chat); n > 0 {
		last = time.UnixMilli(r.chat[n-1].TS)
	}
	r.mu.Unlock()
	if want != "" || (comingBack && !last.IsZero() && time.Since(last) > episodeIdle) {
		a.endEpisode(r, want)
	}
}

// episodes lists an agent's saved episodes, newest first (messages left out).
func (a *App) episodes(agent string) []Episode {
	paths, _ := filepath.Glob(filepath.Join(a.episodeDir(agent), "*.json"))
	var out []Episode
	for _, p := range paths {
		if ep, ok := readEpisode(p); ok {
			ep.Messages = nil
			out = append(out, ep)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ended.After(out[j].Ended) })
	return out
}

func readEpisode(path string) (Episode, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Episode{}, false
	}
	var ep Episode
	return ep, json.Unmarshal(b, &ep) == nil && ep.ID != ""
}

// episodesNote is the last few summaries, for the agent's context.
func (a *App) episodesNote(r *Run) string {
	eps := a.episodes(r.ID)
	if len(eps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<earlier-conversations note=\"each ended and was saved; history_search finds anything in them, history_read reads one\">\n")
	for i, ep := range eps {
		if i == 3 {
			fmt.Fprintf(&b, "- … %d older\n", len(eps)-i)
			break
		}
		fmt.Fprintf(&b, "- %s: %s (%s)\n", ago(ep.Ended), ep.Summary, ep.ID)
	}
	b.WriteString("</earlier-conversations>")
	return b.String()
}

func (a *App) episodeTools() []Tool {
	H := []string{KindHome} // Eitri keeps its small layout-only toolset; its summaries are in its context
	return []Tool{
		{Name: "episode_close", Label: "End this conversation", Kinds: []string{KindHome},
			Description: "Say this conversation is done: the user's request is answered or carried out. It's saved with your summary, and the next one starts fresh (the summary stays in your context). It ends once nothing you raised is still waiting on the user.",
			Parameters:  obj([]string{"summary"}, map[string]any{"summary": sprop("one line: what was asked and what you did, e.g. 'Added 3 tasks to CSV export; booked the review'")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				c.Run.mu.Lock()
				c.Run.wantEnd = oneLine(sArg(m, "summary"), 140)
				c.Run.mu.Unlock()
				if p := a.episodePending(c.Run); p != "" && p != "it's working" {
					return fmt.Sprintf("It ends once the user has dealt with %s.", p), nil
				}
				return "It ends when your turn does. Say anything last now, in a line.", nil
			}},
		{Name: "history_search", Label: "Search earlier conversations", Kinds: H,
			Description: "Find something in your earlier conversations with the user (each ended and was saved).",
			Parameters:  obj([]string{"query"}, map[string]any{"query": sprop("a word or phrase")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				q := strings.ToLower(strings.TrimSpace(sArg(m, "query")))
				if q == "" {
					return "", fixErr("query is empty: give a word to find")
				}
				var b strings.Builder
				hits := 0
				for _, e := range a.episodes(c.Run.ID) {
					ep, _ := readEpisode(filepath.Join(a.episodeDir(c.Run.ID), e.ID+".json"))
					for i, msg := range ep.Messages {
						if !strings.Contains(strings.ToLower(msg.Text+" "+msg.Args), q) {
							continue
						}
						if l := msgLine(i+1, msg, 240); l != "" && hits < 20 {
							fmt.Fprintf(&b, "%s (%s, %s) %s\n", ep.ID, ago(ep.Ended), oneLine(ep.Summary, 60), l)
							hits++
						}
					}
				}
				if hits == 0 {
					return fmt.Sprintf("nothing matches %q in your earlier conversations", sArg(m, "query")), nil
				}
				return b.String() + "(history_read episode=<id> from=<#> reads around a match)", nil
			}},
		{Name: "history_read", Label: "Read an earlier conversation", Kinds: H,
			Description: "Read one of your earlier conversations (an episode id from your context or history_search).",
			Parameters:  obj([]string{"episode"}, map[string]any{"episode": sprop("episode id, e.g. e20261118-101500123"), "from": iprop("message number to start at (optional)")}),
			run: func(a *App, c *Ctx, m map[string]any) (string, error) {
				id := filepath.Base(sArg(m, "episode"))
				ep, ok := readEpisode(filepath.Join(a.episodeDir(c.Run.ID), id+".json"))
				if !ok {
					return "", fixErr("no episode %q. Your context lists recent ones; history_search finds others", id)
				}
				return fmt.Sprintf("%s (%s): %s\n", ep.ID, ago(ep.Ended), ep.Summary) + transcriptWindow(ep.Messages, "", iArg(m, "from", 1), 30), nil
			}},
	}
}
