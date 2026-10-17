package server

import (
	"context"
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
