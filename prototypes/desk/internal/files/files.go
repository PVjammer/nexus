// Package files maps app paths ("<root>/<rel>") to disk, confined to named
// roots. Everything the panes and agent tools touch goes through here.
// In the full design this sits behind a node; here it is in-process.
package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Roots struct {
	mu       sync.RWMutex
	m        map[string]string
	order    []string
	readOnly map[string]bool // agents may not write here (your repos)
	meta     string          // where sidecars (comments) live: never inside your repos
}

// New makes an empty set of roots. meta is the directory for app metadata
// about files (review comments), kept outside the roots themselves.
func New(meta string) *Roots {
	return &Roots{m: map[string]string{}, readOnly: map[string]bool{}, meta: meta}
}

// Add registers a root. readOnly roots are read-only for agents (the user
// can still edit through the UI).
func (r *Roots) Add(name, dir string, readOnly bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if readOnly {
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return fmt.Errorf("%s is not a directory", abs)
		}
	} else if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.m[name]; !ok {
		r.order = append(r.order, name)
	}
	r.m[name] = abs
	r.readOnly[name] = readOnly
	return nil
}

func (r *Roots) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, name)
	delete(r.readOnly, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

func (r *Roots) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Dir returns a root's absolute directory.
func (r *Roots) Dir(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.m[name]
	return d, ok
}

// AgentWritable reports whether an agent may write to this app path.
func (r *Roots) AgentWritable(p string) bool {
	root, _, _ := strings.Cut(strings.TrimPrefix(filepath.ToSlash(p), "/"), "/")
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.m[root]
	return ok && !r.readOnly[root]
}

// LooksSecret flags files that commonly hold credentials. Agents can't read
// them; the vault/broker is the way to share a secret.
func LooksSecret(name string) bool {
	b := strings.ToLower(filepath.Base(name))
	switch {
	case b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env"):
		return !strings.HasSuffix(b, ".example") && !strings.HasSuffix(b, ".sample") && !strings.HasSuffix(b, ".template")
	case strings.HasSuffix(b, ".pem"), strings.HasSuffix(b, ".key"), strings.HasSuffix(b, ".p12"), strings.HasSuffix(b, ".pfx"),
		strings.HasPrefix(b, "id_rsa"), strings.HasPrefix(b, "id_ed25519"), strings.HasPrefix(b, "id_ecdsa"),
		b == ".npmrc", b == ".netrc", b == ".pypirc", b == "kubeconfig", b == "credentials", b == "credentials.json",
		strings.HasSuffix(b, ".tfvars"), b == "secrets.yaml", b == "secrets.yml", b == "vault.json", b == "token":
		return true
	}
	return false
}

// Resolve turns "root/rel/path" into an absolute path inside that root.
func (r *Roots) Resolve(p string) (string, error) {
	p = strings.TrimPrefix(filepath.ToSlash(p), "/")
	root, rel, _ := strings.Cut(p, "/")
	r.mu.RLock()
	dir, ok := r.m[root]
	r.mu.RUnlock()
	if !ok {
		if len(r.Names()) == 0 {
			return "", errors.New("no roots configured")
		}
		return "", fmt.Errorf("unknown root %q in %q (roots: %s)", root, p, strings.Join(r.Names(), ", "))
	}
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if abs != dir && !strings.HasPrefix(abs, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes root %q", p, root)
	}
	if strings.Contains(rel, "/.nexus/") || strings.HasPrefix(rel, ".nexus") {
		return "", errors.New("the .nexus directory is internal")
	}
	return abs, nil
}

// Sidecar returns the path for app metadata about a file (comments, etc.).
func (r *Roots) Sidecar(p, kind string) (string, error) {
	p = strings.TrimPrefix(filepath.ToSlash(p), "/")
	root, rel, _ := strings.Cut(p, "/")
	if _, ok := r.Dir(root); !ok {
		return "", fmt.Errorf("unknown root %q", root)
	}
	return filepath.Join(r.meta, kind, root, filepath.FromSlash(rel)+".json"), nil
}

type Entry struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
	Size int64  `json:"size,omitempty"`
}

// List lists files under a root path (non-recursive), hiding .nexus and dotfiles.
func (r *Roots) List(p string) ([]Entry, error) {
	if p == "" || p == "/" {
		var out []Entry
		for _, n := range r.Names() {
			out = append(out, Entry{Path: n, Dir: true})
		}
		return out, nil
	}
	abs, err := r.Resolve(p)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
			continue
		}
		info, _ := e.Info()
		var size int64
		if info != nil {
			size = info.Size()
		}
		out = append(out, Entry{Path: strings.TrimSuffix(p, "/") + "/" + e.Name(), Dir: e.IsDir(), Size: size})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// WriteAtomic writes via temp file + rename.
func WriteAtomic(abs string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp := abs + ".nexus-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, abs)
}
