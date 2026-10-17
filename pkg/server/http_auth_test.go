package server

import (
	"context"
	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/GauranshMathur/ARR_MCP/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPBearerGateRejectsBeforeMCPAndPreservesHealth(t *testing.T) {
	cfg := cfgWith(nil, permsFull)
	cfg.Server.BearerToken = "fixture-token-for-http-tests"
	handler, err := New(cfg, logger.New("error", "test")).HTTPHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, header := range []string{"", "Bearer wrong", "Basic fixture-token-for-http-tests"} {
		req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated request returned %d", w.Code)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	req.Header.Set("Authorization", "Bearer fixture-token-for-http-tests")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == http.StatusUnauthorized {
		t.Fatal("correct token was refused")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/health", nil))
	if w.Code != http.StatusOK {
		t.Fatal("health probe is unavailable")
	}
}

func TestGuardedHTTPRequiresAuthentication(t *testing.T) {
	cfg := cfgWith(nil, permsFull)
	cfg.Imports = config.ImportPolicy{Guarded: true}
	if _, err := New(cfg, logger.New("error", "test")).HTTPHandler(); err == nil {
		t.Fatal("guarded HTTP was exposed without authentication")
	}
}

type testHTTPBearerTransport struct{ token string }

func (b testHTTPBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(clone)
}

func TestAuthenticatedHTTPCompletesNativeMCPRoundTrip(t *testing.T) {
	backend, hits := fakeArr(t, `[]`)
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: backend.URL, APIKey: "fixture"}}}, permsFull)
	cfg.Tools = config.ToolPolicy{Allow: []string{"sonarr_list_series"}}
	cfg.Server.BearerToken = "fixture-native-mcp-token"
	handler, err := New(cfg, logger.New("error", "test")).HTTPHandler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: testHTTPBearerTransport{token: cfg.Server.BearerToken}}, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if names := toolNames(t, session); len(names) != 1 || names[0] != "sonarr_list_series" {
		t.Fatalf("wrong HTTP catalogue: %v", names)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "sonarr_list_series", Arguments: map[string]any{}})
	if err != nil || res.IsError || *hits != 1 {
		t.Fatalf("HTTP tool round trip failed: %v %+v hits=%d", err, res, *hits)
	}
}
