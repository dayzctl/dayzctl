package config

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestGetMapsDirFallsBackToFixedLocations(t *testing.T) {
	cfg := &ServerConfig{}

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

func TestWebAuthValidation(t *testing.T) {
	cases := []struct {
		name string
		auth WebAuth
		want string
	}{
		{"off", WebAuth{}, ""},
		{"password", WebAuth{Enabled: true, Username: "admin", Password: "x"}, ""},
		{"hash", WebAuth{Enabled: true, Username: "admin", PasswordHash: strings.Repeat("a", 64)}, ""},
		{"no user", WebAuth{Enabled: true, Password: "x"}, "web.auth.username is required"},
		{"no secret", WebAuth{Enabled: true, Username: "admin"}, "password or a password_hash"},
		{"both secrets", WebAuth{Enabled: true, Username: "admin", Password: "x", PasswordHash: strings.Repeat("a", 64)}, "not both"},
		{"bad hash", WebAuth{Enabled: true, Username: "admin", PasswordHash: "abc"}, "64 characters"},
	}

	for _, tc := range cases {
		err := validateWebAuth(tc.auth)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: unexpected error %v", tc.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to mention %q", tc.name, err, tc.want)
		}
	}
}

func TestWebAuthCheckCredentials(t *testing.T) {
	a := WebAuth{Enabled: true, Username: "admin", Password: "s3cret"}
	if !a.CheckCredentials("admin", "s3cret") {
		t.Error("good credentials rejected")
	}
	if a.CheckCredentials("admin", "nope") || a.CheckCredentials("root", "s3cret") {
		t.Error("bad credentials accepted")
	}

	sum := sha256.Sum256([]byte("hunter2"))
	h := WebAuth{Enabled: true, Username: "ops", PasswordHash: "sha256:" + hex.EncodeToString(sum[:])}
	if !h.CheckCredentials("ops", "hunter2") {
		t.Error("hash credentials rejected")
	}
	if h.CheckCredentials("ops", "hunter3") {
		t.Error("wrong password accepted against a hash")
	}

	// A disabled switch lets everything through, which is what the middleware
	// relies on when auth is off.
	var off WebAuth
	if !off.CheckCredentials("", "") {
		t.Error("disabled auth should not reject")
	}
}
