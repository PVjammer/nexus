// nexus-core is the prototype core: one local process serving the shell over
// a websocket contract and driving the agent loop (pi). Every backend choice
// here is a placeholder for UX testing, not a decision.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/pvjammer/nexus/prototypes/ux/internal/agent"
	"github.com/pvjammer/nexus/prototypes/ux/internal/app"
	"github.com/pvjammer/nexus/prototypes/ux/internal/files"
	"github.com/pvjammer/nexus/prototypes/ux/internal/vault"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	home, _ := os.UserHomeDir()
	var roots multi
	addr := flag.String("addr", "127.0.0.1:8740", "listen address (keep it on loopback)")
	dataDir := flag.String("data", filepath.Join(home, ".local", "share", "nexus-ux"), "data directory")
	webDir := flag.String("web", "web", "web assets directory")
	agentKind := flag.String("agent", "pi", "agent loop: pi | none")
	model := flag.String("model", "", "pi model as provider/id (default: pi's configured default)")
	piBin := flag.String("pi", "pi", "pi binary")
	piBuiltins := flag.String("pi-builtins", "", "pi built-in tools to allow (default none: the agent only uses app tools)")
	vaultPath := flag.String("vault", "testdata/vault.json", "test vault file (fake values only!)")
	seed := flag.String("seed", "testdata/workspace", "copy these sample files into the workspace root if it is empty")
	flag.Var(&roots, "root", "extra file root name=path (repeatable), e.g. -root docs=../../docs")
	flag.Parse()

	must(os.MkdirAll(*dataDir, 0o700))
	token := loadToken(filepath.Join(*dataDir, "token"))

	r := files.New()
	ws := filepath.Join(*dataDir, "workspace")
	must(r.Add("workspace", ws))
	for _, spec := range roots {
		name, p, ok := strings.Cut(spec, "=")
		if !ok {
			log.Fatalf("-root wants name=path, got %q", spec)
		}
		must(r.Add(name, p))
	}
	if *seed != "" {
		seedWorkspace(*seed, ws)
	}
	v, err := vault.Open(*vaultPath)
	must(err)

	a, err := app.New(app.Config{DataDir: *dataDir, WebDir: *webDir, Token: token, TermDir: ws}, r, v)
	must(err)

	url := fmt.Sprintf("http://%s/#token=%s", *addr, token)
	// Listen before starting the agent: pi's tool extension fetches the tool list from us.
	ln, err := net.Listen("tcp", *addr)
	must(err)
	go func() { log.Fatal(http.Serve(ln, a.Handler())) }()
	if *agentKind == "pi" {
		ext, _ := filepath.Abs("pi/nexus-tools.ts")
		sys, _ := filepath.Abs("pi/system.md")
		pi := agent.NewPi(agent.PiConfig{
			Binary: *piBin, Model: *model, Extension: ext, SystemExtra: sys,
			SessionDir: filepath.Join(*dataDir, "pi-sessions"), WorkDir: ws, BuiltinTools: *piBuiltins,
			WireLog: filepath.Join(*dataDir, "agent-wire.log"),
			Env:     []string{"NEXUS_URL=http://" + *addr, "NEXUS_TOKEN=" + token},
		})
		if err := a.StartAgent(context.Background(), pi); err != nil {
			log.Printf("agent failed to start: %v (the app still runs; chat is disabled)", err)
		}
	}
	log.Printf("nexus core on %s", url)
	fmt.Println("NEXUS_URL=" + url)
	select {}

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

func seedWorkspace(src, dst string) {
	ents, _ := os.ReadDir(dst)
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), ".") {
			return
		}
	}
	filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			os.MkdirAll(out, 0o755)
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return nil
		}
		defer in.Close()
		f, err := os.Create(out)
		if err != nil {
			return nil
		}
		defer f.Close()
		io.Copy(f, in)
		return nil
	})
	log.Printf("seeded workspace from %s", src)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
