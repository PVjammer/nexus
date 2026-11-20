package agent

// Pi drives `pi --mode rpc` (JSONL on stdin/stdout). Adapted from the PA
// spike's pibackend (which proved the wire format live). Additions: our tool
// extension (-e), a session dir so the transcript can be leak-checked, a wire
// log of everything exchanged, steer-while-busy, abort, and built-in tools
// off by default (the agent reaches files/terminals only through app tools,
// the same shape a node would have).

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
	"sync"
	"sync/atomic"
	"time"
)

type PiConfig struct {
	Binary       string
	Model        string // "provider/id"; empty = pi's default
	Extension    string // path to the nexus tool extension
	SessionDir   string
	WorkDir      string
	SystemExtra  string
	BuiltinTools string // "" = none; e.g. "read" to allow pi's read tool
	Continue     bool   // resume the latest session in SessionDir (the agent keeps its memory across restarts)
	Fork         string // start as a copy of this session file (a side session); overrides Continue
	Env          []string
	WireLog      string // append every line sent/received (for leak checks)
}

type Pi struct {
	cfg    PiConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan Event
	mu     sync.Mutex
	busy   atomic.Bool
	wire   *os.File
	exited chan struct{}
	closed atomic.Bool
}

func NewPi(cfg PiConfig) *Pi {
	if cfg.Binary == "" {
		cfg.Binary = "pi"
	}
	return &Pi{cfg: cfg, events: make(chan Event, 256), exited: make(chan struct{})}
}

func (p *Pi) Name() string {
	if p.cfg.Model != "" {
		return "pi · " + p.cfg.Model
	}
	return "pi · default model"
}

func (p *Pi) Start(ctx context.Context) error {
	args := []string{"--mode", "rpc", "--no-context-files", "--no-skills", "--no-prompt-templates", "--no-extensions"}
	if p.cfg.SessionDir != "" {
		args = append(args, "--session-dir", p.cfg.SessionDir)
		if p.cfg.Fork != "" {
			args = append(args, "--fork", p.cfg.Fork)
		} else if p.cfg.Continue {
			args = append(args, "--continue")
		}
	} else {
		args = append(args, "--no-session")
	}
	if p.cfg.Model != "" {
		args = append(args, "--model", p.cfg.Model)
	}
	if p.cfg.Extension != "" {
		args = append(args, "-e", p.cfg.Extension)
	}
	if p.cfg.BuiltinTools == "" {
		args = append(args, "--no-builtin-tools")
	}
	if p.cfg.SystemExtra != "" {
		args = append(args, "--append-system-prompt", p.cfg.SystemExtra)
	}
	cmd := exec.CommandContext(ctx, p.cfg.Binary, args...)
	cmd.Dir = p.cfg.WorkDir
	cmd.Env = append(os.Environ(), p.cfg.Env...)
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
	if p.cfg.WireLog != "" {
		p.wire, _ = os.OpenFile(p.cfg.WireLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start pi: %w", err)
	}
	p.cmd, p.stdin = cmd, stdin
	go p.read(stdout, &stderr)
	log.Printf("agent: pi started pid=%d", cmd.Process.Pid)
	return nil
}

type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(b []byte) (int, error) {
	if w.buf.Len() < w.max {
		w.buf.Write(b)
	}
	return len(b), nil
}

func (p *Pi) logWire(dir string, line []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.wire == nil {
		return
	}
	p.wire.Write([]byte(dir + " "))
	p.wire.Write(line)
	p.wire.Write([]byte("\n"))
}

func (p *Pi) read(r io.Reader, stderr *bytes.Buffer) {
	defer close(p.events)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		p.logWire("<", line)
		for _, e := range translate(line) {
			if e.Type == EventTurnEnd {
				p.busy.Store(false)
			}
			p.events <- e
		}
	}
	msg := "pi exited"
	if p.cmd != nil {
		if err := p.cmd.Wait(); err != nil {
			msg = "pi exited: " + err.Error()
		}
	}
	close(p.exited)
	if p.closed.Load() {
		return // we stopped it on purpose
	}
	if s := bytes.TrimSpace(stderr.Bytes()); len(s) > 0 {
		msg += "\n" + string(s)
	}
	p.events <- Event{Type: EventError, Text: msg}
}

func translate(line []byte) []Event {
	var rec struct {
		Type string `json:"type"`
		AME  struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
		} `json:"assistantMessageEvent"`
		ToolCallID string          `json:"toolCallId"`
		ToolName   string          `json:"toolName"`
		Args       json.RawMessage `json:"args"`
		IsError    bool            `json:"isError"`
		Result     struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error   string `json:"error"`
		Command string `json:"command"`
		Success *bool  `json:"success"`
		Message struct {
			Role         string `json:"role"`
			StopReason   string `json:"stopReason"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil {
		return nil
	}
	switch rec.Type {
	case "message_update":
		switch rec.AME.Type {
		case "text_delta":
			return []Event{{Type: EventDelta, Text: rec.AME.Delta}}
		case "thinking_delta":
			return []Event{{Type: EventThinking, Text: rec.AME.Delta}}
		case "toolcall_start":
			return []Event{{Type: EventToolDraft}}
		case "toolcall_delta":
			return []Event{{Type: EventToolDraft, Text: rec.AME.Delta}}
		}
	case "message_end":
		if rec.Message.Role == "assistant" && rec.Message.StopReason == "error" && rec.Message.ErrorMessage != "" {
			return []Event{{Type: EventError, Text: rec.Message.ErrorMessage}}
		}
	case "tool_execution_start":
		return []Event{{Type: EventToolStart, Tool: rec.ToolName, CallID: rec.ToolCallID, Args: string(rec.Args)}}
	case "tool_execution_end":
		var text string
		for _, c := range rec.Result.Content {
			if c.Type == "text" {
				text = c.Text
				break
			}
		}
		return []Event{{Type: EventToolEnd, Tool: rec.ToolName, CallID: rec.ToolCallID, Result: text, IsError: rec.IsError}}
	case "agent_settled":
		return []Event{{Type: EventTurnEnd}}
	case "response":
		if rec.Success != nil && !*rec.Success {
			return []Event{{Type: EventError, Text: fmt.Sprintf("pi rejected %s: %s", rec.Command, rec.Error)}}
		}
	case "error":
		return []Event{{Type: EventError, Text: rec.Error}}
	}
	return nil
}

func (p *Pi) send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.logWire(">", b)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stdin == nil {
		return fmt.Errorf("pi not started")
	}
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *Pi) Prompt(_ context.Context, msg string) error {
	cmd := map[string]any{"type": "prompt", "message": msg}
	if p.busy.Load() {
		cmd["streamingBehavior"] = "steer"
	}
	p.busy.Store(true)
	return p.send(cmd)
}

func (p *Pi) Abort() error { return p.send(map[string]any{"type": "abort"}) }

func (p *Pi) Events() <-chan Event { return p.events }

// Close stops pi: stdin is closed, and the process is killed if it hasn't
// exited shortly after.
func (p *Pi) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	p.mu.Lock()
	if p.stdin != nil {
		p.stdin.Close()
	}
	p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		select {
		case <-p.exited:
		case <-time.After(1500 * time.Millisecond):
			p.cmd.Process.Kill()
		}
	}
	p.mu.Lock()
	if p.wire != nil {
		p.wire.Close()
		p.wire = nil
	}
	p.mu.Unlock()
	return nil
}
