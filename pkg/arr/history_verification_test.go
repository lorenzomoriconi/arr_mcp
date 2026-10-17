package arr

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestHistoryPreservesImportEvidenceAndTrimsOtherData(t *testing.T) {
	srv, _ := fakeService(t, 200, `{"records":[{"id":9,"eventType":"downloadFolderImported","downloadId":"ABC","episodeId":2,"seriesId":1,"movieId":3,"data":{"episodeFileId":"77","movieFileId":"88","droppedPath":"/downloads/a.mkv","importedPath":"/library/a.mkv","downloadUrl":"https://secret.invalid/key"}}]}`)
	records, err := ListHistory(context.Background(), NewClient(srv.URL, SonarrSpec, Credentials{APIKey: "fixture"}), 20)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(records)
	body := string(encoded)
	for _, want := range []string{`"downloadId":"ABC"`, `"episodeId":2`, `"seriesId":1`, `"movieId":3`, `"episodeFileId":"77"`, `"movieFileId":"88"`, `"droppedPath":"/downloads/a.mkv"`, `"importedPath":"/library/a.mkv"`} {
		if !strings.Contains(body, want) {
			t.Errorf("lost history evidence %s: %s", want, body)
		}
	}
	if strings.Contains(body, "secret.invalid") {
		t.Fatal("unrelated history data leaked")
	}
}

func TestHistoryPageScopesByDownloadAndReportsTotal(t *testing.T) {
	srv, got := fakeService(t, 200, `{"page":2,"pageSize":50,"totalRecords":101,"records":[{"id":9,"downloadId":"ABC"}]}`)
	out, err := ListHistoryPage(context.Background(), NewClient(srv.URL, SonarrSpec, Credentials{APIKey: "fixture"}), HistoryQuery{Page: 2, Limit: 50, DownloadID: "ABC", SeriesID: 1, EpisodeID: 2})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(got.query)
	for k, v := range map[string]string{"page": "2", "pageSize": "50", "downloadId": "ABC", "seriesIds": "1", "episodeId": "2"} {
		if q.Get(k) != v {
			t.Errorf("%s=%q, want %q", k, q.Get(k), v)
		}
	}
	if out.Total != 101 || out.Page != 2 || len(out.Records) != 1 {
		t.Fatalf("lost page metadata: %+v", out)
	}
}

func TestHistoryPageRejectsInvalidPaginationWithoutRequests(t *testing.T) {
	srv, got := fakeService(t, 200, `{}`)
	c := NewClient(srv.URL, SonarrSpec, Credentials{APIKey: "fixture"})
	for _, q := range []HistoryQuery{{Page: -1}, {Limit: 1001}, {Limit: -1}} {
		if _, err := ListHistoryPage(context.Background(), c, q); err == nil {
			t.Errorf("accepted invalid query: %+v", q)
		}
	}
	if len(got.paths) != 0 {
		t.Fatalf("invalid query contacted app: %v", got.paths)
	}
}

func TestImportFinalPreflightCanRefuseWithoutSubmitting(t *testing.T) {
	srv, got := fakeService(t, 200, `{}`)
	c := NewClient(srv.URL, SonarrSpec, Credentials{APIKey: "fixture"})
	expired := errors.New("expired plan")
	_, err := ManualImportChecked(context.Background(), c, []ManualImportFile{{Path: "/downloads/file.mkv"}}, "copy", func() error { return expired })
	if !errors.Is(err, expired) || len(got.paths) != 0 {
		t.Fatalf("preflight refusal did not stop submission: %v %v", err, got.paths)
	}
}
