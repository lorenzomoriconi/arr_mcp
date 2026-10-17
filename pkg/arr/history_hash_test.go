package arr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeHistoryHashFiltersUseCanonicalCasing(t *testing.T) {
	for _, spec := range []ServiceSpec{SonarrSpec, RadarrSpec, ProwlarrSpec} {
		for _, input := range []string{strings.Repeat("ab", 20), strings.Repeat("Ab", 20), strings.Repeat("a", 64), "SABnzbd_nzo_aBcD", strings.Repeat("a", 32), strings.Repeat("a", 39) + "z"} {
			t.Run(spec.Name+"/"+input, func(t *testing.T) {
				wanted := input
				if spec.Name != "prowlarr" && (len(input) == 40 || len(input) == 64) && !strings.HasSuffix(input, "z") {
					wanted = strings.ToUpper(input)
				}
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Native apps return no records for an incorrectly cased exact filter.
					rows := []map[string]any{}
					if r.URL.Query().Get("downloadId") == wanted {
						rows = append(rows, map[string]any{"downloadId": wanted, "eventType": "downloadFolderImported"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"records": rows, "totalRecords": len(rows)})
				}))
				defer endpoint.Close()
				client := NewClient(endpoint.URL, spec, Credentials{APIKey: "fixture"})
				page, err := ListHistoryPage(context.Background(), client, HistoryQuery{DownloadID: input})
				if err != nil {
					t.Fatal(err)
				}
				if page.Total != 1 || len(page.Records) != 1 || page.Records[0].DownloadID != wanted {
					t.Fatal("exact native history was lost by a wrongly cased hash filter", page)
				}
			})
		}
	}
}
