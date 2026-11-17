package mcpserve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCore answers /agent/tools and /agent/call for token "good" only.
func fakeCore(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Nexus-Token") != "good" {
			http.Error(w, "forbidden", 403)
			return
		}
		switch r.URL.Path {
		case "/agent/tools":
			w.Write([]byte(`[{"name":"home_show","label":"Home screen","description":"See it","parameters":{"type":"object","properties":{},"required":null}}]`))
		case "/agent/call":
			var req struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Name == "boom" {
				w.Write([]byte(`{"text":"error: nope","isError":true}`))
				return
			}
			b, _ := json.Marshal(map[string]any{"text": req.Name + " ran with " + req.Args["x"].(string)})
			w.Write(b)
		}
	}))
}

func run(t *testing.T, s *Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	var res []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		json.Unmarshal([]byte(l), &m)
		res = append(res, m)
	}
	return res
}

func TestServesTheCoresTools(t *testing.T) {
	core := fakeCore(t)
	defer core.Close()
	s := &Server{URL: core.URL, Token: "good"}
	res := run(t, s,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"home_show","arguments":{"x":"y"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"nope"}`)
	if len(res) != 5 {
		t.Fatalf("want 5 replies (no reply to the notification), got %d", len(res))
	}
	if res[0]["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Fatal("should echo the client's protocol version")
	}
	tool := res[1]["result"].(map[string]any)["tools"].([]any)[0].(map[string]any)
	if tool["name"] != "home_show" || tool["inputSchema"].(map[string]any)["required"] != nil {
		t.Fatalf("tool: %v", tool)
	}
	if _, has := tool["inputSchema"].(map[string]any)["required"]; has {
		t.Fatal("null required should be dropped")
	}
	call := res[2]["result"].(map[string]any)
	if call["content"].([]any)[0].(map[string]any)["text"] != "home_show ran with y" || call["isError"] != false {
		t.Fatalf("call: %v", call)
	}
	if res[3]["result"].(map[string]any)["isError"] != true {
		t.Fatal("a tool error should come back as isError")
	}
	if res[4]["error"] == nil {
		t.Fatal("unknown method should be an error")
	}
}

func TestABadTokenGetsNothing(t *testing.T) {
	core := fakeCore(t)
	defer core.Close()
	res := run(t, &Server{URL: core.URL, Token: "bad"}, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if res[0]["error"] == nil {
		t.Fatal("the core refused the token; the list should fail")
	}
}
