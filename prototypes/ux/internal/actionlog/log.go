// Package actionlog is the append-only record of what both actors did.
// Secret values never go in here; callers pass labels and handles only.
package actionlog

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"
)

type Entry struct {
	TS     time.Time `json:"ts"`
	Actor  string    `json:"actor"` // "you" | "agent" | "system" | "plugin:<id>"
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
	Detail string    `json:"detail,omitempty"`
	Space  string    `json:"space,omitempty"`
	Denied bool      `json:"denied,omitempty"`
}

type Log struct {
	mu     sync.Mutex
	path   string
	recent []Entry
	onAdd  func(Entry)
}

func Open(path string) *Log {
	l := &Log{path: path}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				l.recent = append(l.recent, e)
			}
		}
		f.Close()
		if len(l.recent) > 500 {
			l.recent = l.recent[len(l.recent)-500:]
		}
	}
	return l
}

func (l *Log) OnAdd(f func(Entry)) { l.onAdd = f }

func (l *Log) Add(e Entry) {
	if e.TS.IsZero() {
		e.TS = time.Now()
	}
	l.mu.Lock()
	l.recent = append(l.recent, e)
	if len(l.recent) > 500 {
		l.recent = l.recent[len(l.recent)-500:]
	}
	if l.path != "" {
		if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			b, _ := json.Marshal(e)
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	l.mu.Unlock()
	if l.onAdd != nil {
		l.onAdd(e)
	}
}

func (l *Log) Recent(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	if n <= 0 || n > len(l.recent) {
		n = len(l.recent)
	}
	out := make([]Entry, n)
	copy(out, l.recent[len(l.recent)-n:])
	return out
}
