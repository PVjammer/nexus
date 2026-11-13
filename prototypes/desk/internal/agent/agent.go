// Package agent is the agent-loop seam. The core drives "an agent" through
// this interface; pi is the prototype implementation. A custom loop (Go or
// Rust) implements the same interface later. This is a placeholder, not a
// backend decision.
package agent

import "context"

type EventType string

const (
	EventDelta     EventType = "delta"    // streamed assistant text
	EventThinking  EventType = "thinking" // streamed reasoning (shown dimmed)
	EventToolStart EventType = "tool_start"
	EventToolDraft EventType = "tool_draft" // the model is still writing a tool call's arguments (Text = the new chunk)
	EventToolEnd   EventType = "tool_end"
	EventTurnEnd   EventType = "turn_end"
	EventError     EventType = "error"
)

type Event struct {
	Type    EventType `json:"type"`
	Text    string    `json:"text,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	CallID  string    `json:"callId,omitempty"`
	Args    string    `json:"args,omitempty"`
	Result  string    `json:"result,omitempty"`
	IsError bool      `json:"isError,omitempty"`
}

type Backend interface {
	Start(ctx context.Context) error
	Prompt(ctx context.Context, message string) error
	Abort() error
	Events() <-chan Event
	Close() error
	Name() string
}
