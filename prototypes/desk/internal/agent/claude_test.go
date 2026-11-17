package agent

import (
	"strings"
	"testing"
)

func TestClaudeArgsLockItDown(t *testing.T) {
	c := NewClaude(ClaudeConfig{MCPConfig: "/d/mcp.json", SystemFile: "/d/home.md", SessionID: "abc", Resume: true, Model: "sonnet"})
	a := strings.Join(c.Args(), " ")
	for _, want := range []string{"-p --input-format stream-json --output-format stream-json", "--tools  --strict-mcp-config", "--setting-sources  --allowedTools mcp__nexus", "--permission-prompts none",
		"--mcp-config /d/mcp.json", "--system-prompt-file /d/home.md", "--resume abc", "--model sonnet"} {
		if !strings.Contains(a, want) {
			t.Fatalf("args missing %q:\n%s", want, a)
		}
	}
	if strings.Contains(a, "--bare") {
		t.Fatal("--bare only takes an API key; it would refuse the user's login")
	}
	if c2 := NewClaude(ClaudeConfig{SessionID: "abc"}); !strings.Contains(strings.Join(c2.Args(), " "), "--session-id abc") {
		t.Fatal("a new conversation should set --session-id")
	}
}

// Lines in the shape Claude Code prints with --output-format stream-json
// --include-partial-messages.
var claudeTurn = []string{
	`{"type":"system","subtype":"init","tools":["mcp__nexus__home_add"]}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"On it."}}}`,
	`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"tu1","name":"mcp__nexus__home_add","input":{}}}}`,
	`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"kind\":"}}}`,
	`{"type":"assistant","message":{"content":[{"type":"text","text":"On it."},{"type":"tool_use","id":"tu1","name":"mcp__nexus__home_add","input":{"kind":"needs","reason":"r"}}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"added w3"}],"is_error":false}]}}`,
	`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"error: no","is_error":true}]}}`,
	`{"type":"result","subtype":"success","is_error":false,"result":"Done."}`,
}

func TestClaudeEventsTranslate(t *testing.T) {
	c := NewClaude(ClaudeConfig{})
	var got []Event
	for _, l := range claudeTurn {
		got = append(got, c.translate([]byte(l))...)
	}
	want := []EventType{EventThinking, EventDelta, EventToolDraft, EventToolDraft, EventToolStart, EventToolEnd, EventToolEnd, EventTurnEnd}
	if len(got) != len(want) {
		t.Fatalf("got %d events %+v", len(got), got)
	}
	for i, e := range got {
		if e.Type != want[i] {
			t.Fatalf("event %d: %s, want %s", i, e.Type, want[i])
		}
	}
	if got[1].Text != "On it." {
		t.Fatal("text deltas carry the text (and the full message doesn't repeat it)")
	}
	if got[4].Tool != "home_add" || !strings.Contains(got[4].Args, `"needs"`) || got[4].CallID != "tu1" {
		t.Fatalf("tool start: %+v", got[4])
	}
	if got[5].Result != "added w3" || got[5].IsError || got[6].Result != "error: no" || !got[6].IsError {
		t.Fatalf("tool results: %+v %+v", got[5], got[6])
	}
	c.stopping.Store(true) // what Abort does (verified live: the interrupted turn ends with error_during_execution)
	if ev := c.translate([]byte(`{"type":"result","subtype":"error_during_execution","is_error":true}`)); len(ev) != 1 || ev[0].Type != EventTurnEnd {
		t.Fatalf("a stopped turn should end quietly: %+v", ev)
	}
	errs := c.translate([]byte(`{"type":"result","subtype":"error_max_turns","is_error":true,"result":""}`))
	if len(errs) != 2 || errs[0].Type != EventError || !strings.Contains(errs[0].Text, "error_max_turns") || errs[1].Type != EventTurnEnd {
		t.Fatalf("a failed turn: %+v", errs)
	}
}
