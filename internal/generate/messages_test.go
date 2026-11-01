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
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "myserver",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 30, Text: "Restart in #tmin minutes"},
			{Deadline: 59, Text: "Restarting now!", Shutdown: true},
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
	for _, want := range []string{
		"<messages>",
		"<delay>0</delay>",
		"<repeat>0</repeat>",
		"<deadline>30</deadline>",
		"<onConnect>0</onConnect>",
		"<shutdown>0</shutdown>",
		"Restart in #tmin minutes",
		"<deadline>59</deadline>",
		"<shutdown>1</shutdown>",
		"Restarting now!",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("messages.xml missing expected content %q; got:\n%s", want, content)
		}
	}

	// Elements DayZ does not understand must not be written: the file has to
	// match the documented format (delay, repeat, deadline, onConnect,
	// shutdown, text) or the server ignores the messages.
	for _, unwanted := range []string{"<id>", "<playerCount>", "<onconnect>"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("messages.xml contains unsupported element %q; got:\n%s", unwanted, content)
		}
	}

	if got := MessagesPath(cfg, inst); got != path {
		t.Errorf("MessagesPath() = %s, want %s", got, path)
	}
}

// TestGenerateMessagesSkipsInstancesWithoutMessages ensures an existing,
// hand-written mission messages.xml is not clobbered when an instance has no
// shutdown_messages configured.
func TestGenerateMessagesSkipsInstancesWithoutMessages(t *testing.T) {
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
