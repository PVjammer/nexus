package agent

// Claude drives the Claude Code CLI headless (`claude -p` with stream-json in
// and out): no SDK, and it runs on the user's own Claude Code login. All of
// Claude Code's built-in tools are off (--tools ""); the agent's only tools
// are Nexus's, served by `nexus-core mcp` and policed by the core, exactly
// like pi's. Its own settings, hooks and MCP servers aren't loaded, and it
// runs in an empty folder inside Nexus's data directory so no project
// CLAUDE.md is picked up.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ClaudeConfig struct {
	Binary     string // default "claude"
	Model      string // e.g. "sonnet", "opus", or a full model id; empty = Claude Code's default
	SystemFile string // the agent's instructions (replaces Claude Code's coding prompt)
	MCPConfig  string // JSON file declaring the "nexus" MCP server
	SessionID  string // conversation id (UUID)
	Resume     bool   // continue SessionID instead of starting it
	WorkDir    string // an empty folder; tools reach files through Nexus, not the cwd
	Env        []string
	WireLog    string
	// SettingSources is passed as --setting-sources ("" = none of user,
	// project, local: no hooks or permission rules from the user's setup).
	SettingSources string
}

// ToolPrefix is how Claude Code names tools from the "nexus" MCP server.
const ToolPrefix = "mcp__nexus__"

type Claude struct {
	cfg    ClaudeConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan Event
	mu     sync.Mutex
	wire   *os.File
	exited chan struct{}
	closed atomic.Bool
	reqSeq atomic.Int64
	// stopping is set by Abort: the interrupted turn ends with
	// error_during_execution, which is the user's Stop, not a failure.
	stopping atomic.Bool
	// Claude Code streams a tool call's arguments before the full assistant
	// message arrives; names are kept by content-block index for the drafts.
	blocks map[int]string
}

func NewClaude(cfg ClaudeConfig) *Claude {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	return &Claude{cfg: cfg, events: make(chan Event, 256), exited: make(chan struct{}), blocks: map[int]string{}}
}

func (c *Claude) Name() string {
	if c.cfg.Model != "" {
		return "claude code · " + c.cfg.Model
	}
	return "claude code · default model"
}

// Args is the command line (exported for tests).
func (c *Claude) Args() []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--tools", "", "--strict-mcp-config", "--setting-sources", c.cfg.SettingSources,
		"--allowedTools", "mcp__nexus", "--permission-prompts", "none"}
	if c.cfg.MCPConfig != "" {
		args = append(args, "--mcp-config", c.cfg.MCPConfig)
	}
	if c.cfg.SystemFile != "" {
		args = append(args, "--system-prompt-file", c.cfg.SystemFile)
	}
	if c.cfg.SessionID != "" {
		if c.cfg.Resume {
			args = append(args, "--resume", c.cfg.SessionID)
		} else {
			args = append(args, "--session-id", c.cfg.SessionID)
		}
	}
	if c.cfg.Model != "" {
		args = append(args, "--model", c.cfg.Model)
	}
	return args
}

func (c *Claude) Start(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, c.cfg.Binary, c.Args()...)
	cmd.Dir = c.cfg.WorkDir
	// A Claude Code session inside another one refuses to start; this child
	// is its own agent.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") && !strings.HasPrefix(e, "CLAUDE_CODE_ENTRYPOINT=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, c.cfg.Env...)
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{buf: &stderr, max: 16 * 1024}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if c.cfg.WireLog != "" {
		c.wire, _ = os.OpenFile(c.cfg.WireLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	c.cmd, c.stdin = cmd, stdin
	go c.read(stdout, &stderr)
	log.Printf("agent: claude code started pid=%d", cmd.Process.Pid)
	return nil
}

func (c *Claude) logWire(dir string, line []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wire == nil {
		return
	}
	c.wire.Write([]byte(dir + " "))
	c.wire.Write(line)
	c.wire.Write([]byte("\n"))
}

func (c *Claude) read(r io.Reader, stderr *bytes.Buffer) {
	defer close(c.events)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		c.logWire("<", line)
		for _, e := range c.translate(line) {
			c.events <- e
		}
	}
	msg := "claude exited"
	if c.cmd != nil {
		if err := c.cmd.Wait(); err != nil {
			msg = "claude exited: " + err.Error()
		}
	}
	close(c.exited)
	if c.closed.Load() {
		return
	}
	if s := bytes.TrimSpace(stderr.Bytes()); len(s) > 0 {
		msg += "\n" + string(s)
	}
	c.events <- Event{Type: EventError, Text: msg}
}

type ccBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type ccLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Message struct {
		Content []ccBlock `json:"content"`
	} `json:"message"`
	Event struct {
		Type         string  `json:"type"`
		Index        int     `json:"index"`
		ContentBlock ccBlock `json:"content_block"`
		Delta        struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	} `json:"event"`
	IsError bool     `json:"is_error"`
	Result  string   `json:"result"`
	Errors  []string `json:"errors"`
}

// toolName strips the MCP prefix so the transcript shows Nexus's tool names.
func toolName(n string) string { return strings.TrimPrefix(n, ToolPrefix) }

func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" {
				b.WriteString(p.Text)
			}
		}
		return b.String()
	}
	return string(raw)
}

func (c *Claude) translate(line []byte) []Event {
	var rec ccLine
	if json.Unmarshal(line, &rec) != nil {
		return nil
	}
	switch rec.Type {
	case "stream_event":
		switch rec.Event.Type {
		case "content_block_start":
			if rec.Event.ContentBlock.Type == "tool_use" {
				c.blocks[rec.Event.Index] = rec.Event.ContentBlock.Name
				return []Event{{Type: EventToolDraft}}
			}
		case "content_block_delta":
			switch rec.Event.Delta.Type {
			case "text_delta":
				return []Event{{Type: EventDelta, Text: rec.Event.Delta.Text}}
			case "thinking_delta":
				return []Event{{Type: EventThinking, Text: rec.Event.Delta.Thinking}}
			case "input_json_delta":
				return []Event{{Type: EventToolDraft, Text: rec.Event.Delta.PartialJSON}}
			}
		}
	case "assistant":
		// Text already arrived as deltas; the full message brings tool calls.
		var out []Event
		for _, b := range rec.Message.Content {
			if b.Type == "tool_use" {
				out = append(out, Event{Type: EventToolStart, Tool: toolName(b.Name), CallID: b.ID, Args: string(b.Input)})
			}
		}
		return out
	case "user":
		var out []Event
		for _, b := range rec.Message.Content {
			if b.Type == "tool_result" {
				out = append(out, Event{Type: EventToolEnd, CallID: b.ToolUseID, Result: resultText(b.Content), IsError: b.IsError})
			}
		}
		return out
	case "result":
		c.blocks = map[int]string{}
		if c.stopping.Swap(false) && rec.Subtype != "success" {
			return []Event{{Type: EventTurnEnd}}
		}
		if rec.IsError || (rec.Subtype != "" && rec.Subtype != "success") {
			why := rec.Result
			if why == "" {
				why = strings.Join(rec.Errors, "; ")
			}
			if why == "" {
				why = rec.Subtype
			}
			return []Event{{Type: EventError, Text: "claude: " + why}, {Type: EventTurnEnd}}
		}
		return []Event{{Type: EventTurnEnd}}
	}
	return nil
}

func (c *Claude) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.logWire(">", b)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return fmt.Errorf("claude not started")
	}
	_, err = c.stdin.Write(append(b, '\n'))
	return err
}

// Prompt sends a user message. Claude Code queues messages that arrive while
// it's working and answers them in order.
func (c *Claude) Prompt(_ context.Context, msg string) error {
	return c.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": msg}})
}

// Abort asks Claude Code to stop the current turn (the control request the
// SDKs use over stream-json).
func (c *Claude) Abort() error {
	c.stopping.Store(true)
	return c.send(map[string]any{"type": "control_request", "request_id": fmt.Sprintf("nexus-%d", c.reqSeq.Add(1)), "request": map[string]any{"subtype": "interrupt"}})
}

func (c *Claude) Events() <-chan Event { return c.events }

func (c *Claude) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	c.mu.Lock()
	if c.stdin != nil {
		c.stdin.Close()
	}
	c.mu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		select {
		case <-c.exited:
		case <-time.After(3 * time.Second):
			c.cmd.Process.Kill()
		}
	}
	c.mu.Lock()
	if c.wire != nil {
		c.wire.Close()
		c.wire = nil
	}
	c.mu.Unlock()
	return nil
}
