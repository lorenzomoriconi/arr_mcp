package server

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/GauranshMathur/ARR_MCP/pkg/arr"
)

type publicMagnet struct{ Hash, Title, URI string }

var torrentHashPattern = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)
var exactTorrentHashPattern = regexp.MustCompile(`^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$`)

func normalizePublicMagnet(value, title string) (publicMagnet, error) {
	var zero publicMagnet
	if len(value) > 8192 || strings.ContainsAny(value, "\r\n\x00") {
		return zero, fmt.Errorf("one bounded public magnet is required")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "magnet" || parsed.Host != "" || parsed.Path != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return zero, fmt.Errorf("only one magnet URI is supported")
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return zero, fmt.Errorf("malformed magnet query")
	}
	for key, values := range query {
		if key != "xt" && key != "dn" && key != "tr" {
			return zero, fmt.Errorf("magnet source, peer, web-seed and unknown parameters are forbidden")
		}
		if key != "tr" && len(values) != 1 {
			return zero, fmt.Errorf("magnet requires exactly one hash and one label")
		}
	}
	xt := query.Get("xt")
	if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
		return zero, fmt.Errorf("a v1 BTIH hash is required")
	}
	hash := strings.TrimPrefix(strings.ToLower(xt), "urn:btih:")
	if len(hash) == 32 {
		decoded, e := base32.StdEncoding.DecodeString(strings.ToUpper(hash))
		if e != nil {
			return zero, fmt.Errorf("invalid base32 torrent hash")
		}
		hash = hex.EncodeToString(decoded)
	}
	if !torrentHashPattern.MatchString(hash) {
		return zero, fmt.Errorf("invalid v1 torrent hash")
	}
	label := query.Get("dn")
	if title == "" {
		title = label
	} else if label != "" && title != label {
		return zero, fmt.Errorf("releaseTitle must equal the original magnet label")
	}
	if title == "" || strings.TrimSpace(title) != title || len(title) > 500 || strings.ContainsAny(title, "/\\") || strings.Contains(title, "://") || title == "." || title == ".." || strings.ContainsFunc(title, unicode.IsControl) {
		return zero, fmt.Errorf("the original single-line release label is required; paths are forbidden")
	}
	canonical := url.Values{"xt": {"urn:btih:" + hash}, "dn": {title}}
	return publicMagnet{Hash: hash, Title: title, URI: "magnet:?" + canonical.Encode()}, nil
}

type downloadBinding struct {
	ClientID         int    `json:"clientId"`
	Category         string `json:"category"`
	ImportedCategory string `json:"importedCategory,omitempty"`
	Host             string `json:"host"`
	Port             int    `json:"port"`
	SSL              bool   `json:"ssl"`
	RemoveCompleted  bool   `json:"removeCompleted"`
	CategoryPath     string `json:"categoryPath"`
}

type nativeDownloadClient struct {
	ID              int    `json:"id"`
	Enable          bool   `json:"enable"`
	Implementation  string `json:"implementation"`
	RemoveCompleted *bool  `json:"removeCompletedDownloads"`
	Fields          []struct {
		Name  string          `json:"name"`
		Value json.RawMessage `json:"value"`
	} `json:"fields"`
}

// Only selected non-secret fields leave this function. App/provider credentials
// never enter a preview, nonce, fingerprint, audit event or model response.
func (s *Server) downloadBinding(ctx context.Context, c *arr.Client, service string) (*arr.Client, downloadBinding, error) {
	var zero downloadBinding
	if s.cfg.Downloads.Client == "" {
		return nil, zero, fmt.Errorf("downloads.client must select an operator-configured qBittorrent instance")
	}
	instance, err := s.cfg.Resolve("qbittorrent", s.cfg.Downloads.Client)
	if err != nil {
		return nil, zero, err
	}
	qb := arr.NewClient(instance.URL, arr.QBittorrentSpec, InstanceCredentials(instance))
	clients, err := arr.GetJSON[[]nativeDownloadClient](ctx, c, "/downloadclient")
	if err != nil {
		return nil, zero, err
	}
	enabled := []nativeDownloadClient{}
	for _, client := range clients {
		if client.Enable && client.Implementation == "QBittorrent" {
			enabled = append(enabled, client)
		}
	}
	if len(enabled) != 1 {
		return nil, zero, fmt.Errorf("exactly one enabled qBittorrent client must exist in the selected app")
	}
	selected := enabled[0]
	fields := map[string]json.RawMessage{}
	for _, field := range selected.Fields {
		fields[field.Name] = field.Value
	}
	text := func(name string) string { var value string; _ = json.Unmarshal(fields[name], &value); return value }
	prefix := "tv"
	if service == "radarr" {
		prefix = "movie"
	}
	binding := downloadBinding{ClientID: selected.ID, Category: text(prefix + "Category"), ImportedCategory: text(prefix + "ImportedCategory"), Host: text("host"), RemoveCompleted: selected.RemoveCompleted == nil || *selected.RemoveCompleted}
	_ = json.Unmarshal(fields["port"], &binding.Port)
	_ = json.Unmarshal(fields["useSsl"], &binding.SSL)
	endpoint, err := url.Parse(instance.URL)
	if err != nil {
		return nil, zero, err
	}
	port := endpoint.Port()
	if port == "" {
		port = "80"
		if endpoint.Scheme == "https" {
			port = "443"
		}
	}
	// Binding the native client to the configured endpoint avoids using another
	// qBittorrent instance which happens to contain an identically named category.
	if !strings.EqualFold(endpoint.Hostname(), binding.Host) || port != strconv.Itoa(binding.Port) || (endpoint.Scheme == "https") != binding.SSL || strings.Trim(endpoint.Path, "/") != strings.Trim(text("urlBase"), "/") {
		return nil, zero, fmt.Errorf("app download client does not match the configured qBittorrent endpoint")
	}
	if binding.Category == "" || len(binding.Category) > 100 || strings.ContainsFunc(binding.Category, unicode.IsControl) {
		return nil, zero, fmt.Errorf("app download category is unavailable")
	}
	categories, err := arr.QBittorrentListCategories(ctx, qb)
	if err != nil {
		return nil, zero, err
	}
	found := false
	for _, category := range categories {
		if category.Name == binding.Category {
			found = true
			binding.CategoryPath = category.SavePath
		}
	}
	if !found {
		return nil, zero, fmt.Errorf("app category is missing in qBittorrent")
	}
	handling, err := arr.GetJSON[struct {
		Enabled bool `json:"enableCompletedDownloadHandling"`
	}](ctx, c, "/config/downloadclient")
	if err != nil {
		return nil, zero, err
	}
	if !handling.Enabled {
		return nil, zero, fmt.Errorf("native completed-download handling must already be enabled")
	}
	return qb, binding, nil
}

type mediaQueueRow struct {
	ID         int    `json:"id"`
	SeriesID   int    `json:"seriesId"`
	MovieID    int    `json:"movieId"`
	EpisodeID  int    `json:"episodeId"`
	DownloadID string `json:"downloadId"`
	State      string `json:"trackedDownloadState"`
}

func queueOverlap(ctx context.Context, c *arr.Client, service string, target int, units []int, exclude string) ([]string, error) {
	field := "seriesId"
	if service == "radarr" {
		field = "movieId"
	}
	rows, err := arr.GetJSON[[]mediaQueueRow](ctx, c, "/queue/details", arr.Query{field: strconv.Itoa(target)})
	if err != nil {
		return nil, err
	}
	if len(rows) > 10000 {
		return nil, fmt.Errorf("native queue detail exceeds supported bound")
	}
	hashes := []string{}
	for _, row := range rows {
		if exclude != "" && strings.EqualFold(row.DownloadID, exclude) {
			continue
		}
		relevant := service == "radarr" && row.MovieID == target || service == "sonarr" && row.SeriesID == target && slices.Contains(units, row.EpisodeID)
		if relevant {
			hash := strings.ToLower(row.DownloadID)
			if hash == "" {
				hash = "queue:" + strconv.Itoa(row.ID)
			}
			hashes = append(hashes, hash)
		}
	}
	slices.Sort(hashes)
	return slices.Compact(hashes), nil
}

func mediaFingerprint(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type downloadTorrent struct {
	Hash        string   `json:"hash"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	SavePath    string   `json:"save_path"`
	ContentPath string   `json:"content_path"`
	State       string   `json:"state"`
	AmountLeft  *int64   `json:"amount_left"`
	Progress    *float64 `json:"progress"`
	AutoTMM     bool     `json:"auto_tmm"`
}

func exactTorrent(ctx context.Context, qb *arr.Client, hash string) (*downloadTorrent, error) {
	if !exactTorrentHashPattern.MatchString(hash) {
		return nil, fmt.Errorf("one exact torrent hash is required")
	}
	rows, err := arr.GetJSON[[]downloadTorrent](ctx, qb, "/torrents/info", arr.Query{"hashes": hash})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) != 1 || !strings.EqualFold(rows[0].Hash, hash) {
		return nil, fmt.Errorf("inconsistent exact-hash response")
	}
	return &rows[0], nil
}

func cleanMediaPath(value string) bool {
	return value != "/" && path.IsAbs(value) && path.Clean(value) == value && !strings.ContainsAny(value, "\\\r\n\x00")
}
