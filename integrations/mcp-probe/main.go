// Command mcp-probe verifies native MCP discovery and explicitly selected read tools.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	next.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(next)
}

type readCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func run() error {
	endpoint := flag.String("endpoint", "", "native MCP URL")
	tokenFile := flag.String("token-file", "", "private scoped-token file")
	input := flag.String("calls-file", "", "optional private JSON array of read-only calls")
	output := flag.String("output", "", "optional private JSON result file")
	flag.Parse()
	uri, err := url.Parse(*endpoint)
	if err != nil {
		return fmt.Errorf("invalid endpoint")
	}
	ip := net.ParseIP(uri.Hostname())
	if uri.User != nil || uri.RawQuery != "" || uri.Fragment != "" || uri.Scheme != "https" && (uri.Scheme != "http" || ip == nil || !ip.IsPrivate() && !ip.IsLoopback()) {
		return fmt.Errorf("HTTPS or an explicit private HTTP IP is required")
	}
	// These are explicit operator CLI file paths, not remote tool arguments.
	raw, err := os.ReadFile(*tokenFile) // #nosec G304
	if err != nil {
		return fmt.Errorf("cannot read scoped token file")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("invalid scoped token")
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "arr-mcp-read-probe", Version: "1"}, nil)
	httpClient := &http.Client{Transport: bearerTransport{token}, Timeout: 100 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: httpClient, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		return fmt.Errorf("native MCP negotiation failed")
	}
	defer func() { _ = session.Close() }()
	list, err := session.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("native MCP discovery failed")
	}
	tools := map[string]bool{}
	names := []string{}
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		tools[tool.Name] = tool.Annotations != nil && tool.Annotations.ReadOnlyHint
	}
	calls := []readCall{}
	if *input != "" {
		data, err := os.ReadFile(*input) // #nosec G304 -- explicit operator input file
		if err != nil || len(data) > 1024*1024 {
			return fmt.Errorf("invalid read-call file")
		}
		if err := json.Unmarshal(data, &calls); err != nil {
			return fmt.Errorf("invalid read-call JSON")
		}
	}
	results := map[string]any{}
	for _, call := range calls {
		if !tools[call.Name] {
			return fmt.Errorf("probe refuses non-read tool %s", call.Name)
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.Name, Arguments: call.Arguments})
		if err != nil {
			return fmt.Errorf("native read transport failed for %s", call.Name)
		}
		results[call.Name] = result
		if result.IsError {
			return fmt.Errorf("native read tool failed: %s", call.Name)
		}
	}
	if *output != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*output, data, 0600); err != nil {
			return fmt.Errorf("cannot write private read results")
		}
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"tools": names, "readCalls": len(calls)})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
