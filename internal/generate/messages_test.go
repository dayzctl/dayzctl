package generate

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

// TestGenerateMessagesWritesExpectedXML checks the generated file matches the
// documented DayZ format (delay, repeat, deadline, onconnect, shutdown, text).
func TestGenerateMessagesWritesExpectedXML(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 30, Text: "Restart in #tmin minutes"},
			{Deadline: 59, Text: "Restarting now!", Shutdown: true},
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
		"<delay>0</delay>",
		"<repeat>0</repeat>",
		"<deadline>30</deadline>",
		"<onconnect>0</onconnect>",
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

	// DayZ parses this file by element name, so the spelling has to be exactly
	// what the working production files use: all lowercase. <onConnect>,
	// <id> and <playerCount> are not part of the format.
	for _, unwanted := range []string{"<id>", "<playerCount>", "<onConnect>"} {
		if strings.Contains(content, unwanted) {
			t.Errorf("messages.xml contains %q, which is not part of the DayZ format; got:\n%s", unwanted, content)
		}
	}

	if got := MessagesPath(cfg, inst); got != path {
		t.Errorf("MessagesPath() = %s, want %s", got, path)
	}
}

// TestGenerateMessagesWritesOncePerMission is the core of the per-map model:
// several instances running the same mission share one messages.xml, and each
// distinct mission gets its own copy.
func TestGenerateMessagesWritesOncePerMission(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 360, Text: "#name restarting in #tmin", Shutdown: true}},
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

// TestGenerateMessagesInstanceOverrideKeepsWorking keeps the deprecated
// per-instance block usable: it wins for its own mission only.
func TestGenerateMessagesInstanceOverrideKeepsWorking(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 360, Text: "global #tmin"}},
	}

	instances := []*config.Instance{
		{
			Name:             "custom",
			Template:         "dayzOffline.enoch",
			ShutdownMessages: []config.ShutdownMessage{{Deadline: 120, Text: "custom #tmin"}},
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
			Name:             "a",
			Template:         "dayzOffline.chernarusplus",
			ShutdownMessages: []config.ShutdownMessage{{Deadline: 60, Text: "a #tmin"}},
		},
		{
			Name:             "b",
			Template:         "dayzOffline.chernarusplus",
			ShutdownMessages: []config.ShutdownMessage{{Deadline: 90, Text: "b #tmin"}},
		},
	}

	err := GenerateMessages(cfg, instances)
	if err == nil {
		t.Fatal("expected an error for conflicting instance-level shutdown_messages on the same mission")
	}
	if !strings.Contains(err.Error(), "top-level 'shutdown_messages'") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGenerateMessagesSkipsMissionsWithoutMessages ensures an existing,
// hand-written mission messages.xml is not clobbered when nothing is
// configured.
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

// TestGenerateMessagesSupportsWelcomeAndRepeating covers the two other DayZ
// message kinds: an on-connect welcome (with delay) and a repeating reminder.
func TestGenerateMessagesSupportsWelcomeAndRepeating(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{
			{OnConnect: true, Text: "Welcome to #name!"},
			{OnConnect: true, Delay: 2, Text: "Check our web page at www.dayz.com"},
			{Repeat: 30, Text: "You're playing on #name."},
		},
	}
	inst := &config.Instance{Name: "welcome", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}

	var doc struct {
		Messages []struct {
			Delay     int    `xml:"delay"`
			Repeat    int    `xml:"repeat"`
			Deadline  int    `xml:"deadline"`
			OnConnect int    `xml:"onconnect"`
			Shutdown  int    `xml:"shutdown"`
			Text      string `xml:"text"`
		} `xml:"message"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("messages.xml is not well formed: %v\n%s", err, data)
	}
	if len(doc.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d:\n%s", len(doc.Messages), data)
	}

	welcome, delayed, repeating := doc.Messages[0], doc.Messages[1], doc.Messages[2]
	if welcome.OnConnect != 1 || welcome.Delay != 0 || welcome.Deadline != 0 {
		t.Errorf("welcome message not rendered as on-connect: %+v", welcome)
	}
	if delayed.OnConnect != 1 || delayed.Delay != 2 {
		t.Errorf("delayed welcome not rendered with delay: %+v", delayed)
	}
	if repeating.Repeat != 30 || repeating.OnConnect != 0 || repeating.Deadline != 0 {
		t.Errorf("repeating message not rendered with repeat: %+v", repeating)
	}
}

// TestGenerateMessagesKeepsTimeValuesInMinutes guards against a minutes ->
// seconds conversion: DayZ expects minutes.
func TestGenerateMessagesKeepsTimeValuesInMinutes(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 2, Text: "in 2 minutes #tmin", Repeat: 15}},
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
	if !strings.Contains(content, "<deadline>2</deadline>") {
		t.Fatalf("expected deadline 2 minutes, got:\n%s", content)
	}
	if !strings.Contains(content, "<repeat>15</repeat>") {
		t.Fatalf("expected repeat 15 minutes, got:\n%s", content)
	}
}

// TestGenerateMessagesEscapesText guarantees the produced file stays valid XML:
// a raw '&' or '<' inside <text> makes DayZ drop the whole document, which
// looks exactly like "messages.xml is not working".
func TestGenerateMessagesEscapesText(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 10, Text: `R&D <3 restart in #tmin minutes, "it's" fine`}},
	}
	inst := &config.Instance{Name: "escape", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}
	// Only XML-breaking characters are escaped; quotes stay readable in game.
	want := `<text>R&amp;D &lt;3 restart in #tmin minutes, "it's" fine</text>`
	if !strings.Contains(string(data), want) {
		t.Fatalf("expected %q, got:\n%s", want, data)
	}
}

func TestGenerateMessagesRejectsShutdownWithoutDeadline(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Text: "shutting down", Shutdown: true}},
	}
	inst := &config.Instance{Name: "nosd", Template: "dayzOffline.chernarusplus"}

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
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 10, Text: strings.Repeat("a", 161)}},
	}
	inst := &config.Instance{Name: "toolong", Template: "dayzOffline.chernarusplus"}

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
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 360, Text: "#name will restart in #tmin minutes.", Shutdown: true},
			{Deadline: 720, Text: "R&D maintenance", Repeat: 30},
		},
	}
	inst := &config.Instance{Name: "xml", Template: "dayzOffline.chernarusplus"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}

	type message struct {
		Delay     int    `xml:"delay"`
		Repeat    int    `xml:"repeat"`
		Deadline  int    `xml:"deadline"`
		OnConnect int    `xml:"onconnect"`
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

// TestGenerateMessagesMatchesWorkingServer reproduces the messages.xml of a
// live DayZ server (SumrakDZN/Namalsk-Server,
// "Mission Files/regular.namalsk/db/messages.xml"), which uses all six
// lowercase elements: delay, repeat, deadline, onconnect, shutdown, text.
func TestGenerateMessagesMatchesWorkingServer(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths: config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{
			{Deadline: 480, Shutdown: true, Text: "This server will restart in #tmin minutes."},
		},
	}
	inst := &config.Instance{Name: "namalsk", Template: "regular.namalsk"}

	if err := GenerateMessages(cfg, []*config.Instance{inst}); err != nil {
		t.Fatalf("GenerateMessages error: %v", err)
	}

	data, err := os.ReadFile(MessagesPath(cfg, inst))
	if err != nil {
		t.Fatalf("read messages.xml: %v", err)
	}

	want := `<messages>` +
		`<message>` +
		`<delay>0</delay>` +
		`<repeat>0</repeat>` +
		`<deadline>480</deadline>` +
		`<onconnect>0</onconnect>` +
		`<shutdown>1</shutdown>` +
		`<text>This server will restart in #tmin minutes.</text>` +
		`</message>` +
		`</messages>`

	if got := compactXML(string(data)); got != want {
		t.Fatalf("generated messages.xml does not match the working reference\n got: %s\nwant: %s", got, want)
	}
}

// compactXML drops the declaration, comments and inter-tag whitespace so the
// structure of a document can be compared to a reference.
func compactXML(document string) string {
	if idx := strings.Index(document, "<messages>"); idx >= 0 {
		document = document[idx:]
	}
	document = strings.ReplaceAll(document, "\n", "")
	document = strings.ReplaceAll(document, "\r", "")
	document = strings.ReplaceAll(document, "    ", "")
	return document
}

// TestWriteFileIfChanged keeps `dayzctl apply` from rewriting messages.xml on
// every run, so the "restart needed" hint only appears on real changes.
func TestWriteFileIfChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.xml")

	changed, err := writeFileIfChanged(path, []byte("one"))
	if err != nil || !changed {
		t.Fatalf("first write: changed=%v err=%v, want changed=true", changed, err)
	}

	changed, err = writeFileIfChanged(path, []byte("one"))
	if err != nil || changed {
		t.Fatalf("identical write: changed=%v err=%v, want changed=false", changed, err)
	}

	changed, err = writeFileIfChanged(path, []byte("two"))
	if err != nil || !changed {
		t.Fatalf("changed write: changed=%v err=%v, want changed=true", changed, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "two" {
		t.Fatalf("content = %q, want %q", data, "two")
	}
}

func TestOutputUsesLFOnly(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Paths:            config.Paths{Base: tmp},
		ShutdownMessages: []config.ShutdownMessage{{Deadline: 1, Text: "eol test"}},
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
