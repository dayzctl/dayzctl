package generate

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

//go:embed templates/messages.xml.tmpl
var messagesTmpl string

// messagesFile is the root <messages> element of a mission's db/messages.xml.
// messageEntry is a single <message> entry used to render the template.
// Note: config values for Deadline and Repeat are expressed in minutes; the
// generator converts them to seconds in the output that DayZ expects.
type messageEntry struct {
	ID          int
	Deadline    int // seconds
	Repeat      int // seconds
	PlayerCount int
	Shutdown    int
	Text        string
}

// GenerateMessages writes db/messages.xml for each given instance that has
// shutdown_messages configured, under its mission directory
// (<installDir>/mpmissions/<template>/db/messages.xml). Instances with no
// shutdown_messages configured are skipped so any existing messages.xml in
// the mission is left untouched.
func GenerateMessages(cfg *config.ServerConfig, instances []*config.Instance) error {
	installDir := cfg.GetInstallDir()
	for _, instance := range instances {
		if err := generateInstanceMessages(installDir, instance); err != nil {
			return fmt.Errorf("failed to generate messages.xml for %s: %w", instance.Name, err)
		}
	}
	return nil
}

// generateInstanceMessages writes messages.xml for a single instance.
func generateInstanceMessages(installDir string, instance *config.Instance) error {
	if instance.Template == "" {
		return fmt.Errorf("instance %s has no mission 'template' set", instance.Name)
	}

	// Build template data. Convert minutes -> seconds.
	var msgs []messageEntry
	for i, m := range instance.ShutdownMessages {
		shutdown := 0
		if m.Shutdown {
			shutdown = 1
		}
		msgs = append(msgs, messageEntry{
			ID:          i,
			Deadline:    m.Deadline * 60,
			Repeat:      m.Repeat * 60,
			PlayerCount: m.PlayerCount,
			Shutdown:    shutdown,
			Text:        m.Text,
		})
	}

	dbDir := filepath.Join(installDir, "mpmissions", instance.Template, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create mission db directory: %w", err)
	}

	path := filepath.Join(dbDir, "messages.xml")

	// Parse and execute embedded template.
	tmpl, err := template.New("messages").Parse(messagesTmpl)
	if err != nil {
		return fmt.Errorf("failed to parse messages template: %w", err)
	}

	var buf bytes.Buffer
	data := struct{ Messages []messageEntry }{Messages: msgs}
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute messages template: %w", err)
	}

	content := buf.Bytes()
	// Ensure file uses Unix LF line endings.
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("failed to write messages.xml: %w", err)
	}
	logger.Info("Wrote messages.xml", "path", path, "instance", instance.Name)
	return nil
}

// MessagesPath returns the path where messages.xml would be written for
// the given instance, without generating it.
func MessagesPath(cfg *config.ServerConfig, instance *config.Instance) string {
	return filepath.Join(cfg.GetInstallDir(), "mpmissions", instance.Template, "db", "messages.xml")
}
