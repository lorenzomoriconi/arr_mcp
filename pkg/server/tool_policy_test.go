package server

import (
	"context"
	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/GauranshMathur/ARR_MCP/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"testing"
	"time"
)

func TestServerToolPolicyHidesAndRejectsDeniedTools(t *testing.T) {
	srv, hits := fakeArr(t, `[]`)
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: srv.URL, APIKey: "fixture"}}}, permsFull)
	cfg.Tools = config.ToolPolicy{Allow: []string{"sonarr_list_series"}, DenyDestructive: true}
	cs := connect(t, cfg)
	names := toolNames(t, cs)
	if len(names) != 1 || names[0] != "sonarr_list_series" {
		t.Fatalf("unexpected advertised tools: %v", names)
	}
	for _, name := range []string{"sonarr_run_command", "sonarr_delete_series"} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"id": 1, "name": "Delete"}})
		if err == nil && !res.IsError {
			t.Fatalf("denied tool %s accepted", name)
		}
	}
	if *hits != 0 {
		t.Fatalf("denied tools reached the application %d times", *hits)
	}
}

func TestToolPolicyCannotBeWidenedByInstancePermissions(t *testing.T) {
	srv, hits := fakeArr(t, `[]`)
	full := permsFull
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: srv.URL, APIKey: "fixture", Permissions: &full}}}, permsFull)
	cfg.Tools = config.ToolPolicy{Deny: []string{"sonarr_run_command"}, DenyDestructive: true}
	cs := connect(t, cfg)
	names := toolNames(t, cs)
	if !has(names, "sonarr_add_series") || has(names, "sonarr_delete_series") || has(names, "sonarr_run_command") {
		t.Fatalf("unexpected catalogue: %v", names)
	}
	if *hits != 0 {
		t.Fatal("catalogue discovery contacted an app")
	}
}

func TestHermesExampleOnlyNamesExistingTools(t *testing.T) {
	for _, name := range []string{"SONARR_URL", "RADARR_URL", "QBITTORRENT_URL"} {
		t.Setenv(name, "http://example.invalid")
	}
	for _, name := range []string{"SONARR_API_KEY", "RADARR_API_KEY", "QBITTORRENT_USERNAME", "QBITTORRENT_PASSWORD", "ARR_MCP_HTTP_TOKEN"} {
		t.Setenv(name, "fixture-value")
	}
	cfg, err := config.Load("../../config.hermes.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	allowed := cfg.Tools.Allow
	cfg.Tools = config.ToolPolicy{}
	cfg.Permissions = permsFull
	names := toolNames(t, connect(t, cfg))
	for _, name := range allowed {
		if !has(names, name) {
			t.Errorf("example names unknown tool %q", name)
		}
	}
}

func TestCallPolicyIsCheckedAfterRegistration(t *testing.T) {
	srv, hits := fakeArr(t, `[]`)
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: srv.URL, APIKey: "fixture"}}}, permsFull)
	cs := connect(t, cfg)
	cfg.Tools = config.ToolPolicy{Allow: []string{}}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sonarr_list_series", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || *hits != 0 {
		t.Fatal("call-time policy was bypassed")
	}
}

func TestGuardedImportExpiredOrWrongInstancePlanDoesNotReachApp(t *testing.T) {
	srv, hits := fakeArr(t, `[]`)
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: srv.URL, APIKey: "fixture"}, {Name: "other", URL: srv.URL, APIKey: "fixture"}}}, permsFull)
	s := New(cfg, logger.New("error", "test"))
	for _, tc := range []struct {
		name     string
		expired  bool
		instance string
	}{{"expired", true, "main"}, {"wrong-instance", false, "other"}} {
		expires := time.Now().Add(time.Minute)
		if tc.expired {
			expires = time.Now().Add(-time.Second)
		}
		s.imports.pending[tc.name] = importPlan{service: "sonarr", instance: tc.instance, expires: expires}
		_, err := s.applyGuardedImport(context.Background(), nil, "sonarr", ManualImportArgs{InstanceArg: InstanceArg{Instance: "main"}, PlanID: tc.name, AcceptCleanup: true})
		if err == nil {
			t.Errorf("accepted %s plan", tc.name)
		}
	}
	if *hits != 0 {
		t.Fatal("invalid plan contacted an app")
	}
}
