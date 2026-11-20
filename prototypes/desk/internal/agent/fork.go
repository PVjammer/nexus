package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// ForkSource copies a pi session file up to its last finished turn: the
// last assistant message that didn't stop to call a tool. A fork taken while
// the agent is mid-turn then starts from a consistent point, with no tool
// call left waiting for its result. It reports false when there's no
// finished turn to fork from.
func ForkSource(src, dst string) (bool, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	last := -1
	for i, l := range lines {
		var e struct {
			Type    string `json:"type"`
			Message struct {
				Role       string `json:"role"`
				StopReason string `json:"stopReason"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(l), &e) != nil || e.Type != "message" {
			continue
		}
		if e.Message.Role == "assistant" && e.Message.StopReason != "toolUse" {
			last = i
		}
	}
	if last < 0 {
		return false, nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	w := bufio.NewWriter(f)
	for _, l := range lines[:last+1] {
		w.WriteString(l + "\n")
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return false, err
	}
	return true, f.Close()
}
