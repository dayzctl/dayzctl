package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pageHost   = "https://dayz.xam.nu"
	staticHost = "https://static.xam.nu"
	// The static host rejects requests without a referer.
	referer    = "https://dayz.xam.nu/"
	coordSpace = 256
)

// source talks to dayz.xam.nu. It is deliberately polite: a small worker pool
// and a minimum gap between requests, because the CDN drops bursts.
type source struct {
	http    *http.Client
	gate    chan struct{}
	mu      sync.Mutex
	lastReq time.Time
	slots   chan struct{}
}

func newSource(workers int) *source {
	if workers < 1 {
		workers = 1
	}
	return &source{
		http:  &http.Client{Timeout: 60 * time.Second},
		gate:  make(chan struct{}, 1),
		slots: make(chan struct{}, workers),
	}
}

// xamConfig is the subset of the site's map JSON that dayzctl needs. The full
// document also carries loot tables and is several megabytes.
type xamConfig struct {
	Info struct {
		TileSet    string  `json:"tiles"`
		TileVer    string  `json:"tilev"`
		Size       float64 `json:"size"`
		Format     string  `json:"format"`
		GPSInvertY bool    `json:"gpsInvertY"`
	} `json:"info"`
	Markers struct {
		Locations []struct {
			Kind  string    `json:"w"`
			Pos   []float64 `json:"p"`
			Names []string  `json:"s"`
		} `json:"locations"`
	} `json:"markers"`
}

// meta converts the downloaded map data into the file the UI reads.
func (c *xamConfig) meta(id, rev string) *metaFile {
	size := c.Info.Size
	m := &metaFile{
		ID:          id,
		Name:        displayName(id),
		Size:        size,
		Format:      defaultString(c.Info.Format, "webp"),
		TileSize:    256,
		Source:      "dayz.xam.nu",
		Version:     rev,
		Attribution: "Map imagery © dayz.xam.nu (dayz.xam.nu/" + id + ")",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, loc := range c.Markers.Locations {
		if place, ok := toPlace(loc.Kind, loc.Pos, loc.Names, size, c.Info.GPSInvertY); ok {
			m.Names = append(m.Names, place)
		}
	}
	return m
}

// revision resolves the versioned file name the site uses for a map, e.g.
// "1.30-11" for nasdara. That mapping only exists inside the site's JavaScript
// bundle, so it is scraped from there.
func (s *source) revision(id string) (string, error) {
	page, err := s.get(pageHost + "/" + id)
	if err != nil {
		return "", fmt.Errorf("cannot reach dayz.xam.nu: %w", err)
	}
	m := regexp.MustCompile(`src="(/js/bundle[^"]+\.js)"`).FindSubmatch(page)
	if m == nil {
		return "", fmt.Errorf("cannot find the site's script bundle on /%s", id)
	}
	bundle, err := s.get(pageHost + string(m[1]))
	if err != nil {
		return "", fmt.Errorf("cannot fetch the site bundle: %w", err)
	}
	return versionFor(string(bundle), id)
}

// versionFor pulls `"nasdara":{"v":"1.30","i":11}` out of the bundle.
func versionFor(bundle, id string) (string, error) {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(id) +
		`"\s*:\s*\{\s*"v"\s*:\s*"([^"]+)"\s*,\s*"i"\s*:\s*(\d+)`)
	m := re.FindStringSubmatch(bundle)
	if m == nil {
		return "", fmt.Errorf("map %q is not published on dayz.xam.nu", id)
	}
	return m[1] + "-" + m[2], nil
}

func (s *source) config(id, rev string) (*xamConfig, error) {
	data, err := s.get(fmt.Sprintf("%s/dayz/json/%s/%s.json", staticHost, id, rev))
	if err != nil {
		return nil, err
	}
	var cfg xamConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("cannot parse map data: %w", err)
	}
	if cfg.Info.Size <= 0 || cfg.Info.TileVer == "" {
		return nil, fmt.Errorf("map %s: incomplete map data", id)
	}
	return &cfg, nil
}

func (s *source) tile(cfg *xamConfig, z, x, y int) ([]byte, error) {
	url := fmt.Sprintf("%s/dayz/maps/%s/%s/satellite/%d/%d/%d.%s",
		staticHost, cfg.Info.TileSet, cfg.Info.TileVer, z, x, y, defaultString(cfg.Info.Format, "webp"))
	return s.get(url)
}

// get performs a rate-limited request with the headers the map host insists on.
func (s *source) get(url string) ([]byte, error) {
	s.slots <- struct{}{}
	defer func() { <-s.slots }()

	s.gate <- struct{}{}
	if wait := 120*time.Millisecond - time.Since(s.lastReq); wait > 0 {
		time.Sleep(wait)
	}
	s.lastReq = time.Now()
	<-s.gate

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dayzctl-mapfetch/1.0 (+https://github.com/dayzctl/dayzctl)")
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "*/*")

	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// toPlace converts one site entry into DayZ world metres.
//
// The site works in a 256×256 square where the first coordinate is the latitude
// (south → north, so it runs from -256 to 0) and the second the longitude
// (west → east, 0 to 256). Maps that report gpsInvertY need the north/south
// axis flipped to match in-game GPS.
func toPlace(kind string, pos []float64, names []string, size float64, invertY bool) (place, bool) {
	if len(pos) < 2 || len(names) == 0 || size <= 0 {
		return place{}, false
	}
	name := titleCase(names[0])
	if name == "" {
		return place{}, false
	}
	lat, lng := pos[0], pos[1]
	x := lng / coordSpace * size
	y := (coordSpace + lat) / coordSpace * size
	if invertY {
		y = size - y
	}
	return place{Name: name, X: round1(x), Y: round1(y), Kind: kind}, true
}

// titleCase turns "dashtabad" into "Dashtabad" without touching all-caps names.
func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == strings.ToUpper(s) {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func displayName(id string) string {
	switch id {
	case "chernarusplus", "chernarus":
		return "Chernarus"
	case "livonia":
		return "Livonia"
	case "sakhal", "frostline":
		return "Sakhal"
	case "namalsk":
		return "Namalsk"
	case "deerisle":
		return "Deer Isle"
	}
	return titleCase(id)
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
