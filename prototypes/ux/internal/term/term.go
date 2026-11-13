// Package term runs your terminals (a shell on a pty). The agent never types
// into them: it can propose a command (you press Run/Enter) and, if the pane
// is visible to it, read recent output.
package term

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/creack/pty"
)

type Term struct {
	ID       string
	ptmx     *os.File
	cmd      *exec.Cmd
	mu       sync.Mutex
	buf      []byte // ring of recent output
	Proposal string
	onOut    func(id string, data []byte)
	onExit   func(id string)
}

type Manager struct {
	mu     sync.Mutex
	terms  map[string]*Term
	OnOut  func(id string, data []byte)
	OnExit func(id string)
	seq    int
}

func NewManager() *Manager { return &Manager{terms: map[string]*Term{}} }

const ringMax = 64 * 1024

func (m *Manager) Start(dir string) (*Term, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NEXUS_TERM=1")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		return nil, fmt.Errorf("start shell: %w", err)
	}
	m.mu.Lock()
	m.seq++
	t := &Term{ID: fmt.Sprintf("t%d", m.seq), ptmx: ptmx, cmd: cmd, onOut: m.OnOut, onExit: m.OnExit}
	m.terms[t.ID] = t
	m.mu.Unlock()
	go t.read()
	return t, nil
}

func (t *Term) read() {
	b := make([]byte, 8192)
	for {
		n, err := t.ptmx.Read(b)
		if n > 0 {
			chunk := append([]byte(nil), b[:n]...)
			t.mu.Lock()
			t.buf = append(t.buf, chunk...)
			if len(t.buf) > ringMax {
				t.buf = t.buf[len(t.buf)-ringMax:]
			}
			t.mu.Unlock()
			if t.onOut != nil {
				t.onOut(t.ID, chunk)
			}
		}
		if err != nil {
			if t.onExit != nil {
				t.onExit(t.ID)
			}
			return
		}
	}
}

func (m *Manager) Get(id string) *Term {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.terms[id]
}

func (m *Manager) Close(id string) {
	m.mu.Lock()
	t := m.terms[id]
	delete(m.terms, id)
	m.mu.Unlock()
	if t != nil {
		t.ptmx.Close()
		if t.cmd.Process != nil {
			t.cmd.Process.Kill()
		}
	}
}

// Input is the user's keystrokes.
func (t *Term) Input(data []byte) error {
	_, err := t.ptmx.Write(data)
	return err
}

func (t *Term) Resize(rows, cols int) {
	if rows > 0 && cols > 0 {
		pty.Setsize(t.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	}
}

func (t *Term) Scrollback() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.buf...)
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b[()][A-Z0-9]|\r`)

// Tail returns the last n lines of output as plain text.
func (t *Term) Tail(n int) string {
	s := ansi.ReplaceAllString(string(t.Scrollback()), "")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
