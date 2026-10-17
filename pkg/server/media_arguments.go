package server

import (
	"context"
	"fmt"
	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
	"slices"
	"strings"
)

// Guarded deployments retain single-target controls and configured library
// roots while using upstream's native tool catalogue and argument types.
func validateMediaInput(ctx context.Context, c *arr.Client, value any) error {
	root := ""
	switch in := value.(type) {
	case EditSeriesArgs:
		if len(in.SeriesIDs) != 1 || in.SeriesIDs[0] <= 0 || in.QualityProfileID == nil || *in.QualityProfileID <= 0 || in.Monitored != nil || in.SeasonFolder != nil || in.RootFolderPath != "" || in.SeriesType != "" || in.MonitorNewItems != "" || len(in.Tags) > 0 || in.ApplyTags != "" || in.MoveFiles {
			return fmt.Errorf("guarded series edits only assign one selected quality profile to one existing series")
		}
	case EditMoviesArgs:
		if len(in.MovieIDs) != 1 || in.MovieIDs[0] <= 0 || in.QualityProfileID == nil || *in.QualityProfileID <= 0 || in.Monitored != nil || in.MinimumAvailability != "" || in.RootFolderPath != "" || len(in.Tags) > 0 || in.ApplyTags != "" || in.MoveFiles {
			return fmt.Errorf("guarded movie edits only assign one selected quality profile to one existing movie")
		}

	case TorrentHashesArgs:
		if len(in.Hashes) != 1 || !exactTorrentHashPattern.MatchString(in.Hashes[0]) {
			return fmt.Errorf("guarded download controls require one exact torrent hash; bulk selectors are forbidden")
		}
	case MovieIDsArgs:
		if len(in.MovieIDs) != 1 || in.MovieIDs[0] <= 0 {
			return fmt.Errorf("one positive movie id is required")
		}
	case SearchScopeArgs:
		if len(in.EpisodeIDs) > 0 {
			if len(in.EpisodeIDs) > 100 || slices.ContainsFunc(in.EpisodeIDs, func(id int) bool { return id <= 0 }) {
				return fmt.Errorf("search requires at most 100 positive explicit episode ids")
			}
		} else if in.SeriesID <= 0 || in.SeasonNumber != nil && *in.SeasonNumber < 0 {
			return fmt.Errorf("search requires one positive series id and a valid optional season")
		}
	case CreateQualityProfileArgs:
		if !strings.HasPrefix(in.Name, "Hermes - ") || strings.TrimSpace(strings.TrimPrefix(in.Name, "Hermes - ")) == "" {
			return fmt.Errorf("guarded profiles require the reserved Hermes - name prefix")
		}
	case AddSeriesArgs:
		if len(in.Tags) > 0 || in.SearchNow {
			return fmt.Errorf("guarded title creation refuses routing tags and searchNow; use a separate explicitly scoped search")
		}
		if in.TVDBID <= 0 || in.QualityProfileID <= 0 {
			return fmt.Errorf("a positive catalogue id and selected quality profile are required")
		}
		root = in.RootFolderPath
	case AddMovieArgs:
		if len(in.Tags) > 0 || in.SearchNow {
			return fmt.Errorf("guarded title creation refuses routing tags and searchNow; use a separate explicitly scoped search")
		}
		if in.TMDBID <= 0 || in.QualityProfileID <= 0 {
			return fmt.Errorf("a positive catalogue id and selected quality profile are required")
		}
		root = in.RootFolderPath
	default:
		return nil
	}
	if _, series := value.(AddSeriesArgs); series {
		if root == "" {
			return fmt.Errorf("select a configured native library root")
		}
	}
	if _, movie := value.(AddMovieArgs); movie {
		if root == "" {
			return fmt.Errorf("select a configured native library root")
		}
	}
	if root != "" {
		roots, err := arr.ListRootFolders(ctx, c)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(roots, func(folder arr.RootFolder) bool { return folder.Path == root }) {
			return fmt.Errorf("rootFolderPath must exactly match an existing native library root")
		}
	}
	return nil
}

// validateGuardedAction limits the delegated token's native mutations even when
// the client owns the token. Client-side approval is not an isolation boundary.
func (s *Server) validateGuardedAction(ctx context.Context, c *arr.Client, service, instance string, value any) error {
	if err := validateMediaInput(ctx, c, value); err != nil {
		return err
	}
	switch in := value.(type) {
	case UpdateQualityProfileArgs:
		if in.ID <= 0 {
			return fmt.Errorf("select one existing unassigned profile")
		}
		profile, err := arr.GetQualityProfile(ctx, c, in.ID)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(profile.Name, "Hermes - ") || in.Name != nil && (!strings.HasPrefix(*in.Name, "Hermes - ") || strings.TrimSpace(strings.TrimPrefix(*in.Name, "Hermes - ")) == "") {
			return fmt.Errorf("guarded mode only updates reserved Hermes - profiles and refuses renaming into or out of that namespace")
		}
		endpoint := "/series"
		if service == "radarr" {
			endpoint = "/movie"
		}
		rows, err := arr.GetJSON[[]struct {
			QualityProfileID int `json:"qualityProfileId"`
		}](ctx, c, endpoint)
		if err != nil {
			return err
		}
		if len(rows) > 10000 {
			return fmt.Errorf("library exceeds the guarded profile-check bound")
		}
		for _, row := range rows {
			if row.QualityProfileID == in.ID {
				return fmt.Errorf("guarded mode refuses edits to assigned quality profiles; create and configure a separate profile, then assign it to one title")
			}
		}
	case TorrentHashesArgs:
		selected, err := s.cfg.Resolve("qbittorrent", s.cfg.Downloads.Client)
		if err != nil {
			return err
		}
		if service != "qbittorrent" || instance != selected.Name {
			return fmt.Errorf("guarded torrent controls require the selected download client")
		}
		torrent, err := exactTorrent(ctx, c, in.Hashes[0])
		if err != nil {
			return err
		}
		if torrent == nil {
			return fmt.Errorf("torrent is not present in the selected media client")
		}
		categories := map[string]bool{}
		for _, app := range []string{"sonarr", "radarr"} {
			spec := arr.SonarrSpec
			if app == "radarr" {
				spec = arr.RadarrSpec
			}
			for _, target := range s.cfg.Services[app] {
				native := arr.NewClient(target.URL, spec, InstanceCredentials(&target))
				_, binding, err := s.downloadBinding(ctx, native, app)
				if err != nil {
					return err
				}
				categories[binding.Category] = true
				if binding.ImportedCategory != "" {
					categories[binding.ImportedCategory] = true
				}
			}
		}
		if !categories[torrent.Category] {
			return fmt.Errorf("guarded torrent controls only operate on the native media apps' categories")
		}
	}
	return nil
}

func guardedToolNote(name string) string {
	switch {
	case strings.HasSuffix(name, "_update_quality_profile"):
		return " Guarded mode: only unassigned profiles already named Hermes - ... may be updated; the reserved prefix cannot be removed."
	case strings.HasSuffix(name, "_create_quality_profile"):
		return " Guarded mode requires the reserved Hermes - ... name prefix."
	case name == "sonarr_edit_series" || name == "radarr_edit_movies":
		return " Guarded mode: assign a quality profile to exactly one existing title; other generic edit fields are refused."
	case name == "sonarr_add_series" || name == "radarr_add_movie":
		return " Guarded mode: no tags or searchNow; use an existing native root and a separate explicitly scoped search."
	case name == "qbittorrent_stop_torrents" || name == "qbittorrent_start_torrents" || name == "qbittorrent_recheck_torrents" || name == "qbittorrent_reannounce_torrents":
		return " Guarded mode: exactly one hash in a native media-app category; all/bulk selectors are forbidden."
	}
	return ""
}
