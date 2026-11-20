package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestForkSourceCutsAtTheLastFinishedTurn(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "s.jsonl"), filepath.Join(dir, "f.jsonl")
	lines := []string{
		`{"type":"session","id":"s"}`,
		`{"type":"message","id":"1","message":{"role":"user"}}`,
		`{"type":"message","id":"2","message":{"role":"assistant","stopReason":"toolUse"}}`,
		`{"type":"message","id":"3","message":{"role":"toolResult"}}`,
		`{"type":"message","id":"4","message":{"role":"assistant","stopReason":"stop"}}`,
		`{"type":"message","id":"5","message":{"role":"user"}}`,
		`{"type":"message","id":"6","message":{"role":"assistant","stopReason":"toolUse"}}`,
	}
	os.WriteFile(src, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	ok, err := ForkSource(src, dst)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	b, _ := os.ReadFile(dst)
	if got := strings.TrimSpace(string(b)); got != strings.Join(lines[:5], "\n") {
		t.Fatalf("mid-turn entries must be cut:\n%s", got)
	}
	os.WriteFile(src, []byte(strings.Join(lines[:2], "\n")+"\n"), 0o600)
	if ok, _ := ForkSource(src, dst); ok {
		t.Fatal("nothing finished yet: nothing to fork")
	}
}
