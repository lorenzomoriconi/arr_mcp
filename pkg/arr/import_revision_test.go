package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManualImportRetainsNativeRevisionAndIndexerMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v3/manualimport":
			_, _ = w.Write([]byte(`[{"id":1,"path":"/downloads/a.mkv","size":100,"series":{"id":1},"episodes":[{"id":2}],"quality":{"quality":{"id":3,"name":"WEBDL-1080p"},"revision":{"version":2,"real":1,"isRepack":true}},"languages":[{"name":"English"}],"indexerFlags":8,"releaseType":"singleEpisode"}]`))
		case "/api/v3/qualitydefinition":
			_, _ = w.Write([]byte(`[{"quality":{"id":3,"name":"WEBDL-1080p"}}]`))
		case "/api/v3/language":
			_, _ = w.Write([]byte(`[{"id":1,"name":"English"}]`))
		case "/api/v3/command":
			var command struct {
				Files []struct {
					Quality struct {
						Revision QualityRevision `json:"revision"`
					} `json:"quality"`
					Flags       int    `json:"indexerFlags"`
					ReleaseType string `json:"releaseType"`
				} `json:"files"`
			}
			if err := json.NewDecoder(r.Body).Decode(&command); err != nil {
				t.Fatal(err)
			}
			if len(command.Files) != 1 || command.Files[0].Quality.Revision.Version != 2 || !command.Files[0].Quality.Revision.IsRepack || command.Files[0].Flags != 8 || command.Files[0].ReleaseType != "singleEpisode" {
				t.Errorf("native import metadata lost: %+v", command)
			}
			_, _ = w.Write([]byte(`{"id":1,"status":"queued"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, SonarrSpec, Credentials{APIKey: "fixture"})
	candidates, err := ListManualImportCandidates(context.Background(), client, ManualImportQuery{DownloadID: "HASH"})
	if err != nil {
		t.Fatal(err)
	}
	f := candidates[0]
	_, err = ManualImport(context.Background(), client, []ManualImportFile{{Path: f.Path, SeriesID: f.SeriesID, EpisodeIDs: f.EpisodeIDs, Quality: f.Quality, Languages: f.Languages, Revision: f.Revision, IndexerFlags: f.IndexerFlags, ReleaseType: f.ReleaseType}}, "copy")
	if err != nil {
		t.Fatal(err)
	}
}
