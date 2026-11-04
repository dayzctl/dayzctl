// Package gamemap serves map imagery and place names for the log analyzer.
//
// Map data is generated ahead of time by tools/mapfetch. It is read from a
// directory when one is configured — that is how the .deb ships it — and falls
// back to whatever was embedded at build time, so a package build can carry no
// tiles of its own and still work out of the box.
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

// Tile styles. The satellite imagery is photorealistic; the topographic style
// is a drawn map on which roads and streets stay readable at any zoom.
const (
	StyleSatellite   = "satellite"
	StyleTopographic = "topographic"
)

// validStyle reports whether name is a style this package will look up. The
// list is closed so a request can never steer the tile path anywhere else.
func validStyle(name string) bool {
	switch name {
	case StyleSatellite, StyleTopographic:
		return true
	}
	return false
}

// Meta describes one map.
type Meta struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Size     float64 `json:"size"`
	Format   string  `json:"format"`
	TileSize int     `json:"tileSize"`
	MinZoom  int     `json:"minZoom"`
	MaxZoom  int     `json:"maxZoom"`
	Names    []Place `json:"names"`
	// Styles lists the imagery styles on disk, e.g. ["satellite", "topographic"].
	Styles []string `json:"styles,omitempty"`
}

// Styles lists the imagery styles the map ships, never empty: a map generated
// before styles existed only has satellite imagery.
func (m *Meta) StyleList() []string {
	if len(m.Styles) == 0 {
		return []string{StyleSatellite}
	}
	return m.Styles
}

// DefaultStyle is the style to open the map with. Roads win when they are
// available, because trails and event dots read much better on a drawn map.
func (m *Meta) DefaultStyle() string {
	for _, s := range m.StyleList() {
		if s == StyleTopographic {
			return s
		}
	}
	return m.StyleList()[0]
}

// Store serves maps from a stack of filesystems, each holding one directory per
// map: <map>/meta.json and <map>/<z>/<x>/<y>.<format>. Earlier layers win, so a
// directory on disk overrides the copy compiled into the binary.
type Store struct {
	layers []fs.FS

	mu    sync.Mutex
	meta  map[string]*Meta
	tiles map[string]fs.FS // map id → the layer its metadata came from
}

// New wraps a single filesystem of maps.
func New(fsys fs.FS) *Store { return NewLayered(fsys) }

// NewLayered builds a store that searches the given filesystems in order,
// ignoring nil ones.
func NewLayered(layers ...fs.FS) *Store {
	kept := make([]fs.FS, 0, len(layers))
	for _, l := range layers {
		if l != nil {
			kept = append(kept, l)
		}
	}
	return &Store{
		layers: kept,
		meta:   map[string]*Meta{},
		tiles:  map[string]fs.FS{},
	}
}

// IDs lists every map available, from any layer.
func (s *Store) IDs() []string {
	seen := map[string]bool{}
	var ids []string
	for _, layer := range s.layers {
		entries, err := fs.ReadDir(layer, ".")
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || !validID(name) || seen[name] {
				continue
			}
			if _, err := fs.Stat(layer, path.Join(name, "meta.json")); err != nil {
				continue
			}
			seen[name] = true
			ids = append(ids, name)
		}
	}
	sort.Strings(ids)
	return ids
}

// Has reports whether a map is available in any layer.
func (s *Store) Has(id string) bool {
	_, err := s.Meta(id)
	return err == nil
}

// Meta returns the description of a map, from the first layer that has it.
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

	var (
		meta  Meta
		found fs.FS
	)
	for _, layer := range s.layers {
		data, err := fs.ReadFile(layer, path.Join(id, "meta.json"))
		if err != nil {
			continue
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			return nil, fmt.Errorf("map %q: broken meta.json: %w", id, err)
		}
		found = layer
		break
	}
	if found == nil {
		return nil, fmt.Errorf("map %q is not installed", id)
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
	s.tiles[id] = found
	s.mu.Unlock()
	return &meta, nil
}

// Tile returns one satellite tile and its content type.
func (s *Store) Tile(id string, z, x, y int) ([]byte, string, error) {
	return s.TileStyle(id, StyleSatellite, z, x, y)
}

// TileStyle returns one tile of the given style and its content type. Tiles
// come from the layer that provided the map's metadata.
//
// A style is looked up as <map>/<style>/<z>/<x>/<y>.<format>; satellite imagery
// also falls back to the flat <map>/<z>/<x>/<y>.<format> layout written before
// styles existed, so an older map directory keeps working.
func (s *Store) TileStyle(id, style string, z, x, y int) ([]byte, string, error) {
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

	style = normalise(style)
	if style == "" {
		style = StyleSatellite
	}
	if !validStyle(style) {
		return nil, "", fmt.Errorf("unknown map style %q", style)
	}

	s.mu.Lock()
	layer := s.tiles[meta.ID]
	s.mu.Unlock()
	if layer == nil {
		return nil, "", fmt.Errorf("map %q is not installed", meta.ID)
	}

	file := fmt.Sprintf("%d.%s", y, meta.Format)
	names := []string{path.Join(meta.ID, style, fmt.Sprint(z), fmt.Sprint(x), file)}
	if style == StyleSatellite {
		names = append(names, path.Join(meta.ID, fmt.Sprint(z), fmt.Sprint(x), file))
	}
	for _, name := range names {
		data, err := fs.ReadFile(layer, name)
		if err == nil {
			return data, contentTypeFor(meta.Format), nil
		}
	}
	return nil, "", fmt.Errorf("%s tile %d/%d/%d is not installed", style, z, x, y)
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
