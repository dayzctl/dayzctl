package generate

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

// messagesFile is the root <messages> element of a mission's db/messages.xml.
type messagesFile struct {
	XMLName  xml.Name       `xml:"messages"`
	Messages []messageEntry `xml:"message"`
}

// messageEntry is a single <message> entry. Deadline is seconds after
// server start; Shutdown is 1/0 to trigger a graceful shutdown once the
// deadline is reached; PlayerCount is the minimum online players required
// to show the message; Repeat is the repeat interval in seconds (0 = once).
type messageEntry struct {
	ID          int    `xml:"id"`
	Deadline    int    `xml:"deadline"`
	Repeat      int    `xml:"repeat"`
	PlayerCount int    `xml:"playerCount"`
	Shutdown    int    `xml:"shutdown"`
	Text        string `xml:"text"`
}

// GenerateMessages writes db/messages.xml for each given instance that has
// shutdown_messages configured, under its mission directory
// (<installDir>/mpmissions/<template>/db/messages.xml). Instances with no
// shutdown_messages configured are skipped so any existing messages.xml in
// the mission is left untouched.
func GenerateMessages(cfg *config.ServerConfig, instances []*config.Instance) error {
	installDir := cfg.GetInstallDir()
	for _, instance := range instances {
		if len(instance.ShutdownMessages) == 0 {
			continue
		}
		if err := generateInstanceMessages(installDir, instance); err != nil {
			return fmt.Errorf("failed to generate messages.xml for %s: %w", instance.Name, err)
		}
	}
	return nil
}

// generateInstanceMessages writes messages.xml for a single instance.
func generateInstanceMessages(installDir string, instance *config.Instance) error {
	if instance.Template == "" {
		return fmt.Errorf("instance %s has shutdown_messages configured but no mission 'template' is set", instance.Name)
	}

	doc := messagesFile{}
	for i, m := range instance.ShutdownMessages {
		shutdown := 0
		if m.Shutdown {
			shutdown = 1
		}
		doc.Messages = append(doc.Messages, messageEntry{
			ID:          i,
			Deadline:    m.Deadline,
			Repeat:      m.Repeat,
			PlayerCount: m.PlayerCount,
			Shutdown:    shutdown,
			Text:        m.Text,
		})
	}

	out, err := xml.MarshalIndent(doc, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal messages.xml: %w", err)
	}

	dbDir := filepath.Join(installDir, "mpmissions", instance.Template, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create mission db directory: %w", err)
	}

	path := filepath.Join(dbDir, "messages.xml")
	content := append([]byte(xml.Header), out...)
	content = append(content, '\n')

	// Ensure file uses Unix LF line endings. Some tools or editors may
	// introduce CRLF; normalize to LF to avoid issues on Linux where
	// DayZ expects Unix-style files.
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
