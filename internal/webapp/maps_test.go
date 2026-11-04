package webapp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBundledMapsAreListed(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, "GET", "/api/maps", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Maps []struct {
			ID      string  `json:"id"`
			Name    string  `json:"name"`
			Size    float64 `json:"size"`
			Names   int     `json:"names"`
			MaxZoom int     `json:"maxZoom"`
		} `json:"maps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Maps) == 0 {
		t.Fatal("expected at least one bundled map")
	}

	var found bool
	for _, m := range payload.Maps {
		if m.ID != "chernarusplus" {
			continue
		}
		found = true
		if m.Size <= 0 {
			t.Errorf("size = %v, want the map extent", m.Size)
		}
		if m.MaxZoom <= 0 {
			t.Errorf("maxZoom = %d, want the generated zoom range", m.MaxZoom)
		}
		if m.Names == 0 {
			t.Error("expected place names to be reported")
		}
	}
	if !found {
		t.Error("chernarusplus is missing from the bundled map list")
	}
}

// Tiles are served as immutable, so the tile URL must change when the imagery
// is regenerated — otherwise a browser keeps showing the old tiles.
func TestMapMetaVersionsTileURL(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, "GET", "/api/maps/chernarusplus/meta", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var meta struct {
		Version string `json:"version"`
		TileURL string `json:"tileUrl"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if meta.Version == "" {
		t.Fatal("expected the generated map data to carry a version")
	}
	if !strings.Contains(meta.TileURL, "?v="+meta.Version) {
		t.Errorf("tileUrl = %q, want it to carry ?v=%s", meta.TileURL, meta.Version)
	}
}

func TestMapMetaShortCircuitsWhenNotBundled(t *testing.T) {
	srv := newTestServer(t)

	rec := do(t, srv, "GET", "/api/maps/not-a-real-map/meta", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if body := rec.Body.String(); body == "" {
		t.Error("expected an explanatory error body")
	}
}
