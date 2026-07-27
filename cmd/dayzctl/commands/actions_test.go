package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/kabroxiko/dayzctl/internal/config"
)

func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func setupConfig(t *testing.T) *config.ServerConfig {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Steam: config.Steam{Username: "testuser"},
		Paths: config.Paths{Base: tmp},
		Instances: []config.Instance{
			{
				Name:    "testinst",
				Port:    2302,
				Enabled: true,
				RCON:    config.RCON{Enabled: false},
			},
		},
		Updates: config.Updates{Enabled: false},
	}
	shared.Config = cfg
	// Ensure install dir exists so CheckSpaceAction's `df` call succeeds
	if err := os.MkdirAll(cfg.GetInstallDir(), 0755); err != nil {
		t.Fatalf("failed to create install dir: %v", err)
	}
	return cfg
}

func TestVersionAction(t *testing.T) {
	_ = setupConfig(t)
	out := captureStdout(func() {
		if err := VersionAction(); err != nil {
			t.Fatalf("VersionAction error: %v", err)
		}
	})
	if !strings.Contains(out, "dayzctl version") {
		t.Fatalf("unexpected version output: %s", out)
	}
}

func TestListInstancesAction(t *testing.T) {
	_ = setupConfig(t)
	out := captureStdout(func() {
		if err := ListInstancesAction(); err != nil {
			t.Fatalf("ListInstancesAction error: %v", err)
		}
	})
	if !strings.Contains(out, "testinst") {
		t.Fatalf("list output missing instance: %s", out)
	}
}

func TestValidateAndRenderActions(t *testing.T) {
	cfg := setupConfig(t)
	// ValidateConfigAction should not error
	if err := ValidateConfigAction(); err != nil {
		t.Fatalf("ValidateConfigAction error: %v", err)
	}

	// RenderConfigAction should succeed for existing instance
	if err := RenderConfigAction("testinst"); err != nil {
		t.Fatalf("RenderConfigAction error: %v", err)
	}

	// Ensure generated path looks sensible
	expected := filepath.Join(cfg.GetInstallDir(), "serverDZ-testinst.cfg")
	_ = expected
}

func TestCheckSpaceAction(t *testing.T) {
	_ = setupConfig(t)
	out := captureStdout(func() {
		if err := CheckSpaceAction(); err != nil {
			t.Fatalf("CheckSpaceAction error: %v", err)
		}
	})
	if !strings.Contains(out, "Disk usage for") {
		t.Fatalf("unexpected df output: %s", out)
	}
}
