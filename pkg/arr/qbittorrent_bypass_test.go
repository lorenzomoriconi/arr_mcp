package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestQBittorrentExplicitTrustedSourceBypassSkipsLogin(t *testing.T) {
	for _, status := range []int{200, 401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var logins, reads atomic.Int32
			baseURL := ""
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/auth/login" {
					logins.Add(1)
					_, _ = w.Write([]byte("Fails."))
					return
				}
				reads.Add(1)
				if r.Header.Get("Referer") != baseURL || r.Header.Get("Origin") != baseURL || r.Header.Get("Cookie") != "" {
					t.Error("trusted-source requests lost origin protection or invented a session")
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("v5.2.4"))
			}))
			defer endpoint.Close()
			baseURL = endpoint.URL
			client := NewClient(endpoint.URL, QBittorrentSpec, Credentials{AuthBypass: true})
			_, err := client.Get(context.Background(), "/app/version")
			if (err == nil) != (status == 200) {
				t.Fatal("trusted-source denial was not propagated", err)
			}
			if logins.Load() != 0 || reads.Load() != 1 {
				t.Fatalf("unexpected login/retry: logins=%d reads=%d", logins.Load(), reads.Load())
			}
		})
	}
}

func TestTrustedSourceFlagNeverWeakensOtherOrCredentialedAuth(t *testing.T) {
	for _, creds := range []Credentials{
		{Username: "fixture", Password: "fixture", AuthBypass: true},
		{APIKey: "fixture", AuthBypass: true},
		{},
	} {
		var logins, reads atomic.Int32
		endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v2/auth/login" {
				logins.Add(1)
				_, _ = w.Write([]byte("Fails."))
				return
			}
			reads.Add(1)
			_, _ = w.Write([]byte("v5.2.4"))
		}))
		client := NewClient(endpoint.URL, QBittorrentSpec, creds)
		_, err := client.Get(context.Background(), "/app/version")
		endpoint.Close()
		if err == nil || logins.Load() != 1 || reads.Load() != 0 {
			t.Fatal("normal login was bypassed without an empty-credential explicit opt-in")
		}
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "fixture" {
			t.Error("another service's authentication was removed")
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer endpoint.Close()
	client := NewClient(endpoint.URL, SonarrSpec, Credentials{APIKey: "fixture", AuthBypass: true})
	if _, err := client.Get(context.Background(), "/system/status"); err != nil {
		t.Fatal(err)
	}
}
