// Package gamemap serves map imagery and place names for the log analyzer.
//
// The data is generated ahead of time by tools/mapfetch and committed under
// web/log-analyzer/maps/<map>/, then embedded into the binary. Nothing is
// downloaded while the server runs, so the map works offline and the service
// never depends on a third-party host being reachable.
package gamemap

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
)

// Place is a named location on the map, in DayZ world metres.
type Place struct {
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Kind string  `json:"kind,omitempty"`
}

// Meta describes one bundled map.
type Meta struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Size        float64 `json:"size"`
	Format      string  `json:"format"`
	TileSize    int     `json:"tileSize"`
	MinZoom     int     `json:"minZoom"`
	MaxZoom     int     `json:"maxZoom"`
	Source      string  `json:"source"`
	Version     string  `json:"version"`
	Attribution string  `json:"attribution"`
	GeneratedAt string  `json:"generatedAt"`
	Names       []Place `json:"names"`
}

// Store serves the bundled maps from an fs.FS whose root holds one directory
// per map: <map>/meta.json and <map>/<z>/<x>/<y>.<format>.
type Store struct {
	fsys fs.FS

	mu   sync.Mutex
	meta map[string]*Meta // parsed meta.json per map
}

// New wraps a filesystem of bundled maps.
func New(fsys fs.FS) *Store {
	return &Store{fsys: fsys, meta: map[string]*Meta{}}
}

// ids lists the maps present in the store.
func (s *Store) IDs() []string {
	if s.fsys == nil {
		return nil
	}
	entries, err := fs.ReadDir(s.fsys, ".")
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && validID(e.Name()) {
			if _, err := fs.Stat(s.fsys, path.Join(e.Name(), "meta.json")); err == nil {
				ids = append(ids, e.Name())
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// Has reports whether a map is bundled.
func (s *Store) Has(id string) bool {
	_, err := s.Meta(id)
	return err == nil
}

// Meta returns the description of a bundled map.
func (s *Store) Meta(id string) (*Meta, error) {
	id = normalise(id)
	if !validID(id) {
		return nil, fmt.Errorf("invalid map name %q", id)
	}

	s.mu.Lock()
	if m, ok := s.meta[id]; ok {
		s.mu.Unlock()
		return m, nil
	}
	s.mu.Unlock()

	data, err := fs.ReadFile(s.fsys, path.Join(id, "meta.json"))
	if err != nil {
		return nil, fmt.Errorf("map %q is not bundled with dayzctl", id)
	}

	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("map %q: broken meta.json: %w", id, err)
	}
	if meta.ID == "" {
		meta.ID = id
	}
	if meta.TileSize == 0 {
		meta.TileSize = 256
	}
	if meta.Format == "" {
		meta.Format = "webp"
	}

	s.mu.Lock()
	s.meta[id] = &meta
	s.mu.Unlock()
	return &meta, nil
}

// Tile returns one tile image and its content type.
func (s *Store) Tile(id string, z, x, y int) ([]byte, string, error) {
	meta, err := s.Meta(id)
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

	name := path.Join(meta.ID, fmt.Sprint(z), fmt.Sprint(x), fmt.Sprintf("%d.%s", y, meta.Format))
	data, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		return nil, "", fmt.Errorf("tile %d/%d/%d is not bundled", z, x, y)
	}
	return data, contentTypeFor(meta.Format), nil
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

func normalise(id string) string { return strings.ToLower(strings.TrimSpace(id)) }

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case (r == '-' || r == '_') && i > 0:
		default:
			return false
		}
	}
	return true
}
