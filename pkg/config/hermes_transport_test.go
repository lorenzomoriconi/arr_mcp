package config

import "testing"

func TestHermesTransportRejectsUnsafePathAndPeers(t *testing.T) {
	for _, block := range []string{
		"path: relative", "path: /health", "path: //rpc", "path: /rpc?token=x", "path: /rpc/../mcp",
		"allowedPeers: [not-a-network]", "allowedPeers: ['192.0.2.1/999']",
	} {
		_, err := Load(writeCfg(t, "server:\n  "+block+"\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
		if err == nil {
			t.Fatalf("unsafe transport configuration accepted: %s", block)
		}
	}
}

func TestGuardedDownloadsRequireConfiguredClient(t *testing.T) {
	_, err := Load(writeCfg(t, "downloads:\n  guarded: true\nservices:\n  sonarr:\n    - name: main\n      url: http://example.invalid\n      apiKey: fixture\n"))
	if err == nil {
		t.Fatal("guarded download policy silently omitted the independent client binding")
	}
}
