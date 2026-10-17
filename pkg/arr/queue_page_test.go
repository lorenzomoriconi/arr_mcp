package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueuePagePreservesDownloadAndBlockedImportEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("pageSize") != "25" {
			t.Errorf("wrong queue query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalRecords":51,"records":[{"id":1,"downloadId":"HASH","seriesId":3,"episodeId":9,"trackedDownloadState":"importBlocked","statusMessages":[{"title":"Blocked","messages":["File unavailable"]}]}]}`))
	}))
	defer server.Close()
	out, err := ListQueuePage(context.Background(), NewClient(server.URL, SonarrSpec, Credentials{APIKey: "fixture"}), 2, 25)
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 51 || out.Page != 2 || len(out.Items) != 1 || out.Items[0].DownloadID != "HASH" || out.Items[0].EpisodeID != 9 || out.Items[0].TrackedDownloadState != "importBlocked" || len(out.Items[0].StatusMessages) != 1 {
		t.Fatalf("queue evidence lost: %+v", out)
	}
}
