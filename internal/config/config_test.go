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

func TestLoadTranslatesLegacyShutdownMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `steam:
  username: "x"
paths:
  base: "/srv/dayz"
instances:
  - name: dayz
    port: 2302
    template: dayzOffline.chernarusplus
shutdown_messages:
  - deadline: 360
    text: "Restart in #tmin minutes"
    shutdown: true
  - repeat: 30
    text: "Join our Discord"
  - on_connect: true
    delay: 2
    text: "Welcome to #name"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if len(cfg.ShutdownMessages) != 0 {
		t.Errorf("legacy list should be cleared after translation, got %+v", cfg.ShutdownMessages)
	}
	if cfg.Messages.Countdown == nil {
		t.Fatal("expected a shutdown message")
	}
	if got := cfg.Messages.Countdown; got.Deadline != 360 || !got.StopServer || got.Text != "Restart in #tmin minutes" {
		t.Errorf("shutdown = %+v", got)
	}
	if len(cfg.Messages.Cyclic) != 1 || cfg.Messages.Cyclic[0].Every != 30 {
		t.Errorf("cyclic = %+v", cfg.Messages.Cyclic)
	}
	if cfg.Messages.Welcome == nil || cfg.Messages.Welcome.Delay != 2 {
		t.Errorf("welcome = %+v", cfg.Messages.Welcome)
	}
}

func TestLoadTranslatesLegacyCyclicOnConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `steam:
  username: "x"
paths:
  base: "/srv/dayz"
instances:
  - name: dayz
    port: 2302
    template: dayzOffline.chernarusplus
shutdown_messages:
  - delay: 10
    repeat: 50
    on_connect: true
    text: "Check our web page at www.dayz.com"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Messages.Welcome != nil {
		t.Errorf("a repeat+on_connect entry belongs to cyclic, got welcome %+v", cfg.Messages.Welcome)
	}
	if len(cfg.Messages.Cyclic) != 1 {
		t.Fatalf("cyclic = %+v", cfg.Messages.Cyclic)
	}
	got := cfg.Messages.Cyclic[0]
	if got.Every != 50 || !got.OnConnect || got.Delay != 10 {
		t.Errorf("cyclic[0] = %+v", got)
	}
}

func TestLoadMigratesMessagesShutdownToCountdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `steam:
  username: "x"
paths:
  base: "/srv/dayz"
instances:
  - name: dayz
    port: 2302
    template: dayzOffline.chernarusplus
messages:
  shutdown:
    deadline: 360
    text: "Restart in #tmin minutes"
    stop_server: true
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Messages.Shutdown != nil {
		t.Errorf("legacy 'shutdown' section should be cleared, got %+v", cfg.Messages.Shutdown)
	}
	if cfg.Messages.Countdown == nil {
		t.Fatal("expected the section to be migrated to 'countdown'")
	}
	if got := cfg.Messages.Countdown; got.Deadline != 360 || !got.StopServer {
		t.Errorf("countdown = %+v", got)
	}
}

func TestLoadKeepsInstanceMessagesOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `steam:
  username: "x"
paths:
  base: "/srv/dayz"
instances:
  - name: dayz
    port: 2302
    template: dayzOffline.chernarusplus
    messages:
      countdown:
        deadline: 120
        text: "Instance restart in #tmin minutes"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if cfg.Messages.Empty() != true {
		t.Errorf("top-level messages should stay empty, got %+v", cfg.Messages)
	}
	inst := cfg.Instances[0]
	if inst.Messages.Countdown == nil || inst.Messages.Countdown.Deadline != 120 {
		t.Errorf("instance messages = %+v", inst.Messages)
	}
}

func TestWebAuthValidation(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	steamID := "76561198000000001"

	cases := []struct {
		name string
		auth WebAuth
		want string
	}{
		{"off", WebAuth{}, ""},
		{"single password", WebAuth{Enabled: true, Username: "admin", Password: "x"}, ""},
		{"single hash", WebAuth{Enabled: true, Username: "admin", PasswordHash: hash}, ""},
		{"users list", WebAuth{Enabled: true, Users: []WebUser{
			{Username: "admin", Password: "x"},
			{Username: "ops", PasswordHash: hash},
		}}, ""},
		{"steam only", WebAuth{Enabled: true, Steam: WebSteam{Enabled: true, AllowedIDs: []string{steamID}}}, ""},
		{"steam with public url", WebAuth{Enabled: true, Steam: WebSteam{
			Enabled: true, PublicURL: "https://dayz.example.com", AllowedIDs: []string{steamID},
		}}, ""},
		{"nothing to sign in with", WebAuth{Enabled: true}, "at least one local user"},
		{"no user", WebAuth{Enabled: true, Password: "x"}, "username is required for each account"},
		{"no secret", WebAuth{Enabled: true, Username: "admin"}, "needs a password or a password_hash"},
		{"both secrets", WebAuth{Enabled: true, Username: "admin", Password: "x", PasswordHash: hash}, "not both"},
		{"bad hash", WebAuth{Enabled: true, Username: "admin", PasswordHash: "abc"}, "64 characters"},
		{"duplicate user", WebAuth{Enabled: true, Users: []WebUser{
			{Username: "admin", Password: "x"},
			{Username: "Admin", Password: "y"},
		}}, "configured twice"},
		{"steam without allowlist", WebAuth{Enabled: true, Steam: WebSteam{Enabled: true}}, "allowed_ids is required"},
		{"steam bad id", WebAuth{Enabled: true, Steam: WebSteam{Enabled: true, AllowedIDs: []string{"12345"}}}, "not a SteamID64"},
		{"steam bad url", WebAuth{Enabled: true, Steam: WebSteam{
			Enabled: true, PublicURL: "dayz.example.com", AllowedIDs: []string{steamID},
		}}, "absolute http(s) URL"},
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

func TestWebAuthMultipleAccounts(t *testing.T) {
	a := WebAuth{Enabled: true, Users: []WebUser{
		{Username: "admin", Password: "one", Name: "Head Admin"},
		{Username: "ops", Password: "two"},
	}}
	// The shorthand account is honoured next to the list.
	a.Username, a.Password = "legacy", "three"

	for _, tc := range []struct{ user, pass, display string }{
		{"admin", "one", "Head Admin"},
		{"ops", "two", "ops"},
		{"legacy", "three", "legacy"},
	} {
		acct, ok := a.Authenticate(tc.user, tc.pass)
		if !ok {
			t.Fatalf("%s/%s rejected", tc.user, tc.pass)
		}
		if got := acct.Display(); got != tc.display {
			t.Errorf("%s display = %q, want %q", tc.user, got, tc.display)
		}
	}
	if _, ok := a.Authenticate("admin", "two"); ok {
		t.Error("password of another account accepted")
	}
	if _, ok := a.Authenticate("nobody", "one"); ok {
		t.Error("unknown user accepted")
	}
}

func TestWebAuthSteamAllowed(t *testing.T) {
	a := WebAuth{Enabled: true, Steam: WebSteam{
		Enabled:    true,
		AllowedIDs: []string{"76561198000000001"},
		Names:      map[string]string{"76561198000000001": "Alice"},
	}}
	if name, ok := a.SteamAllowed("76561198000000001"); !ok || name != "Alice" {
		t.Errorf("allowed id = %q, %v; want Alice, true", name, ok)
	}
	if name, ok := a.SteamAllowed("76561198000000002"); ok {
		t.Errorf("unknown id accepted as %q", name)
	}
	// A Steam block that is configured but not switched on lets nobody in.
	a.Steam.Enabled = false
	if _, ok := a.SteamAllowed("76561198000000001"); ok {
		t.Error("Steam sign-in accepted while steam.enabled is false")
	}
}
