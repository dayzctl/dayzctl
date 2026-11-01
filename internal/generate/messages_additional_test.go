package generate

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func TestMinutesAreWrittenAsMinutes(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "conv",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 2, Text: "in 2 minutes", Shutdown: false, Repeat: 15},
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
	if !strings.Contains(content, "<deadline>2</deadline>") {
		t.Fatalf("expected deadline 2 minutes, got:\n%s", content)
	}
	if !strings.Contains(content, "<repeat>15</repeat>") {
		t.Fatalf("expected repeat 15 minutes, got:\n%s", content)
	}
}

func TestOutputUsesLFOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:             "eol",
		Template:         "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 1, Text: "eol test"}},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected messages.xml at %s: %v", path, err)
	}
	if strings.Contains(string(data), "\r") {
		t.Fatalf("expected LF-only file, found CR bytes in %s", path)
	}
}

// TestGenerateMessagesEscapesText guarantees the produced file stays valid XML:
// a raw '&' or '<' inside <text> makes DayZ drop the whole document, which
// looks exactly like "messages.xml is not working".
func TestGenerateMessagesEscapesText(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "escape",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 10, Text: "R&D <3 restart in #tmin minutes"},
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
	if !strings.Contains(content, "<text>R&amp;D &lt;3 restart in #tmin minutes</text>") {
		t.Fatalf("expected escaped text node, got:\n%s", content)
	}
}

func TestGenerateMessagesRejectsShutdownWithoutDeadline(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "nosd",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Text: "shutting down", Shutdown: true},
		},
	}

	err := GenerateMessages(cfg, []*config.Instance{inst})
	if err == nil {
		t.Fatal("expected an error for shutdown without deadline")
	}
	if !strings.Contains(err.Error(), "shutdown requires deadline") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateMessagesRejectsTextOver160Chars(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "toolong",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 10, Text: strings.Repeat("a", 161)},
		},
	}

	err := GenerateMessages(cfg, []*config.Instance{inst})
	if err == nil {
		t.Fatal("expected an error for a text longer than 160 characters")
	}
	if !strings.Contains(err.Error(), "at most 160") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGenerateMessagesIsWellFormedXML decodes the produced file with the
// standard XML parser using the element names DayZ documents, which fails if
// the generator ever emits malformed or renamed elements.
func TestGenerateMessagesIsWellFormedXML(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{Paths: config.Paths{Base: tmp}}

	inst := &config.Instance{
		Name:     "xml",
		Template: "dayzOffline.chernarusplus",
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 360, Text: "#name will restart in #tmin minutes.", Shutdown: true},
			{Deadline: 720, Text: "R&D maintenance", Repeat: 30},
		},
	}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	path := filepath.Join(cfg.GetInstallDir(), "mpmissions", inst.Template, "db", "messages.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}

	type message struct {
		Delay     int    `xml:"delay"`
		Repeat    int    `xml:"repeat"`
		Deadline  int    `xml:"deadline"`
		OnConnect int    `xml:"onConnect"`
		Shutdown  int    `xml:"shutdown"`
		Text      string `xml:"text"`
	}
	var doc struct {
		XMLName  struct{}  `xml:"messages"`
		Messages []message `xml:"message"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("messages.xml is not well formed: %v\n%s", err, data)
	}
	if len(doc.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d:\n%s", len(doc.Messages), data)
	}
	if got := doc.Messages[0]; got.Deadline != 360 || got.Shutdown != 1 || got.Text != "#name will restart in #tmin minutes." {
		t.Errorf("unexpected first message: %+v", got)
	}
	if got := doc.Messages[1]; got.Deadline != 720 || got.Repeat != 30 || got.Shutdown != 0 || got.Text != "R&D maintenance" {
		t.Errorf("unexpected second message: %+v", got)
	}
}
