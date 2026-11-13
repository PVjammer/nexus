// Package plugins stores plugins vibed on the spot (tier 2b): a manifest plus
// one HTML file, run in a sandboxed iframe with a default-deny CSP. The only
// way out is the host bridge, which checks the manifest (declared?) and your
// grants (approved?) on every call.
package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/pvjammer/nexus/prototypes/ux/internal/files"
)

// Permissions a plugin may declare. Each is granted by you once.
//
//	connect:<connection>   call a broker connection (no raw network, ever)
//	tool:<name>            call one of the app tools exposed to plugins
//	context                publish text the agent can see (update model context)
type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
}

type Plugin struct {
	Manifest
	Kept    bool              `json:"kept"` // false = draft (vibed, not yet kept)
	Version int               `json:"version"`
	Hash    string            `json:"hash"`
	Updated time.Time         `json:"updated"`
	Grants  map[string]string `json:"grants"` // perm -> "granted" | "denied"
	HTML    string            `json:"-"`
}

type Store struct {
	dir string
	mu  sync.Mutex
	m   map[string]*Plugin
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)
var permRe = regexp.MustCompile(`^(connect:[a-z0-9-]+|tool:[a-z_]+|context)$`)

// Tools a plugin may ask for. Deliberately small; never pane_read or vault.
var PluginTools = map[string]bool{"sheet_read": true, "sheet_edit": true, "files_list": true}

func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, m: map[string]*Plugin{}}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		var p Plugin
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), "plugin.json"))
		if err != nil || json.Unmarshal(b, &p) != nil {
			continue
		}
		h, err := os.ReadFile(filepath.Join(dir, e.Name(), "index.html"))
		if err != nil {
			continue
		}
		p.HTML = string(h)
		if p.Grants == nil {
			p.Grants = map[string]string{}
		}
		if !p.Kept {
			continue // drafts don't survive a restart
		}
		s.m[p.ID] = &p
	}
	return s, nil
}

func (s *Store) saveLocked(p *Plugin) error {
	d := filepath.Join(s.dir, p.ID)
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := files.WriteAtomic(filepath.Join(d, "index.html"), []byte(p.HTML)); err != nil {
		return err
	}
	return files.WriteAtomic(filepath.Join(d, "plugin.json"), b)
}

// Write creates or replaces a plugin (hot reload). Grants carry over for
// permissions that are still declared; new ones start ungranted.
func (s *Store) Write(m Manifest, html string) (*Plugin, error) {
	if !idRe.MatchString(m.ID) {
		return nil, fmt.Errorf("plugin id %q must be lowercase letters, digits and dashes", m.ID)
	}
	if m.Name == "" {
		m.Name = m.ID
	}
	for _, p := range m.Permissions {
		if !permRe.MatchString(p) {
			return nil, fmt.Errorf("unknown permission %q (use connect:<name>, tool:<name>, or context)", p)
		}
		if len(p) > 5 && p[:5] == "tool:" && !PluginTools[p[5:]] {
			return nil, fmt.Errorf("tool %q isn't available to plugins (available: sheet_read, sheet_edit, files_list)", p[5:])
		}
	}
	if len(html) > 512*1024 {
		return nil, errors.New("plugin html too large (512KB max)")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.m[m.ID]
	p := &Plugin{Manifest: m, HTML: html, Updated: time.Now(), Grants: map[string]string{}}
	if old != nil {
		p.Kept = old.Kept
		p.Version = old.Version
		for _, perm := range m.Permissions {
			if g, ok := old.Grants[perm]; ok {
				p.Grants[perm] = g
			}
		}
	}
	p.Version++
	h := sha256.Sum256([]byte(html))
	p.Hash = hex.EncodeToString(h[:6])
	s.m[m.ID] = p
	return p, s.saveLocked(p)
}

func (s *Store) Get(id string) (*Plugin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.m[id]
	if !ok {
		return nil, false
	}
	cp := *p
	cp.Grants = map[string]string{}
	for k, v := range p.Grants {
		cp.Grants[k] = v
	}
	return &cp, true
}

func (s *Store) List() []*Plugin {
	s.mu.Lock()
	ids := make([]string, 0, len(s.m))
	for id := range s.m {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	sort.Strings(ids)
	var out []*Plugin
	for _, id := range ids {
		p, _ := s.Get(id)
		out = append(out, p)
	}
	return out
}

func (s *Store) Keep(id string, keep bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.m[id]
	if p == nil {
		return fmt.Errorf("no plugin %s", id)
	}
	p.Kept = keep
	return s.saveLocked(p)
}

func (s *Store) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	os.RemoveAll(filepath.Join(s.dir, id))
}

func (s *Store) SetGrant(id, perm, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.m[id]
	if p == nil {
		return fmt.Errorf("no plugin %s", id)
	}
	p.Grants[perm] = val
	return s.saveLocked(p)
}

// Check reports whether perm is declared and its grant status
// ("granted", "denied", or "" = not asked yet).
func (p *Plugin) Check(perm string) (declared bool, grant string) {
	for _, d := range p.Permissions {
		if d == perm {
			return true, p.Grants[perm]
		}
	}
	return false, ""
}
