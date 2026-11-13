// Package broker holds credentials and makes outbound calls on behalf of the
// agent and plugins. Callers name a connection; they never hold the token.
//
// Prototype: one in-process mock ("homelab-mock") so demos need no network.
// Real connections would be HTTP with an allowlist of methods/paths.
package broker

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

type Connection struct {
	Name        string
	Description string
	Paths       []string // allowlisted paths
	do          func(path, token string) (any, error)
	token       string // broker-held default credential
}

type Broker struct {
	conns map[string]*Connection
}

func New() *Broker {
	b := &Broker{conns: map[string]*Connection{}}
	b.add(&Connection{
		Name:        "homelab-mock",
		Description: "Fake homelab API: /disks (usage per disk), /services (status), /whoami (checks auth)",
		Paths:       []string{"/disks", "/services", "/whoami"},
		token:       "broker-held-mock-token",
		do:          homelabMock,
	})
	return b
}

func (b *Broker) add(c *Connection) { b.conns[c.Name] = c }

func (b *Broker) List() []map[string]any {
	var out []map[string]any
	for _, c := range b.conns {
		out = append(out, map[string]any{"name": c.Name, "description": c.Description, "paths": c.Paths})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return out
}

func (b *Broker) Has(name string) bool { _, ok := b.conns[name]; return ok }

// Call performs a call. token overrides the broker's credential (e.g. a value
// the user entered via a secret handle, resolved by the caller server-side).
func (b *Broker) Call(name, path, token string) (any, error) {
	c := b.conns[name]
	if c == nil {
		return nil, fmt.Errorf("unknown connection %q", name)
	}
	ok := false
	for _, p := range c.Paths {
		if path == p || strings.HasPrefix(path, p+"?") {
			ok = true
		}
	}
	if !ok {
		return nil, fmt.Errorf("path %q is not allowlisted for %s (allowed: %s)", path, name, strings.Join(c.Paths, ", "))
	}
	if token == "" {
		token = c.token
	}
	return c.do(path, token)
}

func homelabMock(path, token string) (any, error) {
	if token == "" {
		return nil, fmt.Errorf("401 unauthorized")
	}
	switch strings.SplitN(path, "?", 2)[0] {
	case "/whoami":
		// Never echo the token back, not even partially.
		return map[string]any{"status": "auth ok", "user": "nick", "token_length": len(token)}, nil
	case "/disks":
		r := rand.New(rand.NewSource(time.Now().Unix() / 30))
		disks := []map[string]any{}
		for _, d := range []struct {
			name string
			size float64
		}{{"nvme0 (system)", 1000}, {"tank (zfs)", 16000}, {"backup (usb)", 8000}, {"scratch", 2000}} {
			used := d.size * (0.2 + 0.75*r.Float64())
			disks = append(disks, map[string]any{"name": d.name, "size_gb": d.size, "used_gb": float64(int(used))})
		}
		return map[string]any{"host": "homelab", "disks": disks}, nil
	case "/services":
		return map[string]any{"services": []map[string]any{
			{"name": "jellyfin", "status": "up"}, {"name": "immich", "status": "up"},
			{"name": "paperless", "status": "degraded"}, {"name": "backup-cron", "status": "up"},
		}}, nil
	}
	return nil, fmt.Errorf("404")
}

// JSON renders a result for tool output.
func JSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
