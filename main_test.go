package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/tools"
)

// TestHTTP prueba el modo --http sin red: /health y el handshake MCP + tools/list por Streamable HTTP.
func TestHTTP(t *testing.T) {
	srv := httptest.NewServer(httpHandler(server.NewStreamableHTTPServer(tools.NewServer(tools.DefaultDeps(version, userAgent())))))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("/health = %d %s", resp.StatusCode, body)
	}

	post := func(session, payload string) (*http.Response, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp", strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out map[string]any
		if len(b) > 0 {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("respuesta no JSON (%d): %s", resp.StatusCode, b)
			}
		}
		return resp, out
	}

	resp, init := post("", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`)
	session := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != 200 || session == "" || init["result"].(map[string]any)["serverInfo"].(map[string]any)["name"] != "datos-gob-cl" {
		t.Fatalf("initialize = %d session=%q %v", resp.StatusCode, session, init)
	}
	post(session, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_, list := post(session, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if ts := list["result"].(map[string]any)["tools"].([]any); len(ts) != 14 {
		t.Errorf("tools/list = %d tools", len(ts))
	}

	if resp, _ := http.Get(srv.URL + "/otra"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("/otra = %d", resp.StatusCode)
	}
}

func TestUserAgent(t *testing.T) {
	if ua := userAgent(); !strings.HasPrefix(ua, "mcp-datos-gob-cl/") || !strings.Contains(ua, "github.com/MarcoValdiviesoB") {
		t.Errorf("userAgent = %q", ua)
	}
}
