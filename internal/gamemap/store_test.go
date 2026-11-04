package gamemap

import (
	"testing"
	"testing/fstest"
)

// testFS mirrors the layout tools/mapfetch writes:
// <map>/<style>/<z>/<x>/<y>.<format>, plus the flat layout used before styles
// existed.
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"nasdara/meta.json": &fstest.MapFile{Data: []byte(`{
			"id":"nasdara","name":"Nasdara","size":16384,"format":"webp",
			"tileSize":256,"minZoom":3,"maxZoom":4,
			"styles":["satellite","topographic"],
			"names":[{"name":"Dashtabad","x":10655.3,"y":10792.1,"kind":"city"}]
		}`)},
		"nasdara/satellite/3/3/3.webp":   &fstest.MapFile{Data: []byte("tile-3-3-3")},
		"nasdara/satellite/4/15/15.webp": &fstest.MapFile{Data: []byte("tile-4-15-15")},
		"nasdara/topographic/3/3/3.webp": &fstest.MapFile{Data: []byte("roads-3-3-3")},
	}
}

func TestStoreReadsBundledMeta(t *testing.T) {
	s := New(testFS())

	meta, err := s.Meta("nasdara")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Size != 16384 {
		t.Errorf("size = %v, want 16384", meta.Size)
	}
	if meta.MaxZoom != 4 || meta.MinZoom != 3 {
		t.Errorf("zoom range = %d-%d, want 3-4", meta.MinZoom, meta.MaxZoom)
	}
	if len(meta.Names) != 1 || meta.Names[0].Name != "Dashtabad" {
		t.Errorf("names = %+v, want one Dashtabad entry", meta.Names)
	}
}

func TestStoreServesTiles(t *testing.T) {
	s := New(testFS())

	data, contentType, err := s.Tile("Nasdara", 3, 3, 3)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if string(data) != "tile-3-3-3" {
		t.Errorf("data = %q", data)
	}
	if contentType != "image/webp" {
		t.Errorf("content type = %q, want image/webp", contentType)
	}
}

func TestStoreServesTopographicTiles(t *testing.T) {
	s := New(testFS())

	data, _, err := s.TileStyle("nasdara", StyleTopographic, 3, 3, 3)
	if err != nil {
		t.Fatalf("TileStyle: %v", err)
	}
	if string(data) != "roads-3-3-3" {
		t.Errorf("data = %q, want the road-map tile", data)
	}

	// A style the map does not ship is a 404, not a wrong image.
	if _, _, err := s.TileStyle("nasdara", StyleTopographic, 4, 15, 15); err == nil {
		t.Error("expected an error for a tile the road map does not have")
	}
	// An unknown style never reaches the filesystem.
	if _, _, err := s.TileStyle("nasdara", "../../etc", 3, 3, 3); err == nil {
		t.Error("expected an error for an unknown style")
	}
	// The default style for a map that has a road map is the road map.
	if got := mustMeta(t, s, "nasdara").DefaultStyle(); got != StyleTopographic {
		t.Errorf("default style = %q, want %q", got, StyleTopographic)
	}
}

// TestStoreReadsLegacyFlatTileLayout keeps older map directories, generated
// before styles existed, working.
func TestStoreReadsLegacyFlatTileLayout(t *testing.T) {
	legacy := fstest.MapFS{
		"livonia/meta.json":  &fstest.MapFile{Data: []byte(`{"id":"livonia","size":12800,"maxZoom":3}`)},
		"livonia/3/1/2.webp": &fstest.MapFile{Data: []byte("legacy-tile")},
	}
	s := New(legacy)

	data, _, err := s.Tile("livonia", 3, 1, 2)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if string(data) != "legacy-tile" {
		t.Errorf("data = %q, want the legacy tile", data)
	}
	// With no styles in the metadata the map is satellite-only.
	if got := mustMeta(t, s, "livonia").StyleList(); len(got) != 1 || got[0] != StyleSatellite {
		t.Errorf("styles = %v, want [satellite]", got)
	}
}

func mustMeta(t *testing.T, s *Store, id string) *Meta {
	t.Helper()
	meta, err := s.Meta(id)
	if err != nil {
		t.Fatalf("Meta(%q): %v", id, err)
	}
	return meta
}

func TestStoreRejectsOutOfRangeTiles(t *testing.T) {
	s := New(testFS())

	cases := []struct{ z, x, y int }{
		{5, 0, 0},  // beyond maxZoom
		{3, 8, 0},  // x beyond 2^z
		{3, 0, -1}, // negative
	}
	for _, c := range cases {
		if _, _, err := s.Tile("nasdara", c.z, c.x, c.y); err == nil {
			t.Errorf("tile %d/%d/%d: expected an error", c.z, c.x, c.y)
		}
	}

	// Inside the pyramid but not bundled (we only ship zoom 3 and 4).
	if _, _, err := s.Tile("nasdara", 4, 0, 0); err == nil {
		t.Error("expected an error for a tile that was not generated")
	}
}

func TestStoreUnknownMap(t *testing.T) {
	s := New(testFS())

	if _, err := s.Meta("takistanplus"); err == nil {
		t.Error("expected an error for a map that is not bundled")
	}
	if s.Has("takistanplus") {
		t.Error("Has must be false for a map that is not bundled")
	}
	if !s.Has("nasdara") {
		t.Error("Has must be true for a bundled map")
	}
	if got := s.IDs(); len(got) != 1 || got[0] != "nasdara" {
		t.Errorf("IDs = %v, want [nasdara]", got)
	}
}

func TestStoreToleratesMissingTree(t *testing.T) {
	// A tree built without generated maps still has to work.
	s := New(fstest.MapFS{})

	if s.Has("nasdara") {
		t.Error("an empty store must not report maps")
	}
	if _, err := s.Meta("nasdara"); err == nil {
		t.Error("expected an error from an empty store")
	}
	if got := s.IDs(); len(got) != 0 {
		t.Errorf("IDs = %v, want none", got)
	}
}

func TestStoreRejectsTraversal(t *testing.T) {
	s := New(testFS())

	for _, id := range []string{"../nasdara", "nasdara/../..", "Nas dara", ""} {
		if _, err := s.Meta(id); err == nil {
			t.Errorf("Meta(%q): expected an error", id)
		}
	}
}

// TestLayeredDiskOverridesEmbedded covers the packaging arrangement: the .deb
// ships maps on disk and the binary carries a fallback copy.
func TestLayeredDiskOverridesEmbedded(t *testing.T) {
	disk := fstest.MapFS{
		"nasdara/meta.json":       &fstest.MapFile{Data: []byte(`{"id":"nasdara","size":4096,"format":"jpg","maxZoom":3}`)},
		"nasdara/3/0/0.jpg":       &fstest.MapFile{Data: []byte("disk-tile")},
		"chernarusplus/meta.json": &fstest.MapFile{Data: []byte(`{"id":"chernarusplus","size":15360,"format":"webp","maxZoom":3}`)},
	}

	s := NewLayered(disk, testFS())

	// The on-disk copy wins.
	meta, err := s.Meta("nasdara")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Size != 4096 || meta.Format != "jpg" {
		t.Errorf("meta = %+v, want the disk copy", meta)
	}
	data, contentType, err := s.Tile("nasdara", 3, 0, 0)
	if err != nil {
		t.Fatalf("Tile: %v", err)
	}
	if string(data) != "disk-tile" || contentType != "image/jpeg" {
		t.Errorf("tile = %q (%s), want the disk copy", data, contentType)
	}

	// Maps only present in the embedded layer are still served.
	if _, err := s.Meta("chernarusplus"); err != nil {
		t.Errorf("chernarusplus should come from disk: %v", err)
	}
	if !s.Has("chernarusplus") {
		t.Error("Has should report a map that exists on disk")
	}

	// Union of both layers, deduplicated and sorted.
	ids := s.IDs()
	if len(ids) != 2 || ids[0] != "chernarusplus" || ids[1] != "nasdara" {
		t.Errorf("IDs = %v, want [chernarusplus nasdara]", ids)
	}
}

func TestLayeredEmptyDiskFallsBack(t *testing.T) {
	s := NewLayered(fstest.MapFS{}, testFS())

	meta, err := s.Meta("nasdara")
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if meta.Size != 16384 {
		t.Errorf("size = %v, want the embedded copy", meta.Size)
	}
	if _, _, err := s.Tile("nasdara", 3, 3, 3); err != nil {
		t.Errorf("Tile: %v", err)
	}
}
