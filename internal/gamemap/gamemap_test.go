package gamemap

import (
	"math"
	"testing"
)

func TestVersionForFindsTheMapInTheSiteBundle(t *testing.T) {
	bundle := `...JSON.parse('{"chernarusplus":{"v":"1.30","i":0},"nasdara":{"v":"1.30","i":11},` +
		`"livonia":{"v":"1.30","i":0}}')...`

	got, err := versionFor(bundle, "nasdara")
	if err != nil {
		t.Fatalf("versionFor: %v", err)
	}
	if got != "1.30-11" {
		t.Errorf("version = %q, want 1.30-11", got)
	}

	if _, err := versionFor(bundle, "takistanplus"); err == nil {
		t.Error("expected an error for a map the site does not publish")
	}
}

func TestToPlaceConvertsToWorldMetres(t *testing.T) {
	// Dashtabad as published by dayz.xam.nu for nasdara (16 km world).
	place, ok := toPlace("city", []float64{-87.3740387, 166.4884949}, []string{"dashtabad"}, 16384, false)
	if !ok {
		t.Fatal("expected a place")
	}
	if place.Name != "Dashtabad" {
		t.Errorf("name = %q, want Dashtabad", place.Name)
	}
	// longitude 166.49 of 256 → 10655 m east, latitude -87.37 → 10792 m north.
	if math.Abs(place.X-10655.3) > 1 {
		t.Errorf("x = %v, want ~10655", place.X)
	}
	if math.Abs(place.Y-10792.1) > 1 {
		t.Errorf("y = %v, want ~10792", place.Y)
	}
}

func TestToPlaceHonoursGPSInvertY(t *testing.T) {
	plain, _ := toPlace("city", []float64{-87.37, 166.49}, []string{"dashtabad"}, 16384, false)
	inverted, _ := toPlace("city", []float64{-87.37, 166.49}, []string{"dashtabad"}, 16384, true)

	if math.Abs(plain.Y+inverted.Y-16384) > 0.2 {
		t.Errorf("inverted y = %v, want %v", inverted.Y, 16384-plain.Y)
	}
	if plain.X != inverted.X {
		t.Errorf("inverting must not move the east/west axis")
	}
}

func TestToPlaceRejectsIncompleteEntries(t *testing.T) {
	cases := []struct {
		name  string
		pos   []float64
		names []string
		size  float64
	}{
		{"no position", nil, []string{"x"}, 16384},
		{"no name", []float64{0, 0}, nil, 16384},
		{"no size", []float64{0, 0}, []string{"x"}, 0},
	}
	for _, tc := range cases {
		if _, ok := toPlace("city", tc.pos, tc.names, tc.size, false); ok {
			t.Errorf("%s: expected the entry to be dropped", tc.name)
		}
	}
}

func TestTileURLForNasdara(t *testing.T) {
	c := New(t.TempDir())
	meta := &Meta{ID: "nasdara", Format: "webp", TileSet: "nasdara", TileSetVer: "26.10.29"}
	got := c.tilePath(meta, 3, 4, 5)

	want := "/nasdara/tiles/3/4/5.webp"
	if len(got) < len(want) || got[len(got)-len(want):] != want {
		t.Errorf("tilePath = %q, want it to end in %q", got, want)
	}
}

func TestClampMaxZoom(t *testing.T) {
	if got := clampMaxZoom(0); got != maxTileZoom {
		t.Errorf("zoom 0 → %d, want %d", got, maxTileZoom)
	}
	if got := clampMaxZoom(99); got != maxTileZoom {
		t.Errorf("zoom 99 → %d, want %d", got, maxTileZoom)
	}
	if got := clampMaxZoom(5); got != 5 {
		t.Errorf("zoom 5 → %d, want 5", got)
	}
}
