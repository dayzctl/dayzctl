package generate

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"text/template"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

//go:embed templates/messages.xml.tmpl
var messagesTmpl string

// messagesFile is the root <messages> element of a mission's db/messages.xml.
// messageEntry is a single <message> entry: the three config sections
// (welcome, cyclic, shutdown) all end up here, one per message kind.
// All time values are written in minutes, which is DayZ's unit.
type messageEntry struct {
	Delay     int // minutes after connect, welcome messages only
	Repeat    int // minutes between broadcasts, cyclic messages only
	Deadline  int // minutes after server start, shutdown countdown only
	OnConnect int // 0/1
	Shutdown  int // 0/1
	Text      string
}

// resolvedMission is one mission (map) folder that receives a messages.xml,
// together with the messages that belong to it.
type resolvedMission struct {
	Template  string
	Instances []string
	Messages  config.Messages
	override  bool // set when an instance defined its own messages block
}

// GenerateMessages writes db/messages.xml once for every mission (map) used by
// the given instances. DayZ reads the file from the mission folder
// (<installDir>/mpmissions/<template>/db/messages.xml), so it is shared by all
// instances running that mission - writing it per instance would overwrite the
// same path repeatedly. The top-level 'messages' block is used for every
// mission; a (deprecated) instance-level block overrides it for its own
// mission. Missions with no messages configured are left untouched.
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

// resolveMissions groups instances by mission template and picks the messages
// for each of them.
func resolveMissions(cfg *config.ServerConfig, instances []*config.Instance) ([]resolvedMission, error) {
	byTemplate := make(map[string]*resolvedMission)
	var order []string

	for _, instance := range instances {
		override := instance.Messages
		hasOverride := !override.Empty()
		if instance.Template == "" {
			if hasOverride {
				return nil, fmt.Errorf("instance %s has no mission 'template' set", instance.Name)
			}
			if !cfg.Messages.Empty() {
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

		if hasOverride {
			if mission.override && !sameMessages(mission.Messages, override) {
				return nil, fmt.Errorf(
					"instances %v share mission %s but define different messages; messages.xml is per mission, so move the block to the top-level 'messages'",
					append(mission.Instances, instance.Name), instance.Template)
			}
			mission.Messages = override
			mission.override = true
			logger.Warn("Instance-level 'messages' is deprecated: messages.xml is stored per mission (map), not per instance; move the block to the top-level 'messages'",
				"instance", instance.Name, "mission", instance.Template)
		}
		mission.Instances = append(mission.Instances, instance.Name)
	}

	resolved := make([]resolvedMission, 0, len(order))
	for _, template := range order {
		mission := byTemplate[template]
		if !mission.override {
			mission.Messages = cfg.Messages
		} else if !cfg.Messages.Empty() {
			logger.Warn("Top-level 'messages' ignored for this mission because an instance overrides them",
				"mission", template, "instances", mission.Instances)
		}
		if mission.Messages.Empty() {
			logger.Debug("No messages configured, leaving messages.xml untouched", "mission", template)
			continue
		}
		resolved = append(resolved, *mission)
	}
	return resolved, nil
}

// sameMessages reports whether two message blocks are identical.
func sameMessages(a, b config.Messages) bool {
	return reflect.DeepEqual(a, b)
}

// generateMissionMessages writes db/messages.xml for a single mission.
func generateMissionMessages(installDir string, mission resolvedMission) error {
	msgs := buildMessageEntries(mission.Messages)

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

// buildMessageEntries turns the configured messages into template data: one
// welcome message, then the cyclic broadcasts, then the shutdown countdown.
func buildMessageEntries(messages config.Messages) []messageEntry {
	entries := make([]messageEntry, 0, len(messages.Cyclic)+2)

	if welcome := messages.Welcome; welcome != nil {
		entries = append(entries, messageEntry{
			Delay:     welcome.Delay,
			OnConnect: 1,
			Text:      welcome.Text,
		})
	}

	for _, cyclic := range messages.Cyclic {
		entries = append(entries, messageEntry{
			Repeat: cyclic.Every,
			Text:   cyclic.Text,
		})
	}

	if shutdown := messages.Shutdown; shutdown != nil {
		shutdownFlag := 0
		if shutdown.StopServer {
			shutdownFlag = 1
		}
		entries = append(entries, messageEntry{
			Deadline: shutdown.Deadline,
			Shutdown: shutdownFlag,
			Text:     shutdown.Text,
		})
	}

	return entries
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
