package generate

import (
	"bytes"
	_ "embed"
	"fmt"
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
// DayZ only understands the lowercase elements delay, repeat, deadline,
// onconnect, shutdown and text; they are always written, in that order, like
// the working production files (e.g. SumrakDZN/Namalsk-Server) and the
// official wiki example. Every time value is in minutes.
type messageEntry struct {
	Delay     int // minutes, only meaningful with OnConnect
	Repeat    int // minutes between broadcasts, 0 = no repeat
	Deadline  int // minutes after server start, 0 = no countdown
	OnConnect int // 0/1
	Shutdown  int // 0/1, requires Deadline > 0
	Text      string
}

// resolvedMission is one mission (map) folder that receives a messages.xml,
// together with the message list that belongs to it.
type resolvedMission struct {
	Template  string
	Instances []string
	Messages  []config.ShutdownMessage
}

// GenerateMessages writes db/messages.xml once for every mission (map) used
// by the given instances. DayZ reads the file from the mission folder
// (<installDir>/mpmissions/<template>/db/messages.xml), so it is shared by all
// instances running that mission - writing it per instance would just
// overwrite the same path repeatedly. The top-level shutdown_messages are used
// for every mission; a (deprecated) instance-level block overrides them for
// its own mission. Missions with nothing configured are left untouched.
func GenerateMessages(cfg *config.ServerConfig, instances []*config.Instance) error {
	missions, err := resolveMissions(cfg, instances)
	if err != nil {
		return err
	}
	installDir := cfg.GetInstallDir()
	for _, mission := range missions {
		if err := writeMessages(installDir, mission); err != nil {
			return fmt.Errorf("failed to generate messages.xml for mission %s: %w", mission.Template, err)
		}
	}
	return nil
}

// resolveMissions groups instances by mission template and picks the message
// list for each of them.
func resolveMissions(cfg *config.ServerConfig, instances []*config.Instance) ([]resolvedMission, error) {
	byTemplate := make(map[string]*resolvedMission)
	var order []string

	for _, instance := range instances {
		override := instance.ShutdownMessages
		if instance.Template == "" {
			if len(override) > 0 {
				return nil, fmt.Errorf("instance %s has no mission 'template' set", instance.Name)
			}
			if len(cfg.ShutdownMessages) > 0 {
				logger.Warn("Instance has no mission 'template', skipping its messages.xml", "instance", instance.Name)
			}
			continue
		}

		mission := byTemplate[instance.Template]
		if mission == nil {
			mission = &resolvedMission{Template: instance.Template}
			byTemplate[instance.Template] = mission
			order = append(order, instance.Template)
		}

		if len(override) > 0 {
			if mission.Messages != nil && !sameMessages(mission.Messages, override) {
				return nil, fmt.Errorf(
					"instances %v share mission %s but define different shutdown_messages; messages.xml is per mission, so move the block to the top-level 'shutdown_messages'",
					append(mission.Instances, instance.Name), instance.Template)
			}
			mission.Messages = override
			logger.Warn("Instance-level shutdown_messages are deprecated: messages.xml is stored per mission (map), not per instance; move the block to the top-level 'shutdown_messages'",
				"instance", instance.Name, "mission", instance.Template)
		}
		mission.Instances = append(mission.Instances, instance.Name)
	}

	resolved := make([]resolvedMission, 0, len(order))
	for _, template := range order {
		mission := byTemplate[template]
		if mission.Messages == nil {
			mission.Messages = cfg.ShutdownMessages
		} else if len(cfg.ShutdownMessages) > 0 {
			logger.Warn("Top-level shutdown_messages ignored for this mission because an instance overrides them",
				"mission", template, "instances", mission.Instances)
		}
		if len(mission.Messages) == 0 {
			logger.Debug("No shutdown_messages configured, leaving messages.xml untouched", "mission", template)
			continue
		}
		resolved = append(resolved, *mission)
	}
	return resolved, nil
}

// sameMessages reports whether two message lists are identical.
func sameMessages(a, b []config.ShutdownMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeMessages renders and writes the mission's db/messages.xml.
func writeMessages(installDir string, mission resolvedMission) error {
	msgs, err := buildMessageEntries(mission.Template, mission.Messages)
	if err != nil {
		return err
	}

	dbDir := filepath.Join(installDir, "mpmissions", mission.Template, "db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return fmt.Errorf("failed to create mission db directory: %w", err)
	}

	warnIfMissionMissing(installDir, mission.Template)

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

	changed, err := writeFileIfChanged(path, content)
	if err != nil {
		return fmt.Errorf("failed to write messages.xml: %w", err)
	}
	if !changed {
		logger.Debug("messages.xml already up to date", "path", path, "mission", mission.Template)
		return nil
	}

	logger.Info("Wrote messages.xml", "path", path, "mission", mission.Template, "instances", mission.Instances)
	// DayZ loads messages.xml once, when the mission starts, so a running
	// server keeps broadcasting the messages it read earlier.
	logger.Warn("DayZ reads messages.xml at mission load - restart the instance to activate the new messages",
		"units", instanceUnits(mission.Instances), "mission", mission.Template)
	return nil
}

// instanceUnits returns the systemd unit names of the instances that share a
// mission, so the restart hint names the exact services to touch.
func instanceUnits(instances []string) []string {
	units := make([]string, 0, len(instances))
	for _, name := range instances {
		units = append(units, "dayz@"+name+".service")
	}
	return units
}

// writeFileIfChanged writes content only when it differs from what is already
// on disk, so repeated `dayzctl apply` runs do not touch the file.
func writeFileIfChanged(path string, content []byte) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return false, err
	}
	return true, nil
}

// buildMessageEntries validates the configured messages and converts them into
// template data, keeping every time value in minutes (DayZ's own unit).
func buildMessageEntries(template string, messages []config.ShutdownMessage) ([]messageEntry, error) {
	entries := make([]messageEntry, 0, len(messages))
	for i, m := range messages {
		if err := validateShutdownMessage(template, i, m); err != nil {
			return nil, err
		}
		entries = append(entries, messageEntry{
			Delay:     m.Delay,
			Repeat:    m.Repeat,
			Deadline:  m.Deadline,
			OnConnect: flag(m.OnConnect),
			Shutdown:  flag(m.Shutdown),
			// DayZ parses this file as XML: an unescaped '&' or '<' makes the
			// whole document invalid and every message disappears.
			Text: escapeXMLText(m.Text),
		})
	}
	return entries, nil
}

// warnIfMissionMissing points out the most common reason a perfectly valid
// messages.xml does nothing: the server is not running that mission, so the
// file lands in a folder DayZ never opens.
func warnIfMissionMissing(installDir, template string) {
	missionsDir := filepath.Join(installDir, "mpmissions")
	entries, err := os.ReadDir(missionsDir)
	if err != nil {
		return // mpmissions may not exist yet; nothing useful to say
	}

	var available []string
	missionFound := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		available = append(available, entry.Name())
		if entry.Name() == template {
			missionFound = true
		}
	}
	if missionFound {
		return
	}

	logger.Warn("Mission folder does not exist, DayZ will not read this messages.xml - check the instance 'template'",
		"mission", template, "expected", filepath.Join(missionsDir, template), "available", available)
}

// flag converts a bool to the 0/1 integer DayZ expects in its flag elements.
func flag(enabled bool) int {
	if enabled {
		return 1
	}
	return 0
}

// xmlTextEscaper escapes only what is required inside an XML text node. Quotes
// and apostrophes are left alone: they are legal here, and leaving them raw
// keeps the in-game chat text exactly as written in the config.
var xmlTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escapeXMLText makes a configured message safe to embed in <text>.
func escapeXMLText(text string) string {
	return xmlTextEscaper.Replace(text)
}

// validateShutdownMessage rejects configurations DayZ would silently ignore.
// A message with <shutdown>1</shutdown> but no deadline is exactly that: the
// Shutdown flag only matters once the Countdown flag is enabled, so it is
// reported as an error instead of producing a file that never shuts down.
func validateShutdownMessage(mission string, index int, m config.ShutdownMessage) error {
	where := fmt.Sprintf("mission %s: shutdown_messages[%d]", mission, index)

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
	if m.Delay < 0 {
		return fmt.Errorf("%s: delay must not be negative (it is a number of minutes)", where)
	}
	if m.Shutdown && m.Deadline == 0 {
		return fmt.Errorf("%s: shutdown requires deadline > 0 - DayZ ignores the Shutdown flag unless the message is a countdown", where)
	}
	if m.Delay > 0 && !m.OnConnect {
		logger.Warn("messages.xml: delay only applies to on_connect messages; DayZ ignores it here",
			"mission", mission, "index", index)
	}
	if m.Deadline > 0 && !strings.Contains(m.Text, "#tmin") {
		logger.Warn("messages.xml: countdown text has no #tmin placeholder; DayZ repeats the same text at "+countdownOffsets+
			" minutes before the deadline", "mission", mission, "index", index)
	}
	return nil
}

// MessagesPath returns the path of the messages.xml that holds the messages of
// the given instance's mission, without generating it.
func MessagesPath(cfg *config.ServerConfig, instance *config.Instance) string {
	return MessagesPathForMission(cfg, instance.Template)
}

// MessagesPathForMission returns the messages.xml path for a mission template.
func MessagesPathForMission(cfg *config.ServerConfig, template string) string {
	return filepath.Join(cfg.GetInstallDir(), "mpmissions", template, "db", "messages.xml")
}

// MessagesPaths returns the messages.xml path of every mission (map) that
// would receive a file for the given instances, deduplicated and in order.
func MessagesPaths(cfg *config.ServerConfig, instances []*config.Instance) ([]string, error) {
	missions, err := resolveMissions(cfg, instances)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(missions))
	for _, mission := range missions {
		paths = append(paths, MessagesPathForMission(cfg, mission.Template))
	}
	return paths, nil
}
