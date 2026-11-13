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
)

type Roots struct {
	m     map[string]string
	order []string
}

func New() *Roots { return &Roots{m: map[string]string{}} }

func (r *Roots) Add(name, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	if _, ok := r.m[name]; !ok {
		r.order = append(r.order, name)
	}
	r.m[name] = abs
	return nil
}

func (r *Roots) Names() []string { return append([]string(nil), r.order...) }

// Resolve turns "root/rel/path" into an absolute path inside that root.
func (r *Roots) Resolve(p string) (string, error) {
	p = strings.TrimPrefix(filepath.ToSlash(p), "/")
	root, rel, _ := strings.Cut(p, "/")
	dir, ok := r.m[root]
	if !ok {
		if len(r.order) == 0 {
			return "", errors.New("no roots configured")
		}
		return "", fmt.Errorf("unknown root %q in %q (roots: %s)", root, p, strings.Join(r.order, ", "))
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
	dir, ok := r.m[root]
	if !ok {
		return "", fmt.Errorf("unknown root %q", root)
	}
	return filepath.Join(dir, ".nexus", kind, filepath.FromSlash(rel)+".json"), nil
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
		for _, n := range r.order {
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
