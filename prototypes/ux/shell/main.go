// nexus is the desktop shell for the UX prototype: a native window (system
// webview: WebKitGTK on Linux, WKWebView on macOS, WebView2 on Windows) onto
// the core's contract. It starts the core if one isn't already running.
//
// Placeholder: the shell choice (webview vs Wails vs Tauri) is not settled.
// The UI talks only to the core's websocket contract, so swapping the shell
// doesn't touch it.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	webview "github.com/webview/webview_go"
)

func main() {
	home, _ := os.UserHomeDir()
	addr := flag.String("addr", "127.0.0.1:8740", "core address")
	data := flag.String("data", filepath.Join(home, ".local", "share", "nexus-ux"), "core data directory")
	core := flag.String("core", "bin/nexus-core", "core binary (started if no core is listening)")
	debug := flag.Bool("devtools", false, "enable the webview inspector")
	flag.Parse()
	coreArgs := flag.Args() // passed through to the core, e.g. -- -root docs=../../docs

	var child *exec.Cmd
	if !healthy(*addr) {
		child = exec.Command(*core, append([]string{"-addr", *addr, "-data", *data}, coreArgs...)...)
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			log.Fatalf("start core: %v", err)
		}
		for i := 0; i < 50 && !healthy(*addr); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if !healthy(*addr) {
			log.Fatal("core didn't come up; see its output above")
		}
	}
	token := readToken(filepath.Join(*data, "token"))

	w := webview.New(*debug)
	defer w.Destroy()
	w.SetTitle("Nexus")
	w.SetSize(1500, 950, webview.HintNone)
	w.Navigate(fmt.Sprintf("http://%s/#token=%s", *addr, token))
	w.Run()

	if child != nil && child.Process != nil {
		child.Process.Signal(os.Interrupt)
		time.Sleep(200 * time.Millisecond)
		child.Process.Kill()
	}
}

func healthy(addr string) bool {
	c := http.Client{Timeout: 300 * time.Millisecond}
	r, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == 200
}

func readToken(p string) string {
	f, err := os.Open(p)
	if err != nil {
		log.Fatalf("read token %s: %v", p, err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Scan()
	return strings.TrimSpace(s.Text())
}
