package generate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

// TestGenerateMessagesWritesExpectedXML checks one <message> per message kind:
// welcome (onconnect), cyclic (repeat) and the shutdown countdown.
func TestGenerateMessagesWritesExpectedXML(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		Messages: config.Messages{
			Welcome:  &config.WelcomeMessage{Text: "Welcome to #name"},
			Cyclic:   []config.CyclicMessage{{Every: 30, Text: "Join our Discord"}},
			Shutdown: &config.ShutdownMessage{Deadline: 360, Text: "Restart in #tmin minutes", StopServer: true},
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
		"<onconnect>1</onconnect>",
		"Welcome to #name",
		"<repeat>30</repeat>",
		"Join our Discord",
		"<deadline>360</deadline>",
		"<shutdown>1</shutdown>",
		"Restart in #tmin minutes",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("messages.xml missing expected content %q; got:\n%s", want, content)
		}
	}

	// Only the elements DayZ documents are written, and each message kind
	// carries just its own: no <repeat> or <deadline> on the welcome message,
	// no <delay>/<onconnect> on the others, and never <id>/<playerCount>.
	for _, unwanted := range []string{
		"<id>", "<playerCount>",
		"<delay>0</delay>",
		"<onconnect>0</onconnect>",
		"<shutdown>0</shutdown>",
		"<repeat>0</repeat>",
		"<deadline>0</deadline>",
	} {
		if strings.Contains(content, unwanted) {
			t.Errorf("messages.xml contains %q, which should not be written; got:\n%s", unwanted, content)
		}
	}

	if strings.Count(content, "<message>") != 3 {
		t.Errorf("expected 3 messages (welcome, cyclic, shutdown), got:\n%s", content)
	}

	if got := MessagesPath(cfg, inst); got != path {
		t.Errorf("MessagesPath() = %s, want %s", got, path)
	}
}

// TestMinutesAreWrittenAsMinutes guards the unit: DayZ expects minutes, and
// every time value in the config is a minute value.
func TestMinutesAreWrittenAsMinutes(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		Messages: config.Messages{
			Welcome:  &config.WelcomeMessage{Text: "welcome", Delay: 2},
			Cyclic:   []config.CyclicMessage{{Every: 15, Text: "cyclic"}},
			Shutdown: &config.ShutdownMessage{Deadline: 120, Text: "shutdown"},
		},
	}
	inst := &config.Instance{Name: "conv", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}
	content := string(data)
	for _, want := range []string{"<delay>2</delay>", "<repeat>15</repeat>", "<deadline>120</deadline>"} {
		if !strings.Contains(content, want) {
			t.Errorf("messages.xml missing %q; got:\n%s", want, content)
		}
	}
	// stop_server is off, so no <shutdown> element at all.
	if strings.Contains(content, "<shutdown>") {
		t.Errorf("messages.xml should not carry <shutdown> when stop_server is false; got:\n%s", content)
	}
}

// TestCyclicCanAlsoBeSentOnConnect covers a message that is broadcast every N
// minutes and also sent to each connecting player (DayZ example 3).
func TestCyclicCanAlsoBeSentOnConnect(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		Messages: config.Messages{
			Cyclic: []config.CyclicMessage{
				{Every: 50, OnConnect: true, Delay: 10, Text: "Check our web page at www.dayz.com"},
			},
		},
	}
	inst := &config.Instance{Name: "both", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}
	content := string(data)

	if strings.Count(content, "<message>") != 1 {
		t.Fatalf("expected a single message, got:\n%s", content)
	}
	for _, want := range []string{"<delay>10</delay>", "<repeat>50</repeat>", "<onconnect>1</onconnect>"} {
		if !strings.Contains(content, want) {
			t.Errorf("messages.xml missing %q; got:\n%s", want, content)
		}
	}
}

// TestGenerateMessagesWritesOncePerMission is the point of the top-level
// 'messages' block: several instances running the same mission share one
// messages.xml, and each distinct mission gets its own copy.
func TestGenerateMessagesWritesOncePerMission(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		Messages: config.Messages{
			Shutdown: &config.ShutdownMessage{Deadline: 360, Text: "Restart in #tmin minutes", StopServer: true},
		},
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

// TestGenerateMessagesInstanceOverrideKeepsWorking keeps the deprecated
// instance-level block usable: it wins for its own mission only.
func TestGenerateMessagesInstanceOverrideKeepsWorking(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:    config.Paths{Base: tmp},
		Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 360, Text: "global #tmin"}},
	}

	instances := []*config.Instance{
		{
			Name:     "custom",
			Template: "dayzOffline.enoch",
			Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 120, Text: "custom #tmin"}},
		},
		{Name: "plain", Template: "dayzOffline.chernarusplus"},
	}

	if err := GenerateMessages(cfg, instances); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	custom, err := os.ReadFile(MessagesPathForMission(cfg, "dayzOffline.enoch"))
	if err != nil {
		t.Fatalf("read custom mission messages.xml: %v", err)
	}
	if !strings.Contains(string(custom), "custom #tmin") || strings.Contains(string(custom), "global #tmin") {
		t.Fatalf("expected the instance override to win for its mission, got:\n%s", custom)
	}

	plain, err := os.ReadFile(MessagesPathForMission(cfg, "dayzOffline.chernarusplus"))
	if err != nil {
		t.Fatalf("read plain mission messages.xml: %v", err)
	}
	if !strings.Contains(string(plain), "global #tmin") {
		t.Fatalf("expected the global messages for the other mission, got:\n%s", plain)
	}
}

// TestGenerateMessagesRejectsConflictingOverrides refuses two instances that
// share a mission but want different messages: only one file can exist.
func TestGenerateMessagesRejectsConflictingOverrides(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	instances := []*config.Instance{
		{
			Name:     "a",
			Template: "dayzOffline.chernarusplus",
			Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 60, Text: "a #tmin"}},
		},
		{
			Name:     "b",
			Template: "dayzOffline.chernarusplus",
			Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 90, Text: "b #tmin"}},
		},
	}

	err := GenerateMessages(cfg, instances)
	if err == nil {
		t.Fatal("expected an error for conflicting instance-level messages on the same mission")
	}
	if !strings.Contains(err.Error(), "top-level 'messages'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateMessagesRequiresTemplate(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "myserver",
		Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 60, Text: "hi"}},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err == nil {
		t.Fatal("expected error when template is missing")
	}
}

func TestOutputUsesLFOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:    config.Paths{Base: tmp},
		Messages: config.Messages{Shutdown: &config.ShutdownMessage{Deadline: 1, Text: "eol test"}},
	}
	inst := &config.Instance{Name: "eol", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}
	if strings.Contains(string(data), "\r") {
		t.Fatalf("expected LF-only file, found CR bytes in %s", MessagesPath(cfg, inst))
	}
}
