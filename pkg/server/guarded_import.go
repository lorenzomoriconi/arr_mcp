package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
)

const importPlanTTL = 5 * time.Minute
const maxImportPlans = 256

type importPlan struct {
	service  string
	instance string
	query    ManualImportPreviewArgs
	snapshot string
	expires  time.Time
}

type importPlans struct {
	mu       sync.Mutex
	pending  map[string]importPlan
	attempts map[string]time.Time
}

func newImportPlans() *importPlans {
	return &importPlans{pending: map[string]importPlan{}, attempts: map[string]time.Time{}}
}

// Callers hold mu; tests may call this without concurrency.
func (p *importPlans) prune(now time.Time) {
	for id, plan := range p.pending {
		if !now.Before(plan.expires) {
			delete(p.pending, id)
		}
	}
	for key, expires := range p.attempts {
		if !now.Before(expires) {
			delete(p.attempts, key)
		}
	}
}

func importKey(service, instance, downloadID string) string {
	return service + "/" + instance + "/" + strings.ToUpper(downloadID)
}

func importSnapshot(files []arr.ManualImportCandidate) (string, error) {
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(a, b arr.ManualImportCandidate) int { return strings.Compare(a.Path, b.Path) })
	raw, err := json.Marshal(ordered)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Server) previewGuardedImport(ctx context.Context, c *arr.Client, service string, in ManualImportPreviewArgs) (ManualImportCandidateList, error) {
	var zero ManualImportCandidateList
	inst, err := s.cfg.Resolve(service, in.Instance)
	if err != nil {
		return zero, err
	}
	files, warnings, err := guardedImportFiles(ctx, c, service, in)
	if err != nil {
		return zero, err
	}
	snapshot, err := importSnapshot(files)
	if err != nil {
		return zero, err
	}
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	now := time.Now()
	s.imports.prune(now)
	key := importKey(service, inst.Name, in.DownloadID)
	if _, ok := s.imports.attempts[key]; ok {
		return zero, fmt.Errorf("a recent submission for this download may still be running; inspect commands and history before making another plan")
	}
	if len(s.imports.pending) >= maxImportPlans || len(s.imports.attempts) >= maxImportPlans {
		return zero, fmt.Errorf("too many recent import plans; wait for plans to expire")
	}
	id := rand.Text()
	expires := now.Add(importPlanTTL)
	s.imports.pending[id] = importPlan{service: service, instance: inst.Name, query: in, snapshot: snapshot, expires: expires}
	return ManualImportCandidateList{Files: files, Count: len(files), PlanID: id, ExpiresAt: expires.UTC().Format(time.RFC3339), Warnings: warnings}, nil
}

func (s *Server) applyGuardedImport(ctx context.Context, c *arr.Client, service string, in ManualImportArgs) (arr.CommandResult, error) {
	var zero arr.CommandResult
	if in.PlanID == "" || len(in.Files) > 0 || (in.ImportMode != "" && in.ImportMode != "copy") {
		return zero, fmt.Errorf("guarded imports require only planId from a fresh preview; caller files and move/auto modes are forbidden")
	}
	if !in.AcceptCleanup {
		return zero, fmt.Errorf("acceptCleanup must be true: native completed-download handling may remove the torrent and source files even in copy mode")
	}
	inst, err := s.cfg.Resolve(service, in.Instance)
	if err != nil {
		return zero, err
	}
	// Serialise this server's imports through validation and submission. This
	// cannot lock out native app jobs or other API clients.
	s.imports.mu.Lock()
	defer s.imports.mu.Unlock()
	s.imports.prune(time.Now())
	plan, ok := s.imports.pending[in.PlanID]
	if !ok || plan.service != service || plan.instance != inst.Name {
		return zero, fmt.Errorf("unknown, expired or already consumed import plan; obtain a fresh preview")
	}
	key := importKey(service, inst.Name, plan.query.DownloadID)
	if _, ok := s.imports.attempts[key]; ok {
		return zero, fmt.Errorf("this download already has a recent submission; inspect commands and history")
	}
	files, _, err := guardedImportFiles(ctx, c, service, plan.query)
	if err != nil {
		return zero, err
	}
	snapshot, err := importSnapshot(files)
	if err != nil {
		return zero, err
	}
	if snapshot != plan.snapshot {
		return zero, fmt.Errorf("import preview changed; obtain and review a fresh preview")
	}
	payload := make([]arr.ManualImportFile, 0, len(files))
	for _, f := range files {
		payload = append(payload, arr.ManualImportFile{Path: f.Path, SeriesID: f.SeriesID, EpisodeIDs: f.EpisodeIDs, MovieID: f.MovieID, Quality: f.Quality, Languages: f.Languages, ReleaseGroup: f.ReleaseGroup, DownloadID: f.DownloadID})
	}
	submitted := false
	command, err := arr.ManualImportChecked(ctx, c, payload, "copy", func() error {
		if !time.Now().Before(plan.expires) {
			delete(s.imports.pending, in.PlanID)
			return fmt.Errorf("import plan expired during validation; obtain a fresh preview")
		}
		// Consume before POST, including an HTTP failure or lost response.
		delete(s.imports.pending, in.PlanID)
		s.imports.attempts[key] = time.Now().Add(importPlanTTL)
		submitted = true
		return nil
	})
	if err != nil && submitted {
		return zero, fmt.Errorf("import submission outcome is uncertain; do not replay it; inspect commands and history: %w", err)
	}
	return command, err
}

// guardedImportFiles trusts the app's own scanner, not caller paths/mappings.
// It does not independently verify the download-client manifest or filesystem.
func guardedImportFiles(ctx context.Context, c *arr.Client, service string, in ManualImportPreviewArgs) ([]arr.ManualImportCandidate, []string, error) {
	if in.Folder != "" || in.DownloadID == "" || strings.TrimSpace(in.DownloadID) != in.DownloadID || len(in.DownloadID) > 128 {
		return nil, nil, fmt.Errorf("guarded import preview requires downloadId from the queue, never a folder")
	}
	targetID := 0
	if service == "sonarr" && in.SeriesID != nil && *in.SeriesID > 0 && in.MovieID == nil {
		targetID = *in.SeriesID
	}
	if service == "radarr" && in.MovieID != nil && *in.MovieID > 0 && in.SeriesID == nil {
		targetID = *in.MovieID
	}
	if targetID == 0 {
		return nil, nil, fmt.Errorf("guarded import preview requires one positive %s target id", service)
	}
	if err := importDownloadComplete(ctx, c, in.DownloadID, service, targetID); err != nil {
		return nil, nil, err
	}
	roots, err := arr.ListRootFolders(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	if len(roots) == 0 {
		return nil, nil, fmt.Errorf("no library roots available; refusing to import")
	}
	missing := map[int]bool{}
	if service == "sonarr" {
		episodes, e := arr.SonarrListEpisodes(ctx, c, targetID)
		if e != nil {
			return nil, nil, e
		}
		for _, episode := range episodes {
			if episode.SeriesID == targetID && !episode.HasFile && episode.EpisodeFileID == 0 {
				missing[episode.ID] = true
			}
		}
	} else {
		movie, e := arr.GetJSON[struct {
			ID          int  `json:"id"`
			HasFile     bool `json:"hasFile"`
			MovieFileID int  `json:"movieFileId"`
			MovieFile   struct {
				ID int `json:"id"`
			} `json:"movieFile"`
		}](ctx, c, fmt.Sprintf("/movie/%d", targetID))
		if e != nil {
			return nil, nil, e
		}
		if movie.ID == targetID && !movie.HasFile && movie.MovieFileID == 0 && movie.MovieFile.ID == 0 {
			missing[targetID] = true
		}
	}
	filter := false
	candidates, err := arr.ListManualImportCandidates(ctx, c, arr.ManualImportQuery{DownloadID: in.DownloadID, SeriesID: in.SeriesID, MovieID: in.MovieID, FilterExistingFiles: &filter})
	if err != nil {
		return nil, nil, err
	}
	files := make([]arr.ManualImportCandidate, 0, len(candidates))
	seenPaths := map[string]bool{}
	seenUnits := map[int]bool{}
	skipped := 0
	for _, f := range candidates {
		if len(f.Rejections) > 0 || f.SeriesID == 0 && f.MovieID == 0 {
			skipped++
			continue
		}
		if service == "sonarr" && (f.SeriesID != targetID || f.MovieID != 0) || service == "radarr" && (f.MovieID != targetID || f.SeriesID != 0) {
			return nil, nil, fmt.Errorf("preview contains a file matched to a different target")
		}
		if !strings.EqualFold(f.DownloadID, in.DownloadID) {
			return nil, nil, fmt.Errorf("preview downloadId does not match the requested download")
		}
		if !path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || strings.Contains(f.Path, "\\") || strings.ContainsAny(f.Path, "\r\n\x00") || f.Size <= 0 {
			return nil, nil, fmt.Errorf("preview contains an unsafe source path or invalid file size")
		}
		for _, root := range roots {
			rootPath := strings.TrimSuffix(path.Clean(root.Path), "/")
			if !path.IsAbs(root.Path) {
				return nil, nil, fmt.Errorf("library root is not an absolute path")
			}
			if f.Path == rootPath || strings.HasPrefix(f.Path, rootPath+"/") {
				return nil, nil, fmt.Errorf("source file is already inside a library root")
			}
		}
		if f.Quality == "" || len(f.Languages) == 0 {
			return nil, nil, fmt.Errorf("preview must identify quality and languages")
		}
		units := f.EpisodeIDs
		if service == "radarr" {
			units = []int{targetID}
		}
		if len(units) == 0 {
			skipped++
			continue
		}
		for _, id := range units {
			if id <= 0 || !missing[id] {
				return nil, nil, fmt.Errorf("target already has a library file or does not belong to this target")
			}
			if seenUnits[id] {
				return nil, nil, fmt.Errorf("preview maps multiple files to the same target unit")
			}
			seenUnits[id] = true
		}
		if seenPaths[f.Path] {
			return nil, nil, fmt.Errorf("preview contains duplicate source paths")
		}
		seenPaths[f.Path] = true
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no unambiguous, unrejected missing files are available to import")
	}
	warnings := []string{"Native completed-download handling may remove the torrent and source files even in copy mode; applying this plan requires acceptCleanup=true."}
	if skipped > 0 {
		warnings = append(warnings, fmt.Sprintf("Skipped %d rejected or unmatched files; they will not be imported.", skipped))
	}
	return files, warnings, nil
}

func importDownloadComplete(ctx context.Context, c *arr.Client, downloadID, service string, targetID int) error {
	found := false
	for page := 1; page <= 10; page++ {
		out, err := arr.GetJSON[struct {
			Total   int `json:"totalRecords"`
			Records []struct {
				DownloadID string   `json:"downloadId"`
				Status     string   `json:"status"`
				SizeLeft   *float64 `json:"sizeleft"`
				State      string   `json:"trackedDownloadState"`
				SeriesID   int      `json:"seriesId"`
				MovieID    int      `json:"movieId"`
			} `json:"records"`
		}](ctx, c, "/queue", arr.Query{"page": fmt.Sprint(page), "pageSize": "100"})
		if err != nil {
			return err
		}
		for _, row := range out.Records {
			if !strings.EqualFold(row.DownloadID, downloadID) {
				continue
			}
			found = true
			if !strings.EqualFold(row.Status, "completed") || row.SizeLeft == nil || *row.SizeLeft != 0 || row.State != "importBlocked" {
				return fmt.Errorf("download must be completed with zero bytes left and native importBlocked state")
			}
			if service == "sonarr" && row.SeriesID != 0 && row.SeriesID != targetID || service == "radarr" && row.MovieID != 0 && row.MovieID != targetID {
				return fmt.Errorf("download queue belongs to a different target")
			}
		}
		if page*100 >= out.Total {
			if !found {
				return fmt.Errorf("download is not present in the native queue")
			}
			return nil
		}
		if len(out.Records) == 0 {
			break
		}
	}
	return fmt.Errorf("native queue is too large or incomplete to validate safely")
}
