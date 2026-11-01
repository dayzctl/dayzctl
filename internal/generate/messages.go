package generate

import (
	"bytes"
	_ "embed"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode/utf8"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

//go:embed templates/messages.xml.tmpl
var messagesTmpl string

// maxMessageLength is DayZ's hard limit on the length of a message's text.
// Longer texts are dropped/truncated by the server, so refuse them early.
const maxMessageLength = 160

// countdownOffsets documents the moments at which DayZ sends a countdown
// message: 90, 60, 45, 30, 20, 15, 10, 5, 2 and 1 minutes before the
// deadline. Each <message> with deadline > 0 is broadcast at all of them.
const countdownOffsets = "90/60/45/30/20/15/10/5/2/1"

// messageEntry is a single <message> element of a mission's db/messages.xml.
// Every time value is written in minutes, which is what DayZ expects; the
// zero value switches the corresponding flag off, matching the empty element
// DayZ itself writes (e.g. <repeat>0</repeat> means "no Repeat flag").
type messageEntry struct {
	Delay     int // minutes, only meaningful with OnConnect
	Repeat    int // minutes between broadcasts, 0 = no Repeat flag
	Deadline  int // minutes after server start, 0 = no Countdown flag
	OnConnect int // 0/1
	Shutdown  int // 0/1, requires Deadline > 0
	Text      string
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
			logger.Debug("No shutdown_messages configured, leaving messages.xml untouched", "instance", instance.Name)
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
		return fmt.Errorf("instance %s has no mission 'template' set", instance.Name)
	}

	// Build template data. Keep values in minutes (DayZ's own unit).
	var msgs []messageEntry
	for i, m := range instance.ShutdownMessages {
		if err := validateShutdownMessage(instance.Name, i, m); err != nil {
			return err
		}
		shutdown := 0
		if m.Shutdown {
			shutdown = 1
		}
		msgs = append(msgs, messageEntry{
			Repeat:   m.Repeat,
			Deadline: m.Deadline,
			Shutdown: shutdown,
			// DayZ parses this file as XML: an unescaped '&' or '<' makes the
			// whole document invalid and every message disappears.
			Text: html.EscapeString(m.Text),
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

// validateShutdownMessage rejects configurations DayZ would silently ignore.
// A message with <shutdown>1</shutdown> but no deadline is exactly that: the
// Shutdown flag only matters once the Countdown flag is enabled, so it is
// reported as an error instead of producing a file that never shuts down.
func validateShutdownMessage(instance string, index int, m config.ShutdownMessage) error {
	where := fmt.Sprintf("instance %s: shutdown_messages[%d]", instance, index)

	text := strings.TrimSpace(m.Text)
	if text == "" {
		return fmt.Errorf("%s: text is required", where)
	}
	if n := utf8.RuneCountInString(m.Text); n > maxMessageLength {
		return fmt.Errorf("%s: text is %d characters, DayZ allows at most %d", where, n, maxMessageLength)
	}
	if m.Deadline < 0 {
		return fmt.Errorf("%s: deadline must not be negative (it is a number of minutes after server start)", where)
	}
	if m.Repeat < 0 {
		return fmt.Errorf("%s: repeat must not be negative (it is a number of minutes)", where)
	}
	if m.Shutdown && m.Deadline == 0 {
		return fmt.Errorf("%s: shutdown requires deadline > 0 - DayZ ignores the Shutdown flag unless the message is a countdown", where)
	}
	if m.Deadline > 0 && !strings.Contains(m.Text, "#tmin") {
		logger.Warn("messages.xml: countdown text has no #tmin placeholder; DayZ repeats the same text at "+countdownOffsets+
			" minutes before the deadline", "instance", instance, "index", index)
	}
	return nil
}

// MessagesPath returns the path where messages.xml would be written for
// the given instance, without generating it.
func MessagesPath(cfg *config.ServerConfig, instance *config.Instance) string {
	return filepath.Join(cfg.GetInstallDir(), "mpmissions", instance.Template, "db", "messages.xml")
}
