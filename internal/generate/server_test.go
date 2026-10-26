package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

// TestGenerateServerConfigAdminLogging verifies the admin log flags are
// emitted as enabled (1) in the generated serverDZ.cfg by default.
func TestGenerateServerConfigAdminLogging(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Steam: config.Steam{Username: "tester"},
		Paths: config.Paths{Base: tmp},
		Instances: []config.Instance{
			{
				Name:       "inst",
				InstanceID: 1,
				Port:       2302,
				Enabled:    true,
				Hostname:   "Test",
				MaxPlayers: 60,
				Template:   "dayzOffline.chernarusplus",
			},
		},
	}
	cfg.SetDefaults()

	tmplContent, err := os.ReadFile(filepath.Join("templates", "serverDZ.cfg.tmpl"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}

	if err := os.MkdirAll(cfg.GetInstallDir(), 0755); err != nil {
		t.Fatalf("mkdir install dir: %v", err)
	}
	if err := GenerateServerConfig(cfg, string(tmplContent)); err != nil {
		t.Fatalf("GenerateServerConfig: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(cfg.GetInstallDir(), "serverDZ-inst.cfg"))
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	for _, want := range []string{"adminLogPlacement = 1;", "adminLogBuildActions = 1;"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("generated serverDZ.cfg missing %q:\n%s", want, out)
		}
	}
}
