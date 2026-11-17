// nexus-core (desk prototype): one local process serving the shell over a
// websocket contract and running the agents (home, per project, per task)
// through pi. Every backend choice here is a placeholder for UX testing.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

func main() {
	home, _ := os.UserHomeDir()
	var scan, addProjects multi
	addr := flag.String("addr", "127.0.0.1:8741", "listen address (keep it on loopback)")
	dataDir := flag.String("data", filepath.Join(home, ".local", "share", "nexus-desk"), "data directory")
	webDir := flag.String("web", "web", "web assets directory")
	skillsDir := flag.String("skills", "skills", "agent skills directory (SKILL.md per skill)")
	agentKind := flag.String("agent", "pi", "agent loop: pi | none")
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
	if *agentKind == "pi" {
		ext, _ := filepath.Abs("pi/nexus-tools.ts")
		piDir, _ := filepath.Abs("pi")
		cfg.Agents = piFactory{bin: *piBin, model: *model, builtins: *piBuiltins, addr: *addr, dataDir: *dataDir, ext: ext, piDir: piDir,
			byKind: map[string]string{app.KindHome: *modelHome, app.KindProject: *modelProject, app.KindTask: *modelTask}}
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
