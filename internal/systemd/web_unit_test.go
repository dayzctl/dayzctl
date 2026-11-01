package systemd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func TestRenderWebUnit(t *testing.T) {
	content, err := RenderWebUnit(WebData{
		BinaryPath: "/usr/local/bin/dayzctl",
		WorkingDir: "/srv/dayz",
	})
	if err != nil {
		t.Fatalf("RenderWebUnit: %v", err)
	}

	unit := string(content)

	wantLines := []string{
		"ExecStart=/usr/local/bin/dayzctl web",
		"WorkingDirectory=/srv/dayz",
		"Restart=on-failure",
		"ProtectSystem=full",
		"NoNewPrivileges=yes",
		"ReadOnlyPaths=/etc/dayzctl",
		"WantedBy=multi-user.target",
	}

	for _, want := range wantLines {
		if !strings.Contains(unit, want) {
			t.Errorf("unit is missing %q", want)
		}
	}

	if strings.Contains(unit, "{{") {
		t.Error("template placeholders were not substituted")
	}
	if !strings.HasPrefix(unit, "[Unit]") {
		t.Error("unit should start with the [Unit] section")
	}
}

// TestRenderWebUnitLeavesAddressToTheConfig pins why the unit carries no
// --addr/--port: the web command reads web.address and web.port from the config
// file, so editing the config is enough and the unit never goes stale.
func TestRenderWebUnitLeavesAddressToTheConfig(t *testing.T) {
	content, err := RenderWebUnit(WebData{BinaryPath: "dayzctl"})
	if err != nil {
		t.Fatalf("RenderWebUnit: %v", err)
	}

	unit := string(content)
	for _, unwanted := range []string{"--addr", "--port", "WorkingDirectory="} {
		if strings.Contains(unit, unwanted) {
			t.Errorf("unit should not contain %q:\n%s", unwanted, unit)
		}
	}
	if strings.Contains(unit, "\n\n\n") {
		t.Errorf("rendering left a blank gap where WorkingDirectory would be:\n%s", unit)
	}
}

// TestWebUnitName keeps the constant and the filename the .deb ships in sync.
func TestWebUnitName(t *testing.T) {
	if WebUnitName != "dayz-web.service" {
		t.Errorf("WebUnitName = %q", WebUnitName)
	}
}

// TestWriteIfChanged pins the change detection `dayzctl apply` relies on: an
// identical unit must not be rewritten, so systemd is not reloaded for nothing.
func TestWriteIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testing.service")
	content := []byte("[Unit]\nDescription=test\n")

	changed, err := writeIfChanged(path, content)
	if err != nil {
		t.Fatalf("writeIfChanged: %v", err)
	}
	if !changed {
		t.Error("first write should report a change")
	}

	changed, err = writeIfChanged(path, content)
	if err != nil {
		t.Fatalf("writeIfChanged: %v", err)
	}
	if changed {
		t.Error("rewriting identical content should report no change")
	}

	changed, err = writeIfChanged(path, []byte("[Unit]\nDescription=other\n"))
	if err != nil {
		t.Fatalf("writeIfChanged: %v", err)
	}
	if !changed {
		t.Error("different content should report a change")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "Description=other") {
		t.Errorf("file was not updated: %s", got)
	}
}

// withSystemdDir points unit generation at a temporary directory for one test.
func withSystemdDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := systemdDir
	systemdDir = dir
	t.Cleanup(func() { systemdDir = old })
	return dir
}

func testConfig(web bool) *config.ServerConfig {
	return &config.ServerConfig{
		Paths: config.Paths{InstallDir: "/srv/dayz"},
		Instances: []config.Instance{
			{Name: "main", Port: 2302, Enabled: true},
		},
		Web: config.Web{Enabled: web, Address: "127.0.0.1", Port: 8081},
	}
}

// TestGenerateUnitsIsChangeAware is the contract `dayzctl apply` relies on: a
// second run with the same configuration writes nothing, so systemd does not
// get a needless daemon-reload.
func TestGenerateUnitsIsChangeAware(t *testing.T) {
	dir := withSystemdDir(t)
	sysd := New()

	first, err := sysd.GenerateUnits(testConfig(true))
	if err != nil {
		t.Fatalf("GenerateUnits: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("first run should report the units it wrote")
	}
	for _, want := range []string{"dayz@main.service", WebUnitName} {
		if !slices.Contains(first, want) {
			t.Errorf("first run did not write %s (wrote %v)", want, first)
		}
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s was not created: %v", want, err)
		}
	}

	second, err := sysd.GenerateUnits(testConfig(true))
	if err != nil {
		t.Fatalf("GenerateUnits: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("second run should not change anything, changed %v", second)
	}

	// A changed setting must be detected.
	cfg := testConfig(true)
	cfg.Instances[0].Port = 2402
	third, err := sysd.GenerateUnits(cfg)
	if err != nil {
		t.Fatalf("GenerateUnits: %v", err)
	}
	if !slices.Equal(third, []string{"dayz@main.service"}) {
		t.Errorf("changed instance unit = %v, want [dayz@main.service]", third)
	}
}

// TestGenerateUnitsRemovesDisabledWebUnit keeps a disabled service from failing
// at boot and reports the removal so apply can reload systemd.
func TestGenerateUnitsRemovesDisabledWebUnit(t *testing.T) {
	dir := withSystemdDir(t)
	sysd := New()

	if _, err := sysd.GenerateUnits(testConfig(true)); err != nil {
		t.Fatalf("GenerateUnits: %v", err)
	}
	unitPath := filepath.Join(dir, WebUnitName)
	if _, err := os.Stat(unitPath); err != nil {
		t.Fatalf("%s should exist: %v", WebUnitName, err)
	}

	changed, err := sysd.GenerateUnits(testConfig(false))
	if err != nil {
		t.Fatalf("GenerateUnits: %v", err)
	}
	if !slices.Contains(changed, WebUnitName) {
		t.Errorf("removing the web unit should be reported, got %v", changed)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Errorf("%s should be gone, stat err = %v", WebUnitName, err)
	}
}
