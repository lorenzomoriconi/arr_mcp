package config

import (
	"strings"
	"testing"
)

func TestToolPolicyPreservesExplicitEmptyAllowlist(t *testing.T) {
	cfg, err := Load(writeCfg(t, "tools:\n  allow: []\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Allow == nil {
		t.Fatal("explicit empty allowlist became unrestricted")
	}
	if cfg.Tools.Allows("sonarr_list_series", false) {
		t.Fatal("empty allowlist must deny all tools")
	}
}

func TestToolPolicyRejectsPatterns(t *testing.T) {
	for _, block := range []string{"allow: [sonarr_*]", "deny: ['*']"} {
		_, err := Load(writeCfg(t, "tools:\n  "+block+"\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
		if err == nil || !strings.Contains(err.Error(), "tools.") {
			t.Fatalf("invalid tool pattern accepted: %v", err)
		}
	}
}

func TestToolPolicyDenyWinsAndDestructiveCannotBeAllowed(t *testing.T) {
	p := ToolPolicy{Allow: []string{"sonarr_add_series", "sonarr_delete_series"}, Deny: []string{"sonarr_add_series"}, DenyDestructive: true}
	if p.Allows("sonarr_add_series", false) || p.Allows("sonarr_delete_series", true) {
		t.Fatal("deny policy was bypassed")
	}
	if !(ToolPolicy{}).Allows("sonarr_list_series", false) {
		t.Fatal("omitted policy changed upstream defaults")
	}
}

func TestHTTPTokenExpandsPrivatelyAndRejectsUnsetReference(t *testing.T) {
	t.Setenv("ARR_MCP_HTTP_TEST_TOKEN", "fixture-http-value")
	cfg, err := Load(writeCfg(t, "server:\n  bearerToken: ${ARR_MCP_HTTP_TEST_TOKEN}\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
	if err != nil || cfg.Server.BearerToken != "fixture-http-value" {
		t.Fatal("HTTP token expansion failed")
	}
	t.Setenv("ARR_MCP_HTTP_TEST_TOKEN", "")
	_, err = Load(writeCfg(t, "server:\n  bearerToken: ${ARR_MCP_HTTP_TEST_TOKEN}\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
	if err == nil {
		t.Fatal("unset token reference was accepted")
	}
}

func TestUnknownSafetyConfigurationFieldsAreRejected(t *testing.T) {
	for _, block := range []string{"tool:\n  allow: []\n", "tools:\n  allowed: []\n", "imports:\n  guardedImport: true\n"} {
		_, err := Load(writeCfg(t, block+"services:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
		if err == nil {
			t.Fatalf("misspelled safety setting was silently ignored: %s", block)
		}
	}
}
