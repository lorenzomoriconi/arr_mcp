package server

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
)

func validatedManifest(torrent downloadTorrent, manifest []arr.TorrentFile, files []arr.ManualImportCandidate, binding downloadBinding) (string, error) {
	if torrent.AmountLeft == nil || *torrent.AmountLeft != 0 || torrent.Progress == nil || *torrent.Progress != 1 || !slices.Contains([]string{"uploading", "stalledUP", "stoppedUP", "queuedUP"}, torrent.State) {
		return "", fmt.Errorf("torrent is not complete and stable")
	}
	if torrent.Category != binding.Category && (binding.ImportedCategory == "" || torrent.Category != binding.ImportedCategory) {
		return "", fmt.Errorf("torrent category does not match the selected native app")
	}
	if !cleanMediaPath(torrent.SavePath) || !cleanMediaPath(torrent.ContentPath) || len(manifest) == 0 || len(manifest) > 10000 {
		return "", fmt.Errorf("download paths or file manifest unavailable")
	}
	paths := map[string]arr.TorrentFile{}
	indices := map[int]bool{}
	for _, file := range manifest {
		if file.Name == "" || path.IsAbs(file.Name) || path.Clean(file.Name) != file.Name || strings.ContainsAny(file.Name, "\\\r\n\x00") || file.Name == ".." || strings.HasPrefix(file.Name, "../") || file.Index < 0 || indices[file.Index] {
			return "", fmt.Errorf("unsafe or duplicated torrent file entry")
		}
		source := path.Join(torrent.SavePath, file.Name)
		if source != torrent.ContentPath && !strings.HasPrefix(source, torrent.ContentPath+"/") {
			return "", fmt.Errorf("manifest file is outside the torrent content root")
		}
		if _, duplicate := paths[source]; duplicate {
			return "", fmt.Errorf("duplicate manifest source")
		}
		paths[source] = file
		indices[file.Index] = true
	}
	for _, candidate := range files {
		file, ok := paths[candidate.Path]
		if !ok || file.Size <= 0 || file.Size != candidate.Size || file.Progress != 1 || file.Priority <= 0 {
			return "", fmt.Errorf("native import source is not an exact completed wanted torrent file")
		}
	}
	ordered := slices.Clone(manifest)
	slices.SortFunc(ordered, func(a, b arr.TorrentFile) int { return a.Index - b.Index })
	return mediaFingerprint(struct {
		Hash, Category, SavePath, ContentPath string
		AutoTMM                               bool
		Files                                 []arr.TorrentFile
	}{strings.ToLower(torrent.Hash), torrent.Category, torrent.SavePath, torrent.ContentPath, torrent.AutoTMM, ordered})
}

// An explicit download client upgrades the source baseline to independently
// bound wanted-file evidence. No filesystem or media mount is needed.
func (s *Server) guardedImportSnapshot(ctx context.Context, c *arr.Client, service string, in ManualImportPreviewArgs, files []arr.ManualImportCandidate) (string, []string, error) {
	base, err := importSnapshot(files)
	if err != nil {
		return "", nil, err
	}
	if s.cfg.Downloads.Client == "" {
		return base, nil, nil
	}
	qb, binding, err := s.downloadBinding(ctx, c, service)
	if err != nil {
		return "", nil, err
	}
	torrent, err := exactTorrent(ctx, qb, in.DownloadID)
	if err != nil {
		return "", nil, err
	}
	if torrent == nil {
		return "", nil, fmt.Errorf("completed download is not present in the configured qBittorrent instance")
	}
	manifest, err := arr.QBittorrentTorrentFiles(ctx, qb, in.DownloadID)
	if err != nil {
		return "", nil, err
	}
	evidence, err := validatedManifest(*torrent, manifest, files, binding)
	if err != nil {
		return "", nil, err
	}
	units := []int{}
	target := 0
	if service == "sonarr" {
		target = *in.SeriesID
		for _, file := range files {
			units = append(units, file.EpisodeIDs...)
		}
	} else {
		target = *in.MovieID
	}
	overlaps, err := queueOverlap(ctx, c, service, target, units, in.DownloadID)
	if err != nil {
		return "", nil, err
	}
	signature, err := mediaFingerprint(struct {
		Candidates, Manifest string
		Binding              downloadBinding
		Overlaps             []string
	}{base, evidence, binding, overlaps})
	return signature, overlaps, err
}
