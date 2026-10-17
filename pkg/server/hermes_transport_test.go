package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/GauranshMathur/ARR_MCP/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func hermesTransportConfig(t *testing.T, upstream string, peers string) *config.Config {
	t.Helper()
	raw := fmt.Sprintf(`server:
  path: /rpc
  stateless: true
  jsonResponse: true
  allowedPeers: [%s]
  bearerToken: transport-fixture
permissions:
  mode: confirm
  confirmScope: write
  fallback: deny
tools:
  allow: [sonarr_trigger_search]
services:
  sonarr:
    - name: main
      url: %s
      apiKey: fixture
`, peers, upstream)
	filename := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(filename, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filename)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

type postOnlyTransport struct{ unexpected atomic.Int32 }

func (p *postOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodPost || r.URL.Path != "/rpc" {
		p.unexpected.Add(1)
		return nil, fmt.Errorf("appliance refused unexpected method or path")
	}
	cloned := r.Clone(r.Context())
	cloned.Header.Set("Authorization", "Bearer transport-fixture")
	return http.DefaultTransport.RoundTrip(cloned)
}

func TestHermesPOSTOnlyStatelessApprovalRoundTrip(t *testing.T) {
	for _, action := range []string{"accept", "decline", "cancel"} {
		t.Run(action, func(t *testing.T) {
			upstream, hits := fakeArr(t, `{"id":1,"name":"SeriesSearch","status":"queued"}`)
			cfg := hermesTransportConfig(t, upstream.URL, "127.0.0.1/32, '::1/128'")
			handler, err := New(cfg, logger.New("error", "test")).HTTPHandler()
			if err != nil {
				t.Fatal(err)
			}
			endpoint := httptest.NewServer(handler)
			t.Cleanup(endpoint.Close)
			var prompted atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "hermes-fixture", Version: "1"}, &mcp.ClientOptions{
				ElicitationHandler: func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
					prompted.Add(1)
					return &mcp.ElicitResult{Action: action}, nil
				},
			})
			transport := &postOnlyTransport{}
			session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
				Endpoint: endpoint.URL + "/rpc", HTTPClient: &http.Client{Transport: transport},
				DisableStandaloneSSE: true, MaxRetries: -1,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Close() }()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "sonarr_trigger_search", Arguments: map[string]any{"seriesId": 5},
			})
			if err != nil {
				t.Fatal(err)
			}
			if prompted.Load() != 1 {
				t.Fatalf("approval prompts = %d, want 1; result=%s", prompted.Load(), contentText(result))
			}
			if action == "accept" {
				if result.IsError || *hits != 1 {
					t.Fatalf("approved action did not run exactly once: hits=%d result=%s", *hits, contentText(result))
				}
			} else if !result.IsError || *hits != 0 {
				t.Fatalf("refused action reached upstream: hits=%d result=%s", *hits, contentText(result))
			}
			if transport.unexpected.Load() != 0 {
				t.Fatal("protocol required widening appliance methods or paths")
			}
		})
	}
}

func TestHermesHTTPPeerFenceIgnoresForwardedHeaders(t *testing.T) {
	cfg := hermesTransportConfig(t, "http://example.invalid", "192.0.2.1/32")
	handler, err := New(cfg, logger.New("error", "test")).HTTPHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"198.51.100.2:51234", "[2001:db8::1]:51234", "not-an-address"} {
		req := httptest.NewRequest(http.MethodPost, "http://example.invalid/rpc", strings.NewReader(`{}`))
		req.RemoteAddr = remote
		req.Header.Set("Authorization", "Bearer transport-fixture")
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Fatalf("untrusted peer %q got %d", remote, response.Code)
		}
	}
}
