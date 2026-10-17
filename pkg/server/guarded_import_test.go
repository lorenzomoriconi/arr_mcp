package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type importFixture struct {
	mu         sync.Mutex
	sourcePath string
	existing   bool
	incomplete bool
	rejected   bool
	posts      int
	mode       string
	failPost   bool
	movie      bool
}

func guardedFixture(t *testing.T, movie bool) (*importFixture, *mcp.ClientSession) {
	t.Helper()
	f := &importFixture{sourcePath: "/downloads/pack/a.mkv", movie: movie}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		var body any
		switch r.URL.Path {
		case "/api/v3/queue":
			left := 0
			if f.incomplete {
				left = 10
			}
			body = map[string]any{"totalRecords": 1, "records": []any{map[string]any{"downloadId": "HASH", "status": "completed", "sizeleft": left, "trackedDownloadState": "importBlocked"}}}
		case "/api/v3/rootfolder":
			body = []any{map[string]any{"path": "/library"}}
		case "/api/v3/series/1":
			body = map[string]any{"id": 1, "path": "/library/show"}
		case "/api/v3/movie/1":
			fileID := 0
			if f.existing {
				fileID = 9
			}
			body = map[string]any{"id": 1, "path": "/library/movie", "hasFile": f.existing, "movieFileId": fileID}
		case "/api/v3/episode":
			body = []any{map[string]any{"id": 2, "seriesId": 1, "hasFile": f.existing, "episodeFileId": 0}}
		case "/api/v3/manualimport":
			if r.URL.Query().Get("downloadId") != "HASH" {
				t.Error("preview was not bound to download")
			}
			rejections := []string{}
			if f.rejected {
				rejections = []string{"unknown quality"}
			}
			row := map[string]any{"id": 3, "path": f.sourcePath, "size": 100, "downloadId": "HASH", "quality": map[string]any{"quality": map[string]any{"name": "WEBDL-1080p"}}, "languages": []any{map[string]any{"name": "English"}}, "rejections": rejections}
			if movie {
				row["movie"] = map[string]any{"id": 1}
			} else {
				row["series"] = map[string]any{"id": 1}
				row["episodes"] = []any{map[string]any{"id": 2}}
			}
			body = []any{row}
		case "/api/v3/qualitydefinition":
			body = []any{map[string]any{"quality": map[string]any{"id": 3, "name": "WEBDL-1080p"}}}
		case "/api/v3/language":
			body = []any{map[string]any{"id": 1, "name": "English"}}
		case "/api/v3/command":
			if r.Method != "POST" {
				t.Fatalf("unexpected command method %s", r.Method)
			}
			f.posts++
			var command map[string]any
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Error(err)
			}
			f.mode, _ = command["importMode"].(string)
			if f.failPost {
				w.WriteHeader(500)
				return
			}
			body = map[string]any{"id": 10, "name": "ManualImport", "status": "queued"}
		default:
			t.Errorf("unexpected app request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	service := "sonarr"
	if movie {
		service = "radarr"
	}
	cfg := cfgWith(map[string][]config.Instance{service: {{Name: "main", URL: srv.URL, APIKey: "fixture"}}}, permsFull)
	cfg.Imports = config.ImportPolicy{Guarded: true}
	return f, connect(t, cfg)
}

func importCall(t *testing.T, cs *mcp.ClientSession, svc, suffix string, args map[string]any, success bool) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: svc + suffix, Arguments: args})
	if err != nil {
		t.Fatalf("MCP call: %v", err)
	}
	if res.IsError == success {
		t.Fatalf("unexpected result (success=%v): %s", success, contentText(res))
	}
	return res
}

func previewPlan(t *testing.T, cs *mcp.ClientSession, svc string) string {
	t.Helper()
	key := "seriesId"
	if svc == "radarr" {
		key = "movieId"
	}
	res := importCall(t, cs, svc, "_manual_import_preview", map[string]any{"downloadId": "HASH", key: 1}, true)
	var out struct {
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal([]byte(contentText(res)), &out); err != nil {
		t.Fatal(err)
	}
	if out.PlanID == "" {
		t.Fatalf("no plan returned: %s", contentText(res))
	}
	return out.PlanID
}

func TestGuardedImportUsesServerPlanCopyAndIsSingleUse(t *testing.T) {
	for _, svc := range []string{"sonarr", "radarr"} {
		t.Run(svc, func(t *testing.T) {
			f, cs := guardedFixture(t, svc == "radarr")
			plan := previewPlan(t, cs, svc)
			args := map[string]any{"planId": plan, "acceptCleanup": true}
			importCall(t, cs, svc, "_manual_import", args, true)
			importCall(t, cs, svc, "_manual_import", args, false)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 || f.mode != "copy" {
				t.Fatalf("posts=%d mode=%q", f.posts, f.mode)
			}
		})
	}
}

func TestGuardedImportRejectsStaleSourceOrExistingLibraryFile(t *testing.T) {
	for _, change := range []string{"source", "existing", "incomplete", "rejected"} {
		t.Run(change, func(t *testing.T) {
			f, cs := guardedFixture(t, false)
			plan := previewPlan(t, cs, "sonarr")
			f.mu.Lock()
			switch change {
			case "source":
				f.sourcePath = "/downloads/pack/replaced.mkv"
			case "existing":
				f.existing = true
			case "incomplete":
				f.incomplete = true
			case "rejected":
				f.rejected = true
			}
			f.mu.Unlock()
			importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan, "acceptCleanup": true}, false)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 0 {
				t.Fatal("unsafe import reached the app")
			}
		})
	}
}

func TestGuardedImportRejectsCallerFilesMoveAndMissingCleanupAcceptance(t *testing.T) {
	for _, extra := range []map[string]any{{}, {"acceptCleanup": true, "importMode": "move"}, {"acceptCleanup": true, "files": []any{map[string]any{"path": "/arbitrary/a.mkv"}}}} {
		f, cs := guardedFixture(t, false)
		plan := previewPlan(t, cs, "sonarr")
		extra["planId"] = plan
		importCall(t, cs, "sonarr", "_manual_import", extra, false)
		f.mu.Lock()
		posts := f.posts
		f.mu.Unlock()
		if posts != 0 {
			t.Fatal("invalid import reached app")
		}
	}
}

func TestGuardedImportRejectsFoldersAndLibrarySources(t *testing.T) {
	f, cs := guardedFixture(t, false)
	importCall(t, cs, "sonarr", "_manual_import_preview", map[string]any{"folder": "/downloads", "seriesId": 1}, false)
	f.mu.Lock()
	f.sourcePath = "/library/show/a.mkv"
	f.mu.Unlock()
	res := importCall(t, cs, "sonarr", "_manual_import_preview", map[string]any{"downloadId": "HASH", "seriesId": 1}, false)
	if !strings.Contains(contentText(res), "library") {
		t.Fatal("library source was not identified")
	}
}

func TestGuardedImportDoesNotReplayUncertainSubmission(t *testing.T) {
	f, cs := guardedFixture(t, false)
	plan := previewPlan(t, cs, "sonarr")
	f.mu.Lock()
	f.failPost = true
	f.mu.Unlock()
	args := map[string]any{"planId": plan, "acceptCleanup": true}
	importCall(t, cs, "sonarr", "_manual_import", args, false)
	importCall(t, cs, "sonarr", "_manual_import", args, false)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatalf("uncertain command replayed %d times", f.posts)
	}
}

func TestImportPlanExpiry(t *testing.T) {
	plans := newImportPlans()
	plans.pending["expired"] = importPlan{expires: time.Now().Add(-time.Second)}
	plans.prune(time.Now())
	if len(plans.pending) != 0 {
		t.Fatal("expired plan survived")
	}
}

func TestGuardedImportTwoPlansCannotSubmitTheSameDownload(t *testing.T) {
	f, cs := guardedFixture(t, false)
	first := previewPlan(t, cs, "sonarr")
	second := previewPlan(t, cs, "sonarr")
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": first, "acceptCleanup": true}, true)
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": second, "acceptCleanup": true}, false)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatal("duplicate plans submitted more than once")
	}
}
