package server

import (
	"context"
	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
	"strings"
	"testing"
)

func TestGuardedMediaRefusesBulkOrUnscopedActions(t *testing.T) {
	for _, value := range []any{
		TorrentHashesArgs{Hashes: []string{"all"}}, TorrentHashesArgs{Hashes: []string{strings.Repeat("a", 40), strings.Repeat("b", 40)}}, TorrentHashesArgs{},
		EditSeriesArgs{SeriesIDs: []int{1}, RootFolderPath: "/config", MoveFiles: true},
		EditMoviesArgs{MovieIDs: []int{1, 2}},
		MovieIDsArgs{MovieIDs: []int{}}, MovieIDsArgs{MovieIDs: []int{1, 2}}, SearchScopeArgs{SeriesID: 0}, SearchScopeArgs{EpisodeIDs: []int{-1}},
	} {
		if err := validateMediaInput(context.Background(), nil, value); err == nil {
			t.Fatalf("unsafe scope accepted: %+v", value)
		}
	}
	if err := validateMediaInput(context.Background(), nil, TorrentHashesArgs{Hashes: []string{strings.Repeat("a", 40)}}); err != nil {
		t.Fatal(err)
	}
}

func TestGuardedAddsRefuseRoutingTagsOrImplicitSearch(t *testing.T) {
	f, _ := mediaFixture(t)
	client := arr.NewClient(f.baseURL, arr.SonarrSpec, arr.Credentials{APIKey: "fixture"})
	for _, value := range []any{
		AddSeriesArgs{TVDBID: 1, QualityProfileID: 1, RootFolderPath: "/library", Monitor: "none", Tags: []int{1}},
		AddMovieArgs{TMDBID: 1, QualityProfileID: 1, RootFolderPath: "/library", Tags: []int{1}},
		AddSeriesArgs{TVDBID: 1, QualityProfileID: 1, RootFolderPath: "/library", Monitor: "none", SearchNow: true},
		AddMovieArgs{TMDBID: 1, QualityProfileID: 1, RootFolderPath: "/library", SearchNow: true},
		CreateQualityProfileArgs{Name: "Operator preset"},
	} {
		if err := validateMediaInput(context.Background(), client, value); err == nil {
			t.Fatalf("unsafe add/profile creation accepted: %+v", value)
		}
	}
	if err := validateMediaInput(context.Background(), client, AddSeriesArgs{TVDBID: 1, QualityProfileID: 1, RootFolderPath: "/library", Monitor: "none"}); err != nil {
		t.Fatal(err)
	}
}
