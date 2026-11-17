// Package mcpserve is a tiny MCP server (stdio, JSON-RPC 2.0, one message
// per line) that hands an agent harness the Nexus tools of one agent. Like
// pi's tool extension it defines nothing itself: it fetches the agent's tool
// list from the core and forwards every call to it with the agent's token,
// so the core keeps deciding and policing what each agent can do.
//
// `nexus-core mcp` runs it; Claude Code starts it from the MCP config Nexus
// writes for each agent (NEXUS_URL and NEXUS_TOKEN in its environment).
package mcpserve

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Server struct {
	URL, Token string
	HTTP       *http.Client
}

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute} // some tools wait on the user (permission prompts)
}

// Serve reads requests from r and writes responses to w until r ends.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	enc := json.NewEncoder(w)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": rpcErr{-32700, "parse error"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // a notification (e.g. notifications/initialized): no reply
		}
		result, rerr := s.handle(req)
		resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rerr != nil {
			resp["error"] = rerr
		} else {
			resp["result"] = result
		}
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(req rpcReq) (any, *rpcErr) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "nexus", "version": "0.1"},
			"instructions":    "Nexus's tools for this agent. The Nexus core defines and polices them.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools, err := s.tools()
		if err != nil {
			return nil, &rpcErr{-32603, err.Error()}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, &rpcErr{-32602, "bad params"}
		}
		text, isErr, err := s.call(p.Name, p.Arguments)
		if err != nil {
			text, isErr = "error: "+err.Error(), true
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}, nil
	}
	return nil, &rpcErr{-32601, "method not found: " + req.Method}
}

// tools fetches this agent's tools from the core, as MCP tool definitions.
func (s *Server) tools() ([]map[string]any, error) {
	rq, _ := http.NewRequest("GET", s.URL+"/agent/tools", nil)
	rq.Header.Set("X-Nexus-Token", s.Token)
	resp, err := s.client().Do(rq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("core refused the tool list (%s)", resp.Status)
	}
	var defs []struct {
		Name        string         `json:"name"`
		Label       string         `json:"label"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&defs); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(defs))
	for _, d := range defs {
		schema := d.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		if r, ok := schema["required"]; ok && r == nil {
			delete(schema, "required") // some clients reject "required": null
		}
		out = append(out, map[string]any{"name": d.Name, "title": d.Label, "description": d.Description, "inputSchema": schema})
	}
	return out, nil
}

func (s *Server) call(name string, args map[string]any) (string, bool, error) {
	if args == nil {
		args = map[string]any{}
	}
	b, _ := json.Marshal(map[string]any{"name": name, "args": args})
	rq, _ := http.NewRequest("POST", s.URL+"/agent/call", bytes.NewReader(b))
	rq.Header.Set("X-Nexus-Token", s.Token)
	rq.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(rq)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", true, fmt.Errorf("core refused the call (%s)", resp.Status)
	}
	var out struct {
		Text    string `json:"text"`
		IsError bool   `json:"isError"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", true, err
	}
	return out.Text, out.IsError, nil
}
