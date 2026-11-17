// nexus-core (desk prototype): one local process serving the shell over a
// websocket contract and running the agents (home, per project, per task)
// through pi or Claude Code (chosen per agent kind). Every backend choice
// here is a placeholder for UX testing.
//
// `nexus-core mcp` is the MCP server Claude Code agents get Nexus tools from.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pvjammer/nexus/prototypes/desk/internal/agent"
	"github.com/pvjammer/nexus/prototypes/desk/internal/app"
	"github.com/pvjammer/nexus/prototypes/desk/internal/mcpserve"
	"github.com/pvjammer/nexus/prototypes/desk/internal/vault"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

// piFactory starts one `pi --mode rpc` per agent, each with its own token,
// instructions and session directory.
type piFactory struct {
	bin, model, builtins, addr, dataDir, ext, piDir string
	byKind                                         map[string]string // agent kind -> "provider/id[:thinking]"
}

func (f piFactory) modelFor(kind string) string {
	if m := f.byKind[kind]; m != "" {
		return m
	}
	return f.model
}

func (f piFactory) Name() string {
	parts := []string{}
	for _, k := range []string{app.KindHome, app.KindProject, app.KindTask} {
		m := f.modelFor(k)
		if m == "" {
			m = "pi default"
		}
		parts = append(parts, k+": "+m)
	}
	return "pi · " + strings.Join(parts, " · ")
}

func (f piFactory) Start(ctx context.Context, s app.RunSpec) (agent.Backend, error) {
	safe := strings.NewReplacer(":", "-", "/", "-").Replace(s.ID)
	sessDir := filepath.Join(f.dataDir, "pi-sessions", safe)
	prior, _ := filepath.Glob(filepath.Join(sessDir, "*.jsonl"))
	sys, err := composeInstructions(f.dataDir, safe, filepath.Join(f.piDir, s.Kind+".md"), s.Instructions)
	if err != nil {
		return nil, err
	}
	p := agent.NewPi(agent.PiConfig{
		Continue: len(prior) > 0, // an agent that was stopped picks up where it left off
		Binary: f.bin, Model: f.modelFor(s.Kind), Extension: f.ext,
		SystemExtra:  sys,
		SessionDir:   sessDir,
		WorkDir:      s.WorkDir,
		BuiltinTools: f.builtins,
		WireLog:      filepath.Join(f.dataDir, "agent-wire", safe+".log"),
		Env:          []string{"NEXUS_URL=http://" + f.addr, "NEXUS_TOKEN=" + s.Token},
	})
	os.MkdirAll(filepath.Join(f.dataDir, "agent-wire"), 0o700)
	if err := p.Start(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// composeInstructions writes the agent's instructions (its kind's file plus
// its skills) to one file in the data directory and returns the path.
func composeInstructions(dataDir, safe, base, extra string) (string, error) {
	if extra == "" {
		return base, nil
	}
	b, err := os.ReadFile(base)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(dataDir, "prompts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, safe+".md")
	return p, os.WriteFile(p, []byte(strings.TrimSpace(string(b))+"\n\n"+extra+"\n"), 0o600)
}

// claudeFactory starts one headless Claude Code per agent (no SDK; your own
// Claude Code login). Its only tools are Nexus's, through `nexus-core mcp`.
type claudeFactory struct {
	bin, addr, dataDir, piDir, self string
	model                           string
	byKind                          map[string]string
}

func (f claudeFactory) modelFor(kind string) string {
	if m := f.byKind[kind]; m != "" {
		return m
	}
	return f.model
}

func (f claudeFactory) Name() string {
	m := f.model
	if m == "" {
		m = "default model"
	}
	return "claude code · " + m
}

const claudeNote = "You run inside Nexus through Claude Code. Your tools come from the \"nexus\" MCP server, so their names start with mcp__nexus__ (mcp__nexus__home_add is the home_add these instructions mention). You have no other tools: no shell, no file editing, no web."

func (f claudeFactory) Start(ctx context.Context, s app.RunSpec) (agent.Backend, error) {
	safe := strings.NewReplacer(":", "-", "/", "-").Replace(s.ID)
	dir := filepath.Join(f.dataDir, "claude", safe)
	cwd := filepath.Join(dir, "cwd") // empty: no project CLAUDE.md or settings to pick up
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		return nil, err
	}
	sys, err := composeInstructions(f.dataDir, "claude-"+safe, filepath.Join(f.piDir, s.Kind+".md"), claudeNote+"\n\n"+s.Instructions)
	if err != nil {
		return nil, err
	}
	mcp := filepath.Join(dir, "mcp.json")
	cfg := map[string]any{"mcpServers": map[string]any{"nexus": map[string]any{
		"type": "stdio", "command": f.self, "args": []string{"mcp"},
		"env": map[string]string{"NEXUS_URL": "http://" + f.addr, "NEXUS_TOKEN": s.Token},
	}}}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(mcp, b, 0o600); err != nil {
		return nil, err
	}
	// One conversation per agent, resumed after a restart (like pi's --continue).
	sessFile := filepath.Join(dir, "session")
	sid, resume := "", false
	if b, err := os.ReadFile(sessFile); err == nil {
		sid = strings.TrimSpace(string(b))
		home, _ := os.UserHomeDir()
		found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sid+".jsonl"))
		resume = len(found) > 0
	}
	if !resume {
		sid = newUUID()
		os.WriteFile(sessFile, []byte(sid), 0o600)
	}
	os.MkdirAll(filepath.Join(f.dataDir, "agent-wire"), 0o700)
	c := agent.NewClaude(agent.ClaudeConfig{
		Binary: f.bin, Model: f.modelFor(s.Kind), SystemFile: sys, MCPConfig: mcp,
		SessionID: sid, Resume: resume, WorkDir: cwd,
		WireLog: filepath.Join(f.dataDir, "agent-wire", "claude-"+safe+".log"),
	})
	if err := c.Start(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// mixFactory picks the agent loop per agent kind (home, project, task).
type mixFactory struct {
	byKind map[string]app.AgentFactory
	order  []string
}

func (m mixFactory) Start(ctx context.Context, s app.RunSpec) (agent.Backend, error) {
	f := m.byKind[s.Kind]
	if f == nil {
		return nil, fmt.Errorf("no agent loop for %s agents", s.Kind)
	}
	return f.Start(ctx, s)
}

func (m mixFactory) Name() string {
	var parts []string
	seen := map[string]bool{}
	for _, k := range m.order {
		if f := m.byKind[k]; f != nil && !seen[f.Name()] {
			seen[f.Name()] = true
			parts = append(parts, f.Name())
		}
	}
	return strings.Join(parts, " | ")
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		s := &mcpserve.Server{URL: os.Getenv("NEXUS_URL"), Token: os.Getenv("NEXUS_TOKEN")}
		if err := s.Serve(os.Stdin, os.Stdout); err != nil {
			log.Fatal(err)
		}
		return
	}
	home, _ := os.UserHomeDir()
	var scan, addProjects multi
	addr := flag.String("addr", "127.0.0.1:8741", "listen address (keep it on loopback)")
	dataDir := flag.String("data", filepath.Join(home, ".local", "share", "nexus-desk"), "data directory")
	webDir := flag.String("web", "web", "web assets directory")
	skillsDir := flag.String("skills", "skills", "agent skills directory (SKILL.md per skill)")
	agentKind := flag.String("agent", "pi", "agent loop for every agent: pi | claude | none")
	agentHome := flag.String("agent-home", "", "agent loop for the home agent (overrides -agent): pi | claude")
	agentProject := flag.String("agent-project", "", "agent loop for project agents: pi | claude")
	agentTask := flag.String("agent-task", "", "agent loop for task agents: pi | claude")
	claudeBin := flag.String("claude", "claude", "Claude Code binary (headless; uses your Claude Code login)")
	claudeModel := flag.String("claude-model", "", "Claude Code model for every agent, e.g. sonnet or opus (default: Claude Code's)")
	claudeModelHome := flag.String("claude-model-home", "", "Claude Code model for the home agent")
	claudeModelProject := flag.String("claude-model-project", "", "Claude Code model for project agents")
	claudeModelTask := flag.String("claude-model-task", "", "Claude Code model for task agents")
	model := flag.String("model", "", "pi model for every agent, as provider/id[:thinking] (default: pi's configured default)")
	modelHome := flag.String("model-home", "", "model for the home agent (overrides -model), e.g. spark/Qwen3.6-35B-A3B:low")
	modelProject := flag.String("model-project", "", "model for project agents (overrides -model)")
	modelTask := flag.String("model-task", "", "model for dispatched task agents (overrides -model), e.g. spark/Qwen3.8-27B:high")
	piBin := flag.String("pi", "pi", "pi binary")
	piBuiltins := flag.String("pi-builtins", "", "pi built-in tools to allow (default none: agents only use app tools)")
	vaultPath := flag.String("vault", "testdata/vault.json", "test vault file (fake values only!)")
	testHooks := flag.Bool("test-hooks", false, "e2e only: expose /test/agent-token")
	flag.Var(&scan, "scan", "folder onboarding scans for projects (repeatable; default ~/Workspace)")
	flag.Var(&addProjects, "project", "add a project at startup: path or name=path (repeatable)")
	flag.Parse()
	if len(scan) == 0 {
		scan = multi{filepath.Join(home, "Workspace")}
	}

	must(os.MkdirAll(*dataDir, 0o700))
	token := loadToken(filepath.Join(*dataDir, "token"))
	v, err := vault.Open(*vaultPath)
	must(err)

	cfg := app.Config{DataDir: *dataDir, WebDir: *webDir, SkillsDir: *skillsDir, Token: token, ScanRoots: scan, TestHooks: *testHooks}
	if *agentKind != "none" || *agentHome != "" || *agentProject != "" || *agentTask != "" {
		ext, _ := filepath.Abs("pi/nexus-tools.ts")
		piDir, _ := filepath.Abs("pi")
		self, _ := os.Executable()
		if abs, err := filepath.Abs(self); err == nil {
			self = abs
		}
		loops := map[string]app.AgentFactory{
			"pi": piFactory{bin: *piBin, model: *model, builtins: *piBuiltins, addr: *addr, dataDir: *dataDir, ext: ext, piDir: piDir,
				byKind: map[string]string{app.KindHome: *modelHome, app.KindProject: *modelProject, app.KindTask: *modelTask}},
			"claude": claudeFactory{bin: *claudeBin, addr: *addr, dataDir: *dataDir, piDir: piDir, self: self, model: *claudeModel,
				byKind: map[string]string{app.KindHome: *claudeModelHome, app.KindProject: *claudeModelProject, app.KindTask: *claudeModelTask}},
		}
		pick := func(v string) app.AgentFactory {
			if v == "" {
				v = *agentKind
			}
			f, ok := loops[v]
			if !ok && v != "none" {
				log.Fatalf("unknown agent loop %q (pi, claude or none)", v)
			}
			return f
		}
		cfg.Agents = mixFactory{order: []string{app.KindHome, app.KindProject, app.KindTask}, byKind: map[string]app.AgentFactory{
			app.KindHome: pick(*agentHome), app.KindProject: pick(*agentProject), app.KindTask: pick(*agentTask)}}
	}
	a, err := app.New(cfg, v)
	must(err)
	for _, spec := range addProjects {
		name, p, ok := strings.Cut(spec, "=")
		if !ok {
			name, p = "", spec
		}
		if _, err := a.AddProject(name, p); err != nil && !strings.Contains(err.Error(), "already added") {
			log.Printf("-project %s: %v", spec, err)
		}
	}

	url := fmt.Sprintf("http://%s/#token=%s", *addr, token)
	ln, err := net.Listen("tcp", *addr)
	must(err)
	log.Printf("nexus desk on %s", url)
	fmt.Println("NEXUS_URL=" + url)
	log.Fatal(http.Serve(ln, a.Handler()))
}

func loadToken(p string) string {
	if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
		return strings.TrimSpace(string(b))
	}
	b := make([]byte, 16)
	rand.Read(b)
	t := hex.EncodeToString(b)
	must(os.WriteFile(p, []byte(t), 0o600))
	return t
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
