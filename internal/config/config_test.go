package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoadReportsOffendingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "steam:\n  username: \"x\"\n\npaths:\n  base: \"/srv/dayz\"\n\ninstances:\n  - name: dayz\n    port: 2302\n   steam_query_port: 27016\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "failed to parse config YAML") {
		t.Errorf("error = %q, want the parse wrapper", msg)
	}
	// The quoted context has to point at the misaligned key.
	if !strings.Contains(msg, "steam_query_port") {
		t.Errorf("error should quote the offending line, got:\n%s", msg)
	}
}

func TestLoadWarnsAboutTabIndentation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "steam:\n\tusername: \"x\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if !strings.Contains(err.Error(), "indented with a tab") {
		t.Errorf("error should mention tabs, got:\n%s", err.Error())
	}
}
