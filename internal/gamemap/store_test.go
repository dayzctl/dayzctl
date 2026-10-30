package gamemap

import (
	"testing"
	"testing/fstest"
)

// testFS mirrors the layout tools/mapfetch writes:
// <map>/meta.json and <map>/<z>/<x>/<y>.<format>.
func testFS() fstest.MapFS {
	return fstest.MapFS{
		"nasdara/meta.json": &fstest.MapFile{Data: []byte(`{
			"id":"nasdara","name":"Nasdara","size":16384,"format":"webp",
			"tileSize":256,"minZoom":3,"maxZoom":4,"source":"dayz.xam.nu",
			"version":"1.30-11","attribution":"Map imagery © dayz.xam.nu",
			"names":[{"name":"Dashtabad","x":10655.3,"y":10792.1,"kind":"city"}]
		}`)},
		"nasdara/3/3/3.webp":   &fstest.MapFile{Data: []byte("tile-3-3-3")},
		"nasdara/4/15/15.webp": &fstest.MapFile{Data: []byte("tile-4-15-15")},
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
