package webapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMapImageRemoteURLIsProxied(t *testing.T) {
	// A pretend map host; the service proxies it so browsers only ever talk to
	// dayzctl itself.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer origin.Close()

	srv := newTestServer(t)
	srv.cfg.Instances[0].MapImage = origin.URL + "/nasdara.jpg"

	rec := do(t, srv, "GET", "/api/instances/test/map-image", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("content-type = %q, want image/jpeg", ct)
	}
	if rec.Body.String() != "jpeg-bytes" {
		t.Errorf("body = %q, want the proxied image", rec.Body.String())
	}
}

func TestMapImageRemoteURLIsAdvertised(t *testing.T) {
	srv := newTestServer(t)
	srv.cfg.Instances[0].MapImage = "https://example.invalid/nasdara.png"

	if !srv.mapImageAvailable(&srv.cfg.Instances[0]) {
		t.Fatal("a remote map_image should count as available without touching disk")
	}
}

func TestMapImageRemoteFailureIsReported(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer origin.Close()

	srv := newTestServer(t)
	srv.cfg.Instances[0].MapImage = origin.URL + "/missing.jpg"

	rec := do(t, srv, "GET", "/api/instances/test/map-image", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}
