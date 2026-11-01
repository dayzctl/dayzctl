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
// generator writes them as minutes in the produced db/messages.xml.
type messageEntry struct {
	ID          int
	Deadline    int // minutes
	Repeat      int // minutes
	PlayerCount int
	Shutdown    int
	Text        string
}

// resolvedMission is one mission (map) folder that receives a messages.xml,
// together with the message list that belongs to it.
type resolvedMission struct {
	Template  string
	Instances []string
	Messages  []config.ShutdownMessage
}

// GenerateMessages writes db/messages.xml once for every mission (map) used by
// the given instances. DayZ reads the file from the mission folder
// (<installDir>/mpmissions/<template>/db/messages.xml), so it is shared by all
// instances running that mission - writing it per instance would overwrite the
// same path repeatedly. The top-level shutdown_messages are used for every
// mission; a (deprecated) instance-level block overrides them for its own
// mission. Missions with nothing configured are left untouched.
func GenerateMessages(cfg *config.ServerConfig, instances []*config.Instance) error {
	missions, err := resolveMissions(cfg, instances)
	if err != nil {
		return err
	}
	installDir := cfg.GetInstallDir()
	for _, mission := range missions {
		if err := generateMissionMessages(installDir, mission); err != nil {
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

// generateMissionMessages writes db/messages.xml for a single mission.
func generateMissionMessages(installDir string, mission resolvedMission) error {
	// Build template data. Keep values in minutes (no conversion).
	var msgs []messageEntry
	for i, m := range mission.Messages {
		shutdown := 0
		if m.Shutdown {
			shutdown = 1
		}
		msgs = append(msgs, messageEntry{
			ID:          i,
			Deadline:    m.Deadline,
			Repeat:      m.Repeat,
			PlayerCount: m.PlayerCount,
			Shutdown:    shutdown,
			Text:        m.Text,
		})
	}

	dbDir := filepath.Join(installDir, "mpmissions", mission.Template, "db")
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
	logger.Info("Wrote messages.xml", "path", path, "mission", mission.Template, "instances", mission.Instances)
	return nil
}

// MessagesPath returns the path where messages.xml would be written for
// the given instance, without generating it.
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
