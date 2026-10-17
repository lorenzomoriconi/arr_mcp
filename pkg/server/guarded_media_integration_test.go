package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type guardedMediaFixture struct {
	mu            sync.Mutex
	baseURL       string
	present       bool
	addPosts      int
	importPosts   int
	wrongTarget   bool
	existingFile  bool
	incomplete    bool
	overlaps      bool
	wrongCategory bool
	sourceChanged bool
	profileName   string
	malformedAck  bool
	failPost      bool
}

func mediaFixture(t *testing.T) (*guardedMediaFixture, *mcp.ClientSession) {
	t.Helper()
	f := &guardedMediaFixture{profileName: "Standard"}
	hash := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch r.URL.Path {
		case "/api/v3/series", "/api/v3/movie":
			body = []any{map[string]any{"id": 1, "qualityProfileId": 1}}
		case "/api/v3/series/1", "/api/v3/movie/1":
			body = map[string]any{"id": 1, "title": "Example", "path": "/library/example", "qualityProfileId": 1, "hasFile": f.existingFile, "seasons": []any{map[string]any{"seasonNumber": 1}}}
		case "/api/v3/parse":
			id := 1
			if f.wrongTarget {
				id = 2
			}
			body = map[string]any{"series": map[string]any{"id": id}, "parsedEpisodeInfo": map[string]any{"seasonNumber": 1, "fullSeason": true, "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p"}}, "languages": []any{map[string]any{"name": "English"}}}, "episodes": []any{map[string]any{"id": 2, "seriesId": 1, "seasonNumber": 1}}}
		case "/api/v3/episode":
			fileID := 0
			if f.existingFile {
				fileID = 99
			}
			body = []any{map[string]any{"id": 2, "seriesId": 1, "seasonNumber": 1, "episodeNumber": 1, "hasFile": f.existingFile, "episodeFileId": fileID}}
		case "/api/v3/downloadclient":
			parsed, err := url.Parse(f.baseURL)
			if err != nil {
				t.Error(err)
			}
			port, err := strconv.Atoi(parsed.Port())
			if err != nil {
				t.Error(err)
			}
			fields := []any{}
			for key, value := range map[string]any{"host": parsed.Hostname(), "port": port, "useSsl": false, "tvCategory": "tv", "movieCategory": "tv", "password": "sentinel-provider-secret"} {
				fields = append(fields, map[string]any{"name": key, "value": value})
			}
			body = []any{map[string]any{"id": 1, "enable": true, "implementation": "QBittorrent", "fields": fields}}
		case "/api/v3/config/downloadclient":
			body = map[string]any{"enableCompletedDownloadHandling": true}
		case "/api/v3/qualityprofile/1":
			if r.Method == http.MethodPut {
				f.addPosts++
			}
			body = map[string]any{"id": 1, "name": f.profileName}
		case "/api/v3/queue/details":
			body = []any{}
			if f.overlaps {
				body = []any{map[string]any{"id": 8, "seriesId": 1, "movieId": 1, "episodeId": 2, "downloadId": strings.Repeat("b", 40)}}
			}
		case "/api/v3/queue":
			body = map[string]any{"totalRecords": 1, "records": []any{map[string]any{"downloadId": hash, "seriesId": 1, "movieId": 1, "episodeId": 2, "status": "completed", "sizeleft": 0, "trackedDownloadState": "importBlocked"}}}
		case "/api/v3/rootfolder":
			body = []any{map[string]any{"path": "/library"}}
		case "/api/v3/manualimport":
			source := "/downloads/pack/a.mkv"
			if f.sourceChanged {
				source = "/downloads/pack/changed.mkv"
			}
			body = []any{map[string]any{"id": 3, "path": source, "size": 100, "downloadId": hash, "series": map[string]any{"id": 1}, "episodes": []any{map[string]any{"id": 2}}, "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p"}}, "languages": []any{map[string]any{"name": "English"}}}}
		case "/api/v3/qualitydefinition":
			body = []any{map[string]any{"quality": map[string]any{"id": 3, "name": "WEBDL-1080p"}}}
		case "/api/v3/language":
			body = []any{map[string]any{"id": 1, "name": "English"}}
		case "/api/v3/command":
			if r.Method == http.MethodGet {
				body = []any{}
				break
			}
			f.importPosts++
			var command struct {
				Mode string `json:"importMode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Error(err)
			}
			if command.Mode != "copy" {
				t.Error("import mode is not copy")
			}
			body = map[string]any{"id": 10, "name": "ManualImport", "status": "queued"}
		case "/api/v2/auth/login":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("Ok."))
			return
		case "/api/v2/torrents/categories":
			body = map[string]any{"tv": map[string]any{"name": "tv", "savePath": "/downloads"}}
		case "/api/v2/app/preferences":
			body = map[string]any{"dht": true}
		case "/api/v2/torrents/info":
			body = []any{}
			if f.present {
				category := "tv"
				if f.wrongCategory {
					category = "other"
				}
				body = []any{map[string]any{"hash": hash, "category": category, "state": "stoppedUP", "amount_left": 0, "progress": 1, "save_path": "/downloads", "content_path": "/downloads/pack", "auto_tmm": true}}
			}
		case "/api/v2/torrents/files":
			progress := 1.0
			if f.incomplete {
				progress = 0.9
			}
			body = []any{map[string]any{"index": 0, "name": "pack/a.mkv", "size": 100, "progress": progress, "priority": 1}}
		case "/api/v2/torrents/stop":
			f.addPosts++
			body = map[string]any{}
		case "/api/v2/torrents/add":
			f.addPosts++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			for key := range r.Form {
				if key != "urls" && key != "category" && key != "autoTMM" && key != "rename" {
					t.Errorf("unexpected add parameter: %s", key)
				}
			}
			if r.Form.Get("category") != "tv" || r.Form.Get("autoTMM") != "true" || strings.Contains(r.Form.Get("urls"), "tr=") {
				t.Error("magnet destination or source hints were not fenced")
			}
			if f.failPost {
				w.WriteHeader(500)
				return
			}
			f.present = true
			if f.malformedAck {
				body = map[string]any{"success_count": 1, "failure_count": 0}
			} else {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("Ok."))
				return
			}
		default:
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	f.baseURL = srv.URL
	t.Cleanup(srv.Close)
	cfg := cfgWith(map[string][]config.Instance{
		"sonarr":      {{Name: "main", URL: srv.URL, APIKey: "fixture"}},
		"qbittorrent": {{Name: "main", URL: srv.URL, AuthBypass: true}},
	}, permsFull)
	cfg.Downloads.Client = "main"
	cfg.Downloads.Guarded = true
	cfg.Imports.Guarded = true
	return f, connect(t, cfg)
}

func mediaCall(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, success bool) map[string]any {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	text := contentText(result)
	if strings.Contains(text, "sentinel-provider-secret") {
		t.Fatal("native provider secret leaked into model response")
	}
	if result.IsError == success {
		t.Fatalf("%s unexpected status: %s", name, text)
	}
	out := map[string]any{}
	if success {
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func magnetArgs() map[string]any {
	return map[string]any{"itemId": 1, "magnet": "magnet:?xt=urn:btih:" + strings.Repeat("a", 40) + "&dn=Example.S01.1080p&tr=https://tracker.invalid/passkey"}
}

func TestGuardedMagnetNativePreviewApplyAndNoReplay(t *testing.T) {
	for _, jsonAck := range []bool{false, true} {
		t.Run(fmt.Sprint(jsonAck), func(t *testing.T) {
			f, session := mediaFixture(t)
			f.malformedAck = jsonAck
			preview := mediaCall(t, session, "sonarr_magnet_preview", magnetArgs(), true)
			args := map[string]any{"planId": preview["planId"], "acceptUnverified": true}
			receipt := mediaCall(t, session, "sonarr_add_magnet", args, true)
			if receipt["submitted"] != true || receipt["imported"] != false || f.addPosts != 1 {
				t.Fatal("add was not confirmed exactly once without claiming import", receipt)
			}
			mediaCall(t, session, "sonarr_add_magnet", args, false)
			if f.addPosts != 1 {
				t.Fatal("spent plan was replayed")
			}
		})
	}
}
func TestGuardedMagnetRejectsWrongTargetStaleProfileAndReplacement(t *testing.T) {
	for _, kind := range []string{"wrong-target", "profile", "replacement", "existing-other-category", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			f, session := mediaFixture(t)
			if kind == "wrong-target" {
				f.wrongTarget = true
				mediaCall(t, session, "sonarr_magnet_preview", magnetArgs(), false)
				return
			}
			if kind == "existing-other-category" {
				f.present = true
				f.wrongCategory = true
				mediaCall(t, session, "sonarr_magnet_preview", magnetArgs(), false)
				return
			}
			if kind == "replacement" {
				f.overlaps = true
			}
			preview := mediaCall(t, session, "sonarr_magnet_preview", magnetArgs(), true)
			if kind == "profile" {
				f.profileName = "Changed"
			}
			if kind == "uncertain" {
				f.failPost = true
			}
			args := map[string]any{"planId": preview["planId"], "acceptUnverified": true}
			mediaCall(t, session, "sonarr_add_magnet", args, false)
			if kind == "uncertain" {
				mediaCall(t, session, "sonarr_add_magnet", args, false)
				if f.addPosts != 1 {
					t.Fatal("uncertain add was replayed")
				}
			} else if f.addPosts != 0 {
				t.Fatal("refused magnet mutated qBittorrent")
			}
		})
	}
}
func TestGuardedMagnetMatchingExistingHashDoesNotResumeOrRecategorize(t *testing.T) {
	f, session := mediaFixture(t)
	f.present = true
	preview := mediaCall(t, session, "sonarr_magnet_preview", magnetArgs(), true)
	receipt := mediaCall(t, session, "sonarr_add_magnet", map[string]any{"planId": preview["planId"], "acceptUnverified": true}, true)
	if receipt["alreadyPresent"] != true || receipt["submitted"] != false || f.addPosts != 0 {
		t.Fatal("existing hash was modified")
	}
}
func TestGuardedImportManifestAndOverlapAreRechecked(t *testing.T) {
	for _, kind := range []string{"valid", "incomplete", "changed-source", "overlap-declined", "overlap-accepted", "category-change"} {
		t.Run(kind, func(t *testing.T) {
			f, session := mediaFixture(t)
			f.present = true
			if strings.HasPrefix(kind, "overlap") {
				f.overlaps = true
			}
			preview := mediaCall(t, session, "sonarr_manual_import_preview", map[string]any{"seriesId": 1, "downloadId": strings.Repeat("a", 40)}, true)
			switch kind {
			case "incomplete":
				f.incomplete = true
			case "changed-source":
				f.sourceChanged = true
			case "category-change":
				f.wrongCategory = true
			}
			success := kind == "valid" || kind == "overlap-accepted"
			mediaCall(t, session, "sonarr_manual_import", map[string]any{"planId": preview["planId"], "acceptCleanup": true, "acceptOverlap": kind == "overlap-accepted"}, success)
			expected := 0
			if success {
				expected = 1
			}
			if f.importPosts != expected {
				t.Fatalf("native import submissions = %d, want %d", f.importPosts, expected)
			}
		})
	}
}

func TestGuardedProfileCannotChangeAssignedLibraryPreset(t *testing.T) {
	f, session := mediaFixture(t)
	mediaCall(t, session, "sonarr_update_quality_profile", map[string]any{"id": 1, "name": "Unexpected global edit"}, false)
	if f.addPosts != 0 {
		t.Fatal("assigned shared profile was changed")
	}
}

func TestGuardedTorrentControlsStayInNativeMediaCategory(t *testing.T) {
	for _, wrongCategory := range []bool{false, true} {
		t.Run(fmt.Sprint(wrongCategory), func(t *testing.T) {
			f, session := mediaFixture(t)
			f.present = true
			f.wrongCategory = wrongCategory
			mediaCall(t, session, "qbittorrent_stop_torrents", map[string]any{"hashes": []string{strings.Repeat("a", 40)}}, !wrongCategory)
			wanted := 1
			if wrongCategory {
				wanted = 0
			}
			if f.addPosts != wanted {
				t.Fatal("wrong-category control reached native backend")
			}
		})
	}
}
