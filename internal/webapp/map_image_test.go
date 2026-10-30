package webapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTestImage drops a tiny file the server can serve as a map background.
func writeTestImage(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// 1x1 PNG.
	png := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
}

func TestMapImageDiscoveredInProfilesFolder(t *testing.T) {
	srv := newTestServer(t)

	// newTestServer puts the profiles folder under a temp install dir.
	dir := srv.Service().ProfilesDir("test")
	writeTestImage(t, filepath.Join(dir, "map.png"))

	rec := do(t, srv, "GET", "/api/instances/test/map-image", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content-type = %q, want image/png", ct)
	}

	// The instance list must advertise the image so the UI can load it.
	list := do(t, srv, "GET", "/api/instances", nil)
	var payload struct {
		Instances []instanceView `json:"instances"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Instances[0].MapImage == "" {
		t.Error("expected mapImage to be advertised for the instance")
	}
}

func TestMapImageMissing(t *testing.T) {
	srv := newTestServer(t)
	rec := do(t, srv, "GET", "/api/instances/test/map-image", nil)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMapImageFromInstallMapsFolder(t *testing.T) {
	srv := newTestServer(t)
	install := srv.cfg.GetInstallDir()
	writeTestImage(t, filepath.Join(install, "maps", "test.png"))

	rec := do(t, srv, "GET", "/api/instances/test/map-image", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestMapImageConfiguredPathWins(t *testing.T) {
	srv := newTestServer(t)
	install := srv.cfg.GetInstallDir()
	custom := filepath.Join(install, "custom-map.png")
	writeTestImage(t, custom)
	// A decoy in the profiles folder must lose to the explicit setting.
	writeTestImage(t, filepath.Join(srv.Service().ProfilesDir("test"), "map.png"))

	srv.cfg.Instances[0].MapImage = "custom-map.png"

	path, ok := srv.mapImagePath(&srv.cfg.Instances[0])
	if !ok || path != custom {
		t.Fatalf("mapImagePath = %q (ok=%v), want %q", path, ok, custom)
	}
}
