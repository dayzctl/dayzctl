package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func TestGenerateMessagesWritesExpectedXML(t *testing.T) {
	tmp := t.TempDir()
	// The global block is the supported way to configure messages.
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 30, Text: "Restart in 30 minutes"},
			{Deadline: 59, Text: "Restarting now!", Shutdown: true},
		},
	}

	inst := &config.Instance{Name: "myserver", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected messages.xml at %s: %v", path, err)
	}

	content := string(data)
	for _, want := range []string{
		"<messages>",
		"<id>0</id>",
		"<deadline>30</deadline>",
		"<shutdown>0</shutdown>",
		"Restart in 30 minutes",
		"<id>1</id>",
		"<deadline>59</deadline>",
		"<shutdown>1</shutdown>",
		"Restarting now!",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("messages.xml missing expected content %q; got:\n%s", want, content)
		}
	}

	if got := MessagesPath(cfg, inst); got != path {
		t.Errorf("MessagesPath() = %s, want %s", got, path)
	}
}

// TestGenerateMessagesWritesOncePerMission is the point of the top-level
// shutdown_messages block: several instances running the same mission share one
// messages.xml, and each distinct mission gets its own copy.
func TestGenerateMessagesWritesOncePerMission(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 360, Text: "Restart in #tmin minutes", Shutdown: true}},
	}

	instances := []*config.Instance{
		{Name: "chernarus-a", Template: "dayzOffline.chernarusplus"},
		{Name: "chernarus-b", Template: "dayzOffline.chernarusplus"},
		{Name: "livonia", Template: "dayzOffline.enoch"},
	}

	if err := GenerateMessages(cfg, instances); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	paths, err := MessagesPaths(cfg, instances)
	if err != nil {
		t.Fatalf("MessagesPaths error: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 messages.xml targets (one per mission), got %v", paths)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected messages.xml at %s: %v", path, err)
		}
		if !strings.Contains(string(data), "<deadline>360</deadline>") {
			t.Errorf("messages.xml at %s missing the configured message:\n%s", path, data)
		}
	}
}

// TestGenerateMessagesSkipsMissionsWithoutMessages keeps an existing,
// hand-written mission messages.xml untouched when nothing is configured.
func TestGenerateMessagesSkipsMissionsWithoutMessages(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{Name: "myserver", Template: "dayzOffline.chernarusplus"}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	original := `<?xml version="1.0" encoding="UTF-8"?><messages><message><text>mine</text></message></messages>`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed messages.xml: %v", err)
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}
	if string(data) != original {
		t.Fatalf("existing messages.xml was modified:\n%s", data)
	}
}

func TestGenerateMessagesRequiresTemplate(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:             "myserver",
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 60, Text: "hi"}},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err == nil {
		t.Fatal("expected error when template is missing")
	}
}
