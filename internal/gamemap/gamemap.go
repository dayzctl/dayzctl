// Package gamemap supplies map imagery and place names for the log analyzer's
// map view.
//
// The tiles come from the community map at dayz.xam.nu, which publishes a
// standard {z}/{x}/{y} pyramid per map. Nothing is redistributed with dayzctl:
// tiles are proxied on demand and cached on disk, so a server that has fetched
// a map once keeps working without internet access.
package gamemap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/version"
)

const (
	staticHost = "https://static.xam.nu"
	pageHost   = "https://dayz.xam.nu"
	// Referer matters: the static host rejects requests without it.
	referer = "https://dayz.xam.nu/"

	// A map's tiles span the world square in 2^zoom tiles per side, one image
	// per tile. 256 is the pyramid's tile size.
	tileSize = 256
	// The site itself keeps a 256×256 coordinate space across the map.
	coordSpace = 256

	maxTileZoom = 7
	metaTTL     = 7 * 24 * time.Hour
	bundleTTL   = time.Hour
)

// Place is a named location on the map, already converted to DayZ world metres.
type Place struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Kind string  `json:"kind,omitempty"`
}

// Meta describes one map: its extent, how to fetch tiles, and its place names.
type Meta struct {
	ID         string    `json:"id"`
	Size       float64   `json:"size"`
	Format     string    `json:"format"`
	Version    string    `json:"version"`
	TileSet    string    `json:"tileSet"`
	TileSetVer string    `json:"tileSetVersion"`
	MaxZoom    int       `json:"maxZoom"`
	Source     string    `json:"source"`
	FetchedAt  time.Time `json:"fetchedAt"`
	Names      []Place   `json:"names"`
}

// Client fetches and caches map metadata and tiles.
type Client struct {
	cacheDir string
	http     *http.Client

	mu        sync.Mutex
	bundles   map[string]bundleEntry // map id → "1.30-11"
	bundleRaw string
	bundleAt  time.Time

	// Politeness: the CDN drops connections when hammered, so requests are
	// serialised with a small gap between them.
	gate     chan struct{}
	lastCall time.Time
}

type bundleEntry struct {
	version string // e.g. "1.30-11"
	at      time.Time
}

// New returns a client caching everything under cacheDir.
func New(cacheDir string) *Client {
	return &Client{
		cacheDir: cacheDir,
		http: &http.Client{
			Timeout: 45 * time.Second,
		},
		bundles: map[string]bundleEntry{},
		gate:    make(chan struct{}, 1),
	}
}

// CacheDir reports where map data is stored.
func (c *Client) CacheDir() string { return c.cacheDir }

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

// Meta returns the map description, preferring a cached copy.
func (c *Client) Meta(ctx context.Context, id string) (*Meta, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !validID(id) {
		return nil, fmt.Errorf("invalid map name %q", id)
	}

	cached, cacheErr := c.readMeta(id)
	if cacheErr == nil && time.Since(cached.FetchedAt) < metaTTL {
		return cached, nil
	}

	fresh, err := c.fetchMeta(ctx, id)
	if err != nil {
		if cacheErr == nil {
			logger.Debug("Serving stale map metadata", "map", id, "error", err)
			return cached, nil
		}
		return nil, err
	}
	if err := c.writeMeta(id, fresh); err != nil {
		logger.Debug("Cannot cache map metadata", "map", id, "error", err)
	}
	return fresh, nil
}

// xamConfig is the subset of the site's map JSON that dayzctl needs. The full
// document also carries loot tables and is several megabytes.
type xamConfig struct {
	Info struct {
		Tiles      string  `json:"tiles"`
		TileVer    string  `json:"tilev"`
		Size       float64 `json:"size"`
		Format     string  `json:"format"`
		MaxZoom    int     `json:"maxZoom"`
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

func (c *Client) fetchMeta(ctx context.Context, id string) (*Meta, error) {
	rev, err := c.revision(ctx, id)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/dayz/json/%s/%s.json", staticHost, id, rev)
	body, err := c.get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("map %s: %w", id, err)
	}

	var cfg xamConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("map %s: cannot parse map data: %w", id, err)
	}
	if cfg.Info.Size <= 0 || cfg.Info.TileVer == "" {
		return nil, fmt.Errorf("map %s: incomplete map data", id)
	}

	size := cfg.Info.Size
	meta := &Meta{
		ID:         id,
		Size:       size,
		Format:     defaultString(cfg.Info.Format, "webp"),
		Version:    rev,
		TileSet:    defaultString(cfg.Info.Tiles, id),
		TileSetVer: cfg.Info.TileVer,
		MaxZoom:    clampMaxZoom(cfg.Info.MaxZoom),
		Source:     "dayz.xam.nu",
		FetchedAt:  time.Now(),
	}

	for _, loc := range cfg.Markers.Locations {
		if place, ok := toPlace(loc.Kind, loc.Pos, loc.Names, size, cfg.Info.GPSInvertY); ok {
			meta.Names = append(meta.Names, place)
		}
	}

	logger.Debug("Map metadata fetched", "map", id, "size", size, "names", len(meta.Names), "version", rev)
	return meta, nil
}

// toPlace converts one site entry into DayZ world metres.
//
// The site works in a 256×256 square where the first coordinate is the latitude
// (south → north, so it runs from -256 to 0) and the second the longitude
// (west → east, 0 to 256). Maps that report gpsInvertY need the north/south
// axis flipped to match in-game GPS.
func toPlace(kind string, pos []float64, names []string, size float64, invertY bool) (Place, bool) {
	if len(pos) < 2 || len(names) == 0 || size <= 0 {
		return Place{}, false
	}
	name := titleCase(names[0])
	if name == "" {
		return Place{}, false
	}
	lat, lng := pos[0], pos[1]
	x := lng / coordSpace * size
	y := (coordSpace + lat) / coordSpace * size
	if invertY {
		y = size - y
	}
	return Place{Name: name, X: round1(x), Y: round1(y), Kind: kind}, true
}

// revision resolves the versioned file name the site uses for a map, e.g.
// "1.30-11" for nasdara. The mapping only exists inside the site's JavaScript
// bundle, so it is scraped from there and cached.
func (c *Client) revision(ctx context.Context, id string) (string, error) {
	c.mu.Lock()
	entry, ok := c.bundles[id]
	raw, rawAt := c.bundleRaw, c.bundleAt
	c.mu.Unlock()

	if ok && time.Since(entry.at) < bundleTTL {
		return entry.version, nil
	}

	if time.Since(rawAt) > bundleTTL || raw == "" {
		fetched, err := c.fetchBundle(ctx, id)
		if err != nil {
			return "", err
		}
		raw = fetched
		c.mu.Lock()
		c.bundleRaw, c.bundleAt = raw, time.Now()
		c.mu.Unlock()
	}

	rev, err := versionFor(raw, id)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	c.bundles[id] = bundleEntry{version: rev, at: time.Now()}
	c.mu.Unlock()
	return rev, nil
}

var bundleSrc = regexp.MustCompile(`src="(/js/bundle[^"]+\.js)"`)

func (c *Client) fetchBundle(ctx context.Context, id string) (string, error) {
	page, err := c.get(ctx, pageHost+"/"+id)
	if err != nil {
		return "", fmt.Errorf("map %s: cannot reach dayz.xam.nu: %w", id, err)
	}
	m := bundleSrc.FindSubmatch(page)
	if m == nil {
		return "", fmt.Errorf("map %s: cannot find the site's script bundle", id)
	}
	bundle, err := c.get(ctx, pageHost+string(m[1]))
	if err != nil {
		return "", fmt.Errorf("map %s: cannot fetch the site bundle: %w", id, err)
	}
	return string(bundle), nil
}

// versionFor pulls `"nasdara":{"v":"1.30","i":11}` out of the bundle.
func versionFor(bundle, id string) (string, error) {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(id) + `"\s*:\s*\{\s*"v"\s*:\s*"([^"]+)"\s*,\s*"i"\s*:\s*(\d+)`)
	m := re.FindStringSubmatch(bundle)
	if m == nil {
		return "", fmt.Errorf("map %q is not published on dayz.xam.nu", id)
	}
	return m[1] + "-" + m[2], nil
}

// ---------------------------------------------------------------------------
// Tiles
// ---------------------------------------------------------------------------

// Tile returns one tile image and its content type. Tiles are served from the
// disk cache when present.
func (c *Client) Tile(ctx context.Context, id string, z, x, y int) ([]byte, string, error) {
	meta, err := c.Meta(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if z < 0 || z > meta.MaxZoom {
		return nil, "", fmt.Errorf("zoom %d out of range (0-%d)", z, meta.MaxZoom)
	}
	limit := 1 << z
	if x < 0 || y < 0 || x >= limit || y >= limit {
		return nil, "", fmt.Errorf("tile %d/%d/%d out of range", z, x, y)
	}

	contentType := contentTypeFor(meta.Format)
	path := c.tilePath(meta, z, x, y)
	if data, err := os.ReadFile(path); err == nil {
		return data, contentType, nil
	}

	url := fmt.Sprintf("%s/dayz/maps/%s/%s/satellite/%d/%d/%d.%s",
		staticHost, meta.TileSet, meta.TileSetVer, z, x, y, meta.Format)

	data, err := c.get(ctx, url)
	if err != nil {
		return nil, "", fmt.Errorf("tile %d/%d/%d: %w", z, x, y, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			logger.Debug("Cannot cache map tile", "path", path, "error", err)
		}
	}
	return data, contentType, nil
}

func contentTypeFor(format string) string {
	switch strings.ToLower(format) {
	case "webp":
		return "image/webp"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

// get performs a politely rate-limited request with the headers the map host
// insists on.
func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	c.gate <- struct{}{}
	defer func() { <-c.gate }()

	if wait := 60*time.Millisecond - time.Since(c.lastCall); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c.lastCall = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dayzctl/"+version.Version+" (+https://github.com/dayzctl/dayzctl)")
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	// Map metadata is a few megabytes; tiles are small.
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (c *Client) mapDir(id string) string { return filepath.Join(c.cacheDir, id) }

func (c *Client) metaPath(id string) string { return filepath.Join(c.mapDir(id), "meta.json") }

func (c *Client) tilePath(meta *Meta, z, x, y int) string {
	return filepath.Join(c.mapDir(meta.ID), "tiles",
		strconv.Itoa(z), strconv.Itoa(x), fmt.Sprintf("%d.%s", y, meta.Format))
}

func (c *Client) readMeta(id string) (*Meta, error) {
	data, err := os.ReadFile(c.metaPath(id))
	if err != nil {
		return nil, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func (c *Client) writeMeta(id string, meta *Meta) error {
	if err := os.MkdirAll(c.mapDir(id), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(c.metaPath(id), data, 0o644)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var validIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func validID(id string) bool { return validIDPattern.MatchString(id) }

func clampMaxZoom(z int) int {
	if z <= 0 || z > maxTileZoom {
		return maxTileZoom
	}
	return z
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// titleCase turns "dashtabad" into "Dashtabad" without touching all-caps names.
func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if s == strings.ToUpper(s) {
		return s
	}
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
