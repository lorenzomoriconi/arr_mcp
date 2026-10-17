package server

import (
	"strings"
	"testing"

	"github.com/GauranshMathur/ARR_MCP/pkg/config"
)

func TestPublicMagnetNormalization(t *testing.T) {
	hash := strings.Repeat("a", 40)
	magnet, err := normalizePublicMagnet("magnet:?xt=urn:btih:"+hash+"&dn=Example.S01.1080p&tr=https%3A%2F%2Ftracker.invalid%2Fpasskey", "")
	if err != nil {
		t.Fatal(err)
	}
	if magnet.Hash != hash || magnet.Title != "Example.S01.1080p" || strings.Contains(magnet.URI, "tr=") || strings.Contains(magnet.URI, "tracker") {
		t.Fatalf("unsafe normalized magnet: %+v", magnet)
	}
	base32, err := normalizePublicMagnet("magnet:?xt=urn:btih:"+strings.Repeat("A", 32), "Example.S01")
	if err != nil || base32.Hash != strings.Repeat("0", 40) {
		t.Fatal("base32 hash not normalized", err)
	}
	for _, value := range []string{
		"https://example.invalid/torrent", "magnet:?xt=urn:btih:all&dn=Example", "magnet:?xt=urn:btih:" + hash + "&dn=../Example",
		"magnet:?xt=urn:btih:" + hash + "&dn=Example&x.pe=192.0.2.2:42", "magnet:?xt=urn:btih:" + hash + "&dn=Example&ws=https://example.invalid/a",
		"magnet:?xt=urn:btih:" + hash + "&dn=Example&xs=https://example.invalid/a", "magnet:?xt=urn:btih:" + hash + "&xt=urn:btih:" + hash + "&dn=Example",
		"magnet:?xt=urn:btih:" + hash + "&dn=Example%0Ainjection", "magnet:?xt=urn:btih:" + hash + "&dn=Example&unknown=x",
	} {
		if _, err := normalizePublicMagnet(value, ""); err == nil {
			t.Fatalf("unsafe magnet accepted: %s", value)
		}
	}
}

func TestGuardedMagnetToolsRequireConfiguredDownloadClient(t *testing.T) {
	cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: "http://example.invalid", APIKey: "fixture"}}}, permsFull)
	names := toolNames(t, connect(t, cfg))
	if !has(names, "sonarr_magnet_preview") || !has(names, "sonarr_add_magnet") {
		t.Fatal("guarded magnet tools not registered")
	}
}
