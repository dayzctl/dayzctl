package config

import (
	"bytes"
	"log/slog"
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

func TestPasswordForPrefersInstanceOverride(t *testing.T) {
	cfg := &ServerConfig{Server: Server{Password: "joinpass", PasswordAdmin: "adminpass"}}

	plain := Instance{Name: "plain"}
	if got := cfg.PasswordFor(plain); got != "joinpass" {
		t.Fatalf("PasswordFor fallback: got=%q want=%q", got, "joinpass")
	}
	if got := cfg.PasswordAdminFor(plain); got != "adminpass" {
		t.Fatalf("PasswordAdminFor fallback: got=%q want=%q", got, "adminpass")
	}

	override := Instance{Name: "override", Password: "instjoin", PasswordAdmin: "instadmin"}
	if got := cfg.PasswordFor(override); got != "instjoin" {
		t.Fatalf("PasswordFor override: got=%q want=%q", got, "instjoin")
	}
	if got := cfg.PasswordAdminFor(override); got != "instadmin" {
		t.Fatalf("PasswordAdminFor override: got=%q want=%q", got, "instadmin")
	}
}

func TestLoadReadsPasswordAdminFromServerAndInstance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `steam:
  username: testuser
paths:
  base: ` + dir + `
server:
  password: serverjoin
  password_admin: serveradmin
instances:
  - name: plain
    port: 2302
    enabled: true
  - name: override
    port: 2303
    enabled: true
    password: instjoin
    password_admin: instadmin
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	plain, err := cfg.GetInstanceByName("plain")
	if err != nil {
		t.Fatalf("GetInstanceByName(plain): %v", err)
	}
	if got := cfg.PasswordAdminFor(*plain); got != "serveradmin" {
		t.Fatalf("plain password_admin: got=%q want=%q", got, "serveradmin")
	}
	if got := cfg.PasswordFor(*plain); got != "serverjoin" {
		t.Fatalf("plain password: got=%q want=%q", got, "serverjoin")
	}

	override, err := cfg.GetInstanceByName("override")
	if err != nil {
		t.Fatalf("GetInstanceByName(override): %v", err)
	}
	if got := cfg.PasswordAdminFor(*override); got != "instadmin" {
		t.Fatalf("override password_admin: got=%q want=%q", got, "instadmin")
	}
	if got := cfg.PasswordFor(*override); got != "instjoin" {
		t.Fatalf("override password: got=%q want=%q", got, "instjoin")
	}
}

func TestLoadWarnsAboutUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := `steam:
  username: testuser
paths:
  base: ` + dir + `
server:
  passwordAdmin: camelCaseTypo
instances:
  - name: plain
    port: 2302
    enabled: true
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !strings.Contains(buf.String(), "unknown fields") {
		t.Fatalf("expected a warning about unknown fields, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "passwordAdmin") {
		t.Fatalf("expected the warning to name the offending key, got: %q", buf.String())
	}
}
