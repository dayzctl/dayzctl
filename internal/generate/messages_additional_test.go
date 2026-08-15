package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func TestMinutesAreConvertedToSeconds(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "conv",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 2, Text: "in 2 minutes", Shutdown: false, Repeat: 15},
		},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected messages.xml at %s: %v", path, err)
	}
	content := string(data)
	if !strings.Contains(content, "<deadline>120</deadline>") {
		t.Fatalf("expected deadline 120 seconds, got:\n%s", content)
	}
	if !strings.Contains(content, "<repeat>900</repeat>") {
		t.Fatalf("expected repeat 900 seconds, got:\n%s", content)
	}
}

func TestOutputUsesLFOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:             "eol",
		Template:         "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 1, Text: "eol test"}},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected messages.xml at %s: %v", path, err)
	}
	if strings.Contains(string(data), "\r") {
		t.Fatalf("expected LF-only file, found CR bytes in %s", path)
	}
}
