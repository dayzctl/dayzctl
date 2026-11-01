package webapp

import (
	"encoding/json"
	"net/http"
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
		if m.ID != "nasdara" {
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
		t.Error("nasdara is missing from the bundled map list")
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
