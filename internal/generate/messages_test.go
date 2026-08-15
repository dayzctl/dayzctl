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
			{Deadline: 30, Text: "Restart in 30 minutes"},
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
		"<id>0</id>",
		"<deadline>1800</deadline>",
		"<shutdown>0</shutdown>",
		"Restart in 30 minutes",
		"<id>1</id>",
		"<deadline>3540</deadline>",
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

func TestGenerateMessagesSkipsInstancesWithoutMessages(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

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
	if !strings.Contains(content, "<messages>") {
		t.Fatalf("expected messages.xml to contain <messages>, got:\n%s", content)
	}
	// Ensure there are no real <message> elements outside the commented examples.
	// Find end of comment block and ensure no <message> appears after it.
	endComment := strings.Index(content, "-->")
	if endComment == -1 {
		t.Fatalf("expected template to include commented examples, got:\n%s", content)
	}
	after := content[endComment+3:]
	if strings.Contains(after, "<message>") {
		t.Fatalf("expected no <message> entries after comment block for empty shutdown_messages, got:\n%s", after)
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
