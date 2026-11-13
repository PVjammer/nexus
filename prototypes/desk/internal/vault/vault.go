// Package vault is the sensitive-data plane for the prototype.
//
//   - Items: a test vault file of named values. The agent can list names, and
//     a secure tool delivers a value straight to a pane (the agent gets an ack).
//   - Handles: values you type into a secret-entry pane. The agent gets the
//     handle and can pass it to broker-backed tools; it never sees the value.
//   - Connections: broker-held credentials for plugin/agent HTTP calls.
//
// Values live only in memory here (plus the vault file you control). They are
// never written to the action log, the agent wire, or pane props.
package vault

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

type Item struct {
	Name  string `json:"name"`
	Kind  string `json:"kind,omitempty"` // password, document-number, code...
	Value string `json:"value"`
}

type Handle struct {
	ID      string    `json:"id"`
	Label   string    `json:"label"`
	Purpose string    `json:"purpose,omitempty"`
	Ready   bool      `json:"ready"`
	Created time.Time `json:"created"`
	value   string
}

type Vault struct {
	mu       sync.Mutex
	items    map[string]Item
	handles  map[string]*Handle
	delivery map[string]string // paneID -> value delivered out of band
}

func Open(path string) (*Vault, error) {
	v := &Vault{items: map[string]Item{}, handles: map[string]*Handle{}, delivery: map[string]string{}}
	if path == "" {
		return v, nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return v, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, fmt.Errorf("vault %s: %w", path, err)
	}
	for _, it := range items {
		v.items[it.Name] = it
	}
	return v, nil
}

// Names is what the agent may see.
func (v *Vault) Names() []map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []map[string]string
	for _, it := range v.items {
		out = append(out, map[string]string{"name": it.Name, "kind": it.Kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

func (v *Vault) Get(name string) (Item, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	it, ok := v.items[name]
	return it, ok
}

// Deliver records a value delivered to a pane (so a reconnecting client can
// re-render it). Only the pane's client receives it.
func (v *Vault) Deliver(paneID, value string) {
	v.mu.Lock()
	v.delivery[paneID] = value
	v.mu.Unlock()
}

func (v *Vault) Delivered(paneID string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.delivery[paneID]
	return s, ok
}

func (v *Vault) Forget(paneID string) {
	v.mu.Lock()
	delete(v.delivery, paneID)
	v.mu.Unlock()
}

func (v *Vault) NewHandle(label, purpose string) *Handle {
	b := make([]byte, 4)
	rand.Read(b)
	h := &Handle{ID: "secret:" + hex.EncodeToString(b), Label: label, Purpose: purpose, Created: time.Now()}
	v.mu.Lock()
	v.handles[h.ID] = h
	v.mu.Unlock()
	return h
}

func (v *Vault) Fill(id, value string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	h := v.handles[id]
	if h == nil {
		return fmt.Errorf("no handle %s", id)
	}
	if value == "" {
		return fmt.Errorf("empty value")
	}
	h.value, h.Ready = value, true
	return nil
}

func (v *Vault) HandleInfo(id string) (Handle, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	h := v.handles[id]
	if h == nil {
		return Handle{}, false
	}
	return *h, true
}

// Use resolves a handle's value for a broker-side call. Never return this to
// the agent.
func (v *Vault) Use(id string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	h := v.handles[id]
	if h == nil {
		return "", fmt.Errorf("no handle %s", id)
	}
	if !h.Ready {
		return "", fmt.Errorf("handle %s is still waiting for the user to enter the value", id)
	}
	return h.value, nil
}
