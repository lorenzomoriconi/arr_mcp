package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
)

// MagnetPreviewArgs selects one original public magnet and an existing native library target.
type MagnetPreviewArgs struct {
	InstanceArg
	ItemID       int    `json:"itemId" jsonschema:"existing series or movie id in this app"`
	Magnet       string `json:"magnet" jsonschema:"one original public/DHT v1 magnet; tracker hints are omitted"`
	ReleaseTitle string `json:"releaseTitle,omitempty" jsonschema:"original release label, only when the magnet has no dn"`
}

// MagnetAddArgs applies a server-owned magnet plan with explicit risk acceptance.
type MagnetAddArgs struct {
	InstanceArg
	PlanID                string `json:"planId" jsonschema:"opaque five-minute plan from magnet_preview"`
	AcceptUnverified      bool   `json:"acceptUnverified" jsonschema:"explicitly accept that label, quality, audio and availability are unverified"`
	AcceptReplacementRisk bool   `json:"acceptReplacementRisk,omitempty" jsonschema:"explicitly accept existing-library or overlapping-download replacement risks shown by the preview"`
}

// MagnetPreview reports native label matching and unverified manual-selection risks.
type MagnetPreview struct {
	PlanID                        string   `json:"planId"`
	ExpiresAt                     string   `json:"expiresAt"`
	ItemID                        int      `json:"itemId"`
	Title                         string   `json:"title"`
	ReleaseTitle                  string   `json:"releaseTitle"`
	Hash                          string   `json:"hash"`
	Category                      string   `json:"category"`
	ParsedQuality                 string   `json:"parsedQuality"`
	Languages                     []string `json:"languages"`
	EpisodeIDs                    []int    `json:"episodeIds,omitempty"`
	ExistingLibraryUnits          int      `json:"existingLibraryUnits"`
	OverlappingDownloads          []string `json:"overlappingDownloads"`
	RequiresReplacementAcceptance bool     `json:"requiresReplacementAcceptance"`
	AlreadyPresent                bool     `json:"alreadyPresent"`
	Warnings                      []string `json:"warnings"`
}

// MagnetAdded reports exact-hash visibility without claiming a completed import.
type MagnetAdded struct {
	Submitted      bool   `json:"submitted"`
	AlreadyPresent bool   `json:"alreadyPresent"`
	Hash           string `json:"hash"`
	Category       string `json:"category"`
	Imported       bool   `json:"imported"`
}
type magnetPlan struct {
	service, instance, snapshot string
	query                       MagnetPreviewArgs
	normalized                  publicMagnet
	expires                     time.Time
}
type magnetPlans struct {
	mu       sync.Mutex
	pending  map[string]magnetPlan
	busy     map[string]bool
	attempts map[string]time.Time
}

func newMagnetPlans() *magnetPlans {
	return &magnetPlans{pending: map[string]magnetPlan{}, busy: map[string]bool{}, attempts: map[string]time.Time{}}
}
func (p *magnetPlans) prune(now time.Time) {
	for key, plan := range p.pending {
		if !now.Before(plan.expires) {
			delete(p.pending, key)
		}
	}
	for key, expires := range p.attempts {
		if !now.Before(expires) {
			delete(p.attempts, key)
		}
	}
}

type nativeMediaTarget struct {
	ID               int    `json:"id"`
	Title            string `json:"title"`
	Path             string `json:"path"`
	QualityProfileID int    `json:"qualityProfileId"`
	HasFile          bool   `json:"hasFile"`
	MovieFile        struct {
		ID int `json:"id"`
	} `json:"movieFile"`
	Seasons []struct {
		Number int `json:"seasonNumber"`
	} `json:"seasons"`
}
type parsedMagnetInfo struct {
	Quality struct {
		Quality struct {
			Name string `json:"name"`
		} `json:"quality"`
	} `json:"quality"`
	Languages []struct {
		Name string `json:"name"`
	} `json:"languages"`
	SeasonNumbers          []int `json:"seasonNumbers"`
	SeasonNumber           int   `json:"seasonNumber"`
	EpisodeNumbers         []int `json:"episodeNumbers"`
	AbsoluteEpisodeNumbers []int `json:"absoluteEpisodeNumbers"`
	FullSeason             bool  `json:"fullSeason"`
	MultiSeason            bool  `json:"isMultiSeason"`
	PartialSeason          bool  `json:"isPartialSeason"`
}
type nativeMagnetParse struct {
	Series      *nativeMediaTarget `json:"series"`
	Movie       *nativeMediaTarget `json:"movie"`
	EpisodeInfo *parsedMagnetInfo  `json:"parsedEpisodeInfo"`
	MovieInfo   *parsedMagnetInfo  `json:"parsedMovieInfo"`
	Episodes    []struct {
		ID           int `json:"id"`
		SeriesID     int `json:"seriesId"`
		SeasonNumber int `json:"seasonNumber"`
	} `json:"episodes"`
}

func (s *Server) magnetContext(ctx context.Context, c *arr.Client, service string, in MagnetPreviewArgs, normalized publicMagnet) (MagnetPreview, string, *arr.Client, downloadBinding, error) {
	var zero MagnetPreview
	var binding downloadBinding
	if in.ItemID <= 0 {
		return zero, "", nil, binding, fmt.Errorf("an existing positive series or movie id is required")
	}
	noun := "series"
	if service == "radarr" {
		noun = "movie"
	}
	item, err := arr.GetJSON[nativeMediaTarget](ctx, c, fmt.Sprintf("/%s/%d", noun, in.ItemID))
	if err != nil {
		return zero, "", nil, binding, err
	}
	if item.ID != in.ItemID || item.QualityProfileID <= 0 || !cleanMediaPath(item.Path) {
		return zero, "", nil, binding, fmt.Errorf("library target identity, profile or path unavailable")
	}
	parsed, err := arr.GetJSON[nativeMagnetParse](ctx, c, "/parse", arr.Query{"title": normalized.Title})
	if err != nil {
		return zero, "", nil, binding, err
	}
	match, info := parsed.Series, parsed.EpisodeInfo
	if service == "radarr" {
		match, info = parsed.Movie, parsed.MovieInfo
	}
	if match == nil || match.ID != item.ID || info == nil {
		return zero, "", nil, binding, fmt.Errorf("the app cannot match the original release label to this library title")
	}
	qb, binding, err := s.downloadBinding(ctx, c, service)
	if err != nil {
		return zero, "", nil, binding, err
	}
	prefs, err := arr.GetJSON[struct {
		DHT bool `json:"dht"`
	}](ctx, qb, "/app/preferences")
	if err != nil {
		return zero, "", nil, binding, err
	}
	if !prefs.DHT {
		return zero, "", nil, binding, fmt.Errorf("public magnets require DHT already enabled")
	}
	profile, err := arr.GetJSON[json.RawMessage](ctx, c, fmt.Sprintf("/qualityprofile/%d", item.QualityProfileID))
	if err != nil {
		return zero, "", nil, binding, err
	}
	result := MagnetPreview{ItemID: item.ID, Title: item.Title, ReleaseTitle: normalized.Title, Hash: normalized.Hash, Category: binding.Category, ParsedQuality: info.Quality.Quality.Name, OverlappingDownloads: []string{}, Warnings: []string{
		"Public/DHT magnets only. Tracker hints are omitted; source, peer and web-seed hints are refused.",
		"The original label is parsed, but actual files/audio, quality, size and availability are unverified. Automatic release selection checks are bypassed.",
		"Native import/upgrade handling can replace existing files; a completed download is not proof of library import.",
	}}
	for _, language := range info.Languages {
		result.Languages = append(result.Languages, language.Name)
	}
	var episodeState any
	if service == "sonarr" {
		seasons := info.SeasonNumbers
		if len(seasons) == 0 {
			if info.MultiSeason || info.PartialSeason {
				return zero, "", nil, binding, fmt.Errorf("multi/partial-season labels must identify explicit seasons")
			}
			seasons = []int{info.SeasonNumber}
		}
		for _, number := range seasons {
			if !slices.ContainsFunc(item.Seasons, func(v struct {
				Number int `json:"seasonNumber"`
			}) bool {
				return v.Number == number
			}) {
				return zero, "", nil, binding, fmt.Errorf("release identifies a season not present in the library target")
			}
		}
		if len(parsed.Episodes) == 0 || !info.FullSeason && len(info.EpisodeNumbers) == 0 && len(info.AbsoluteEpisodeNumbers) == 0 {
			return zero, "", nil, binding, fmt.Errorf("native parsing did not identify a definite episode scope")
		}
		current, err := arr.SonarrListEpisodes(ctx, c, item.ID)
		if err != nil {
			return zero, "", nil, binding, err
		}
		selected := []arr.Episode{}
		for _, mapped := range parsed.Episodes {
			if mapped.ID <= 0 || mapped.SeriesID != item.ID || !slices.Contains(seasons, mapped.SeasonNumber) || slices.Contains(result.EpisodeIDs, mapped.ID) {
				return zero, "", nil, binding, fmt.Errorf("inconsistent parsed episode mapping")
			}
			index := slices.IndexFunc(current, func(e arr.Episode) bool { return e.ID == mapped.ID && e.SeriesID == item.ID })
			if index < 0 {
				return zero, "", nil, binding, fmt.Errorf("parsed episode does not exist in selected library target")
			}
			episode := current[index]
			selected = append(selected, episode)
			result.EpisodeIDs = append(result.EpisodeIDs, mapped.ID)
			if episode.HasFile || episode.EpisodeFileID != 0 {
				result.ExistingLibraryUnits++
			}
		}
		slices.Sort(result.EpisodeIDs)
		slices.SortFunc(selected, func(a, b arr.Episode) int { return a.ID - b.ID })
		episodeState = selected
	} else if item.HasFile || item.MovieFile.ID != 0 {
		result.ExistingLibraryUnits = 1
	}
	overlaps, err := queueOverlap(ctx, c, service, item.ID, result.EpisodeIDs, normalized.Hash)
	if err != nil {
		return zero, "", nil, binding, err
	}
	result.OverlappingDownloads = overlaps
	existing, err := exactTorrent(ctx, qb, normalized.Hash)
	if err != nil {
		return zero, "", nil, binding, err
	}
	if existing != nil {
		if existing.Category != binding.Category && (binding.ImportedCategory == "" || existing.Category != binding.ImportedCategory) {
			return zero, "", nil, binding, fmt.Errorf("hash exists in another category; no torrent may be changed")
		}
		result.AlreadyPresent = true
	}
	result.RequiresReplacementAcceptance = !result.AlreadyPresent && (result.ExistingLibraryUnits > 0 || len(overlaps) > 0)
	signature, err := mediaFingerprint(struct {
		Target   nativeMediaTarget
		Binding  downloadBinding
		Profile  json.RawMessage
		Episodes any
		Overlaps []string
		Parsed   nativeMagnetParse
	}{item, binding, profile, episodeState, overlaps, parsed})
	return result, signature, qb, binding, err
}

func registerGuardedMagnets(s *Server, service string, spec arr.ServiceSpec) {
	register(s, service, spec, toolMeta{name: service + "_magnet_preview", description: "Preview one user-supplied public magnet for an existing library title. Uses that app's native qBittorrent client/category; never takes a save path. Actual audio/content and seed availability remain unverified.", access: AccessRead, nonIdempotent: true}, func(ctx context.Context, c *arr.Client, in MagnetPreviewArgs) (MagnetPreview, error) {
		var zero MagnetPreview
		normalized, err := normalizePublicMagnet(in.Magnet, in.ReleaseTitle)
		if err != nil {
			return zero, err
		}
		result, signature, _, _, err := s.magnetContext(ctx, c, service, in, normalized)
		if err != nil {
			return zero, err
		}
		instance, err := s.cfg.Resolve(service, in.Instance)
		if err != nil {
			return zero, err
		}
		s.magnets.mu.Lock()
		defer s.magnets.mu.Unlock()
		now := time.Now()
		s.magnets.prune(now)
		if len(s.magnets.pending) >= maxImportPlans || len(s.magnets.attempts) >= maxImportPlans {
			return zero, fmt.Errorf("too many recent magnet plans")
		}
		if _, recent := s.magnets.attempts[s.cfg.Downloads.Client+"/"+normalized.Hash]; recent {
			return zero, fmt.Errorf("recent magnet submission may still be pending; inspect the exact hash")
		}
		id := rand.Text()
		expires := now.Add(importPlanTTL)
		// Raw tracker hints never enter cached plans or logs.
		in.Magnet = normalized.URI
		s.magnets.pending[id] = magnetPlan{service: service, instance: instance.Name, snapshot: signature, query: in, normalized: normalized, expires: expires}
		result.PlanID = id
		result.ExpiresAt = expires.UTC().Format(time.RFC3339)
		s.log.Info("magnet planned: service=%s instance=%s hash=%s", service, instance.Name, normalized.Hash)
		return result, nil
	})
	register(s, service, spec, toolMeta{name: service + "_add_magnet", description: "Apply one fresh magnet preview after accepting unverified selection and any replacement risk. Rechecks native target/profile/client state, fixes automatic management and app category, and never accepts paths or arbitrary torrent sources. Verify the exact hash and library import afterwards.", access: AccessWrite}, func(ctx context.Context, c *arr.Client, in MagnetAddArgs) (MagnetAdded, error) {
		var zero MagnetAdded
		if !in.AcceptUnverified || in.PlanID == "" {
			return zero, fmt.Errorf("a fresh planId and acceptUnverified=true are required")
		}
		instance, err := s.cfg.Resolve(service, in.Instance)
		if err != nil {
			return zero, err
		}
		s.magnets.mu.Lock()
		s.magnets.prune(time.Now())
		plan, ok := s.magnets.pending[in.PlanID]
		if !ok || plan.service != service || plan.instance != instance.Name {
			s.magnets.mu.Unlock()
			return zero, fmt.Errorf("unknown, expired or consumed magnet plan")
		}
		key := s.cfg.Downloads.Client + "/" + plan.normalized.Hash
		_, recent := s.magnets.attempts[key]
		if recent || s.magnets.busy[key] {
			s.magnets.mu.Unlock()
			return zero, fmt.Errorf("this hash has a validation or recent submission in progress")
		}
		delete(s.magnets.pending, in.PlanID)
		s.magnets.busy[key] = true
		s.magnets.mu.Unlock()
		defer func() { s.magnets.mu.Lock(); delete(s.magnets.busy, key); s.magnets.mu.Unlock() }()
		current, signature, qb, binding, err := s.magnetContext(ctx, c, service, plan.query, plan.normalized)
		if err != nil {
			return zero, err
		}
		if signature != plan.snapshot {
			return zero, fmt.Errorf("library, profile, native client or overlapping downloads changed; obtain a fresh preview")
		}
		if current.AlreadyPresent {
			return MagnetAdded{AlreadyPresent: true, Hash: current.Hash, Category: current.Category}, nil
		}
		if current.RequiresReplacementAcceptance && !in.AcceptReplacementRisk {
			return zero, fmt.Errorf("existing library files or overlapping downloads require explicit acceptReplacementRisk=true; obtain a fresh preview")
		}
		s.magnets.mu.Lock()
		if !time.Now().Before(plan.expires) {
			s.magnets.mu.Unlock()
			return zero, fmt.Errorf("magnet plan expired during validation")
		}
		s.magnets.attempts[key] = time.Now().Add(importPlanTTL)
		s.magnets.mu.Unlock()
		automatic := true
		s.log.Info("magnet submitting: service=%s instance=%s hash=%s", service, instance.Name, current.Hash)
		postErr := arr.QBittorrentAddTorrent(ctx, qb, arr.AddTorrentRequest{URLs: []string{plan.normalized.URI}, Category: binding.Category, AutoTMM: &automatic, Rename: plan.normalized.Title})
		// qBittorrent 5.2 can answer empty/text/JSON acknowledgements. Visibility
		// of this exact hash/category, rather than response text, is the receipt.
		for attempt := 0; attempt < 3; attempt++ {
			torrent, lookupErr := exactTorrent(ctx, qb, current.Hash)
			if lookupErr == nil && torrent != nil && (torrent.Category == binding.Category || binding.ImportedCategory != "" && torrent.Category == binding.ImportedCategory) {
				return MagnetAdded{Submitted: true, Hash: current.Hash, Category: torrent.Category}, nil
			}
			if attempt < 2 {
				select {
				case <-ctx.Done():
					return zero, fmt.Errorf("magnet outcome uncertain; inspect exact hash %s before retrying", current.Hash)
				case <-time.After(200 * time.Millisecond):
				}
			}
		}
		_ = postErr // HTTP/ack failure cannot override independently confirmed visibility.
		return zero, fmt.Errorf("magnet outcome uncertain; inspect exact hash %s and native history before retrying; this plan is spent", current.Hash)
	})
}
