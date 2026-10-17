package server

import (
	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
	"strings"
	"testing"
)

func TestCompletedManifestBindsEveryImportSource(t *testing.T) {
	left := int64(0)
	progress := 1.0
	torrent := downloadTorrent{Hash: strings.Repeat("a", 40), Category: "tv", SavePath: "/downloads", ContentPath: "/downloads/pack", State: "stoppedUP", AmountLeft: &left, Progress: &progress}
	binding := downloadBinding{Category: "tv"}
	files := []arr.TorrentFile{{Index: 0, Name: "pack/a.mkv", Size: 100, Progress: 1, Priority: 1}, {Index: 1, Name: "pack/extra.txt", Size: 10, Progress: 0, Priority: 0}}
	candidates := []arr.ManualImportCandidate{{Path: "/downloads/pack/a.mkv", Size: 100}}
	original, err := validatedManifest(torrent, files, candidates, binding)
	if err != nil || original == "" {
		t.Fatal("valid manifest refused", err)
	}
	for _, kind := range []string{"incomplete", "unwanted", "wrong-size", "outside", "traversal", "duplicate", "missing-left", "moving", "category"} {
		t.Run(kind, func(t *testing.T) {
			current := torrent
			rows := append([]arr.TorrentFile(nil), files...)
			selected := append([]arr.ManualImportCandidate(nil), candidates...)
			switch kind {
			case "incomplete":
				rows[0].Progress = 0.99
			case "unwanted":
				rows[0].Priority = 0
			case "wrong-size":
				rows[0].Size = 99
			case "outside":
				selected[0].Path = "/other/a.mkv"
			case "traversal":
				rows[0].Name = "pack/../pack/a.mkv"
			case "duplicate":
				rows = append(rows, rows[0])
			case "missing-left":
				current.AmountLeft = nil
			case "moving":
				current.State = "moving"
			case "category":
				current.Category = "another"
			}
			if _, err := validatedManifest(current, rows, selected, binding); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
	files[1].Priority = 1
	changed, err := validatedManifest(torrent, files, candidates, binding)
	if err != nil || changed == original {
		t.Fatal("manifest priority change not bound into snapshot", err)
	}
}
