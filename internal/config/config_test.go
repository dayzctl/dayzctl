package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetInstallDirRelativeAndAbsolute(t *testing.T) {
	cfg := &ServerConfig{}
	cfg.Paths.Base = "/srv/base"
	cfg.Paths.InstallDir = "inst"
	got := cfg.GetInstallDir()
	want := filepath.Join("/srv/base", "inst")
	if got != want {
		t.Fatalf("GetInstallDir relative: got=%q want=%q", got, want)
	}

	cfg.Paths.InstallDir = "/opt/dayz"
	got2 := cfg.GetInstallDir()
	if got2 != "/opt/dayz" {
		t.Fatalf("GetInstallDir absolute: got=%q want=%q", got2, "/opt/dayz")
	}
}

func TestMapsCandidatesOrder(t *testing.T) {
	dirs := mapsCandidates()
	if len(dirs) < 3 {
		t.Fatalf("expected at least three candidates, got %v", dirs)
	}

	// The list always ends with the two system locations, in that order.
	last := dirs[len(dirs)-2:]
	if last[0] != "/usr/local/share/dayzctl/maps" || last[1] != "/usr/share/dayzctl/maps" {
		t.Errorf("system candidates = %v, want /usr/local/share then /usr/share", last)
	}

	// The first entry lives next to the running binary.
	exe, err := os.Executable()
	if err != nil {
		t.Skip("cannot resolve the test binary path")
	}
	if want := filepath.Join(filepath.Dir(exe), "maps"); dirs[0] != want {
		t.Errorf("first candidate = %q, want %q", dirs[0], want)
	}
}

func TestGetMapsDirPrefersExplicitSetting(t *testing.T) {
	dir := t.TempDir()
	cfg := &ServerConfig{}
	cfg.Paths.Base = "/srv/base"
	cfg.Paths.Maps = "custom-maps"
	if err := os.MkdirAll(filepath.Join("/srv/base", "custom-maps"), 0o755); err == nil {
		t.Skip("cannot create /srv/base in this environment")
	}

	// An absolute, existing path is returned as-is.
	cfg.Paths.Maps = dir
	if got := cfg.GetMapsDir(); got != dir {
		t.Errorf("GetMapsDir = %q, want %q", got, dir)
	}
}

func TestGetMapsDirFallsBackToFixedLocations(t *testing.T) {
	cfg := &ServerConfig{}
	cfg.Paths.Maps = filepath.Join(t.TempDir(), "missing")

	got := cfg.GetMapsDir()

	// Nothing may exist on this machine, in which case the answer is empty (the
	// server then uses the maps compiled into the binary).
	if got == "" {
		return
	}
	var found bool
	for _, want := range mapsCandidates() {
		if got == want {
			found = true
		}
	}
	if !found {
		t.Errorf("GetMapsDir = %q, which is neither empty nor a fixed location", got)
	}
}

func TestGetMapsDirHonoursEnvironmentOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(MapsDirEnv, dir)

	cfg := &ServerConfig{}
	if got := cfg.GetMapsDir(); got != dir {
		t.Errorf("GetMapsDir = %q, want the %s override %q", got, MapsDirEnv, dir)
	}
}
