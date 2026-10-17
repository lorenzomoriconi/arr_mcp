package server

import (
	"context"
	"encoding/json"
	"fmt"
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
	mu               sync.Mutex
	sourcePath       string
	existing         bool
	incomplete       bool
	rejected         bool
	posts            int
	mode             string
	failPost         bool
	movie            bool
	activeCommand    string
	startOnLookup    bool
	multiEpisode     bool
	importedEpisodes int
	targetPath       string
	backendURL       string
	queueTarget      int
	totalQueue       int
	queueReads       int
	duplicate        bool
}

func guardedFixture(t *testing.T, movie bool) (*importFixture, *mcp.ClientSession) {
	t.Helper()
	f := &importFixture{sourcePath: "/downloads/pack/a.mkv", movie: movie, queueTarget: 1, totalQueue: 1, targetPath: "/library/show"}
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
			f.queueReads++
			page := 1
			fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
			rows := []any{}
			for i := (page - 1) * 100; i < page*100 && i < f.totalQueue; i++ {
				hash := "OTHER"
				if i == f.totalQueue-1 {
					hash = "HASH"
				}
				rows = append(rows, map[string]any{"downloadId": hash, "status": "completed", "sizeleft": left, "trackedDownloadState": "importBlocked", "seriesId": f.queueTarget, "movieId": f.queueTarget})
			}
			body = map[string]any{"totalRecords": f.totalQueue, "records": rows}
		case "/api/v3/rootfolder":
			body = []any{map[string]any{"path": "/library"}}
		case "/api/v3/series/1":
			body = map[string]any{"id": 1, "path": f.targetPath}
		case "/api/v3/movie/1":
			fileID := 0
			if f.existing {
				fileID = 9
			}
			body = map[string]any{"id": 1, "path": "/library/movie", "hasFile": f.existing, "movieFileId": fileID}
		case "/api/v3/episode":
			body = []any{map[string]any{"id": 2, "seriesId": 1, "hasFile": f.existing, "episodeFileId": 0}}
			if f.multiEpisode {
				body = []any{map[string]any{"id": 2, "seriesId": 1, "hasFile": f.existing}, map[string]any{"id": 3, "seriesId": 1, "hasFile": f.existing}}
			}
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
				if f.multiEpisode {
					row["episodes"] = []any{map[string]any{"id": 2}, map[string]any{"id": 3}}
				}
			}
			body = []any{row}
			if f.duplicate {
				body = []any{row, row}
			}
		case "/api/v3/qualitydefinition":
			if f.startOnLookup {
				f.activeCommand = "ManualImport"
			}
			body = []any{map[string]any{"quality": map[string]any{"id": 3, "name": "WEBDL-1080p"}}}
		case "/api/v3/language":
			body = []any{map[string]any{"id": 1, "name": "English"}}
		case "/api/v3/command":
			if r.Method == "GET" {
				body = []any{}
				if f.activeCommand != "" {
					body = []any{map[string]any{"id": 8, "name": f.activeCommand, "status": "started"}}
				}
				break
			}
			if r.Method != "POST" {
				t.Fatalf("unexpected command method %s", r.Method)
			}
			f.posts++
			var command map[string]any
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Error(err)
			}
			f.mode, _ = command["importMode"].(string)
			if files, ok := command["files"].([]any); ok && len(files) > 0 {
				if file, ok := files[0].(map[string]any); ok {
					if episodes, ok := file["episodeIds"].([]any); ok {
						f.importedEpisodes = len(episodes)
					}
				}
			}
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
	f.backendURL = srv.URL
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

func TestActiveNativeImportBlocksEvenAfterServerRestart(t *testing.T) {
	for _, name := range []string{"ManualImport", "DownloadedEpisodesScan", "DownloadedMoviesScan", "ProcessMonitoredDownloads"} {
		t.Run(name, func(t *testing.T) {
			f, cs := guardedFixture(t, false)
			plan := previewPlan(t, cs, "sonarr")
			importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan, "acceptCleanup": true}, true)
			f.mu.Lock()
			f.activeCommand = name
			f.mu.Unlock()
			cfg := cfgWith(map[string][]config.Instance{"sonarr": {{Name: "main", URL: f.backendURL, APIKey: "fixture"}}}, permsFull)
			cfg.Imports = config.ImportPolicy{Guarded: true}
			restarted := connect(t, cfg)
			importCall(t, restarted, "sonarr", "_manual_import_preview", map[string]any{"downloadId": "HASH", "seriesId": 1}, false)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 {
				t.Fatal("native import was replayed")
			}
		})
	}
}

func TestGuardedImportQueueTargetPaginationAndDuplicateMappings(t *testing.T) {
	for _, tc := range []struct {
		name               string
		target, total      int
		duplicate, success bool
	}{{"wrong-target", 9, 1, false, false}, {"unknown-target", 0, 1, false, false}, {"two-pages", 1, 150, false, true}, {"too-large", 1, 1001, false, false}, {"duplicate", 1, 1, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f, cs := guardedFixture(t, false)
			f.mu.Lock()
			f.queueTarget = tc.target
			f.totalQueue = tc.total
			f.duplicate = tc.duplicate
			f.mu.Unlock()
			importCall(t, cs, "sonarr", "_manual_import_preview", map[string]any{"downloadId": "HASH", "seriesId": 1}, tc.success)
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 0 {
				t.Fatal("preview submitted an import")
			}
			if tc.success && f.queueReads != 2 {
				t.Fatal("second queue page was not checked")
			}
		})
	}
}

func TestConcurrentGuardedAppliesSubmitOnlyOnce(t *testing.T) {
	f, cs := guardedFixture(t, false)
	plans := []string{previewPlan(t, cs, "sonarr"), previewPlan(t, cs, "sonarr")}
	outcomes := make(chan bool, 2)
	for _, plan := range plans {
		go func(id string) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sonarr_manual_import", Arguments: map[string]any{"planId": id, "acceptCleanup": true}})
			outcomes <- err == nil && !res.IsError
		}(plan)
	}
	successes := 0
	for range plans {
		if <-outcomes {
			successes++
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if successes != 1 || f.posts != 1 {
		t.Fatalf("successes=%d posts=%d", successes, f.posts)
	}
}

func TestGuardedImportRechecksCommandsImmediatelyBeforePost(t *testing.T) {
	f, cs := guardedFixture(t, false)
	plan := previewPlan(t, cs, "sonarr")
	f.mu.Lock()
	f.startOnLookup = true
	f.mu.Unlock()
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan, "acceptCleanup": true}, false)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 0 {
		t.Fatal("import was submitted after another native import started")
	}
}

func TestGuardedImportFencesTheTargetsOwnPathOutsideConfiguredRoots(t *testing.T) {
	f, cs := guardedFixture(t, false)
	f.mu.Lock()
	f.targetPath = "/legacy/show"
	f.sourcePath = "/legacy/show/a.mkv"
	f.mu.Unlock()
	importCall(t, cs, "sonarr", "_manual_import_preview", map[string]any{"downloadId": "HASH", "seriesId": 1}, false)
}

func TestGuardedImportPreservesMultiEpisodeMappings(t *testing.T) {
	f, cs := guardedFixture(t, false)
	f.mu.Lock()
	f.multiEpisode = true
	f.mu.Unlock()
	plan := previewPlan(t, cs, "sonarr")
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan, "acceptCleanup": true}, true)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.importedEpisodes != 2 {
		t.Fatalf("imported episode mappings=%d, want 2", f.importedEpisodes)
	}
}

func TestMissingCleanupAcceptanceLeavesGuardedPlanUnspent(t *testing.T) {
	f, cs := guardedFixture(t, false)
	plan := previewPlan(t, cs, "sonarr")
	// Input refusal occurs before plan validation and must not consume it.
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan}, false)
	importCall(t, cs, "sonarr", "_manual_import", map[string]any{"planId": plan, "acceptCleanup": true}, true)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatal("input refusal spent the plan")
	}
}
