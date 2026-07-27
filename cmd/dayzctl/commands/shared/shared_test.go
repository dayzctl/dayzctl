package shared

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kabroxiko/dayzctl/internal/config"
)

func TestGetInstanceErrors(t *testing.T) {
	// No config loaded
	Config = nil
	if _, err := GetInstance("any"); err == nil {
		t.Fatalf("expected error when config not loaded")
	}

	// Config loaded but instance missing
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}
	Config = cfg
	if _, err := GetInstance("missing"); err == nil {
		t.Fatalf("expected error for missing instance")
	}
}

func TestSaveConfigWrites(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Steam: config.Steam{Username: "tst"},
		Paths: config.Paths{Base: tmp},
		Instances: []config.Instance{
			{Name: "one", Port: 2302, Enabled: true},
		},
	}
	Config = cfg

	// Ensure config directory exists
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if fi, err := os.Stat(cfgDir); err != nil || !fi.IsDir() {
		t.Fatalf("config dir missing after mkdir: %v %v", fi, err)
	}
	if err := SaveConfig(); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	outPath := filepath.Join(tmp, "config", "config.yaml")
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		t.Fatalf("expected config file at %s", outPath)
	}
}

func TestUpdateServerConfigInsertMod(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Steam:     config.Steam{Username: "tst"},
		Paths:     config.Paths{Base: tmp},
		Instances: []config.Instance{{Name: "testinst", Port: 2302, Enabled: true, Mods: []config.ModRef{{ID: "@mod1"}}}},
	}
	Config = cfg

	// Prepare server config file without mod line
	installDir := cfg.GetInstallDir()
	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(installDir, "serverDZ-testinst.cfg")
	content := "server={\n  setting=1\n}\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	inst := &Config.Instances[0]
	if err := UpdateServerConfig(inst); err != nil {
		t.Fatalf("UpdateServerConfig failed: %v", err)
	}

	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(out) == content {
		t.Fatalf("expected mod param inserted, file unchanged")
	}
}
