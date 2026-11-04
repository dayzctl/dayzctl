package webapp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The map metadata tells the UI which basemaps exist and how to address them.
func TestMapMetaAdvertisesStyles(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, "GET", "/api/maps/nasdara/meta", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var meta struct {
		Styles       []string `json:"styles"`
		DefaultStyle string   `json:"defaultStyle"`
		TileURL      string   `json:"tileUrl"`
		MaxZoom      int      `json:"maxZoom"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(meta.Styles) == 0 {
		t.Fatal("expected at least one imagery style")
	}
	var roads bool
	for _, s := range meta.Styles {
		if s == "topographic" {
			roads = true
		}
	}
	if !roads {
		t.Errorf("styles = %v, want the road map among them", meta.Styles)
	}
	if meta.DefaultStyle != "topographic" {
		t.Errorf("defaultStyle = %q, want the road map", meta.DefaultStyle)
	}
	if meta.TileURL == "" || !strings.Contains(meta.TileURL, "{style}") {
		t.Errorf("tileUrl = %q, want a {style} placeholder", meta.TileURL)
	}
}

// Both the styled route and the shorthand route must return a real tile.
func TestMapTileRoutes(t *testing.T) {
	srv := newTestServer(t)

	cases := []string{
		"/api/maps/nasdara/tiles/3/4/3",
		"/api/maps/nasdara/tiles/satellite/3/4/3",
		"/api/maps/nasdara/tiles/topographic/3/4/3",
	}
	for _, path := range cases {
		rec := do(t, srv, "GET", path, nil)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200: %s", path, rec.Code, rec.Body.String())
			continue
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty body", path)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "image/webp" {
			t.Errorf("%s: content-type = %q, want image/webp", path, ct)
		}
	}
}

func TestMapTileRejectsUnknownStyle(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, "GET", "/api/maps/nasdara/tiles/blueprint/3/4/3", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
