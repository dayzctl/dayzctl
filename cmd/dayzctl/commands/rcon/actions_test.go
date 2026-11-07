package rcon

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/banhints"
	"github.com/dayzctl/dayzctl/internal/config"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
)

func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func setupConfig(t *testing.T) *config.ServerConfig {
	t.Helper()
	tmp := t.TempDir()
	cfg := &config.ServerConfig{
		Steam: config.Steam{Username: "testuser"},
		Paths: config.Paths{Base: tmp},
		Instances: []config.Instance{
			{
				Name:    "testinst",
				Port:    2302,
				Enabled: true,
				RCON:    config.RCON{Enabled: true, Port: 2303, Password: "pw"},
			},
		},
		Updates: config.Updates{Enabled: false},
	}
	shared.Config = cfg
	return cfg
}

type fakeClient struct {
	players  []intrcon.Player
	bans     []intrcon.Ban
	sendResp string
	unbanErr error
	// unbanNoEffect makes `unban <id>` leave the list untouched, like a server
	// that wants the GUID instead of the list index.
	unbanNoEffect bool
	// sendNoEffect makes raw commands (the GUID/removeBan attempts) do nothing.
	sendNoEffect bool
	// removeBanWorks makes `removeBan <ref>` remove the ban, like a build that
	// uses that spelling instead of `unban`.
	removeBanWorks bool
	// guidOnly makes the server ignore `unban <id>` and honour the GUID form.
	guidOnly bool
	sent     []string
	kickedID int
	bannedID int
	unbanned int
}

func (f *fakeClient) Open() error  { return nil }
func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Players() ([]intrcon.Player, error) { return f.players, nil }
func (f *fakeClient) Bans() ([]intrcon.Ban, error)       { return f.bans, nil }

func (f *fakeClient) Send(cmd string) (string, error) { return f.run(cmd) }
func (f *fakeClient) Run(cmd string) (string, error)  { return f.run(cmd) }

func (f *fakeClient) run(cmd string) (string, error) {
	f.sent = append(f.sent, cmd)
	if f.sendNoEffect {
		return f.sendResp, nil
	}
	switch {
	case strings.HasPrefix(cmd, "unban ") && !f.unbanNoEffect:
		ref := strings.TrimSpace(strings.TrimPrefix(cmd, "unban "))
		if _, err := strconv.Atoi(ref); err != nil || !f.guidOnly {
			f.removeByRef(ref)
		}
	case strings.HasPrefix(cmd, "removeBan ") && f.removeBanWorks:
		f.removeByRef(strings.TrimSpace(strings.TrimPrefix(cmd, "removeBan ")))
	}
	return f.sendResp, f.unbanErr
}

func (f *fakeClient) removeByRef(ref string) {
	f.removeBans(func(b intrcon.Ban) bool {
		return strings.EqualFold(strings.TrimSpace(b.GUID), ref) ||
			strings.EqualFold(strings.TrimSpace(b.IP), ref) ||
			strconv.Itoa(b.ID) == ref
	})
}

func (f *fakeClient) Kick(id int, reason string) (string, error) {
	f.kickedID = id
	return "ok", nil
}
func (f *fakeClient) Ban(id int, minutes int, reason string) (string, error) {
	f.bannedID = id
	return "ok", nil
}
func (f *fakeClient) Unban(banID int) (string, error) {
	f.unbanned = banID
	if !f.unbanNoEffect {
		f.removeBans(func(b intrcon.Ban) bool { return b.ID == banID })
	}
	return "ok", f.unbanErr
}

// RemoveBan mirrors the real client: every spelling is tried, and the result is
// read back from the (fake) ban list.
func (f *fakeClient) RemoveBan(ban intrcon.Ban) ([]string, error) {
	var attempts []string
	for _, cmd := range intrcon.UnbanCommands(ban) {
		reply, err := f.run(cmd)
		attempts = append(attempts, fmt.Sprintf("%s -> %q", cmd, reply))
		if err != nil && !errors.Is(err, intrcon.ErrNoResponse) {
			return attempts, err
		}
		if !intrcon.BanStillListed(f.bans, ban) {
			return attempts, nil
		}
	}
	if f.unbanErr != nil {
		return attempts, fmt.Errorf("%w: %v", intrcon.ErrBanNotRemoved, f.unbanErr)
	}
	return attempts, intrcon.ErrBanNotRemoved
}
func (f *fakeClient) Say(msg string) (string, error) { return "ok", nil }
func (f *fakeClient) Shutdown() (string, error)      { return "ok", nil }

func (f *fakeClient) removeBans(match func(intrcon.Ban) bool) {
	kept := f.bans[:0]
	for _, ban := range f.bans {
		if !match(ban) {
			kept = append(kept, ban)
		}
	}
	f.bans = kept
}

func TestPlayersAndSendActions(t *testing.T) {
	_ = setupConfig(t)

	orig := newClient
	defer func() { newClient = orig }()

	newClient = func(port int, password string) Client {
		return &fakeClient{
			players:  []intrcon.Player{{ID: 1, IP: "1.2.3.4", Port: "2302", Ping: "50", GUID: "guid", Name: "PlayerOne"}},
			sendResp: "resp ok",
		}
	}

	out := captureStdout(func() {
		if err := PlayersAction("testinst", nil); err != nil {
			t.Fatalf("PlayersAction error: %v", err)
		}
	})
	if !strings.Contains(out, "PlayerOne") {
		t.Fatalf("PlayersAction output missing player: %s", out)
	}
	for _, heading := range []string{"ID", "IP", "PORT", "PING", "GUID", "NAME"} {
		if !strings.Contains(out, heading) {
			t.Fatalf("PlayersAction output missing heading %q: %s", heading, out)
		}
	}

	out2 := captureStdout(func() {
		if err := SendAction("testinst", []string{"say", "hello"}); err != nil {
			t.Fatalf("SendAction error: %v", err)
		}
	})
	if !strings.Contains(out2, "resp ok") {
		t.Fatalf("SendAction output unexpected: %s", out2)
	}
}

func TestUnbanActionByGUIDAndID(t *testing.T) {
	_ = setupConfig(t)

	orig := newClient
	defer func() { newClient = orig }()

	fake := &fakeClient{bans: []intrcon.Ban{
		{ID: 3, GUID: "0123456789abcdef0123456789abcdef"},
		{ID: 5, GUID: "76561198000000005"},
	}}
	newClient = func(port int, password string) Client { return fake }

	out := captureStdout(func() {
		if err := UnbanAction("testinst", []string{"0123456789ABCDEF0123456789ABCDEF"}); err != nil {
			t.Fatalf("UnbanAction error: %v", err)
		}
	})
	if !strings.Contains(out, "ban #3") {
		t.Fatalf("output = %q, want the ban label", out)
	}
	if len(fake.bans) != 1 || fake.bans[0].ID != 5 {
		t.Fatalf("bans = %+v, want only ban #5 left", fake.bans)
	}

	// A plain number is used as the ban id, no list lookup needed.
	if err := UnbanAction("testinst", []string{"5"}); err != nil {
		t.Fatalf("UnbanAction(5) error: %v", err)
	}
	if len(fake.bans) != 0 {
		t.Fatalf("bans = %+v, want all bans removed", fake.bans)
	}
}

func TestResolveBanReportsKnownBans(t *testing.T) {
	fake := &fakeClient{bans: []intrcon.Ban{
		{ID: 3, GUID: "0123456789abcdef0123456789abcdef"},
	}}

	_, _, err := resolveBan(fake, "deadbeef", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown GUID")
	}
	if !strings.Contains(err.Error(), "#3 0123456789abcdef0123456789abcdef") {
		t.Fatalf("error = %q, want it to list the known ban", err)
	}
}

func TestResolveBanByName(t *testing.T) {
	players := []intrcon.Player{{ID: 1, GUID: "0123456789abcdef0123456789abcdef", Name: "Mira"}}
	bans := []intrcon.Ban{{ID: 3, GUID: "0123456789abcdef0123456789abcdef"}}

	// Connected: the name resolves to the live player's GUID.
	connected := &fakeClient{players: players, bans: bans}
	ban, label, err := resolveBan(connected, "mira", nil)
	if err != nil || ban.ID != 3 {
		t.Fatalf("resolveBan(connected) = (%d, %q, %v), want ban 3", ban.ID, label, err)
	}

	// Offline: the web UI's GUID → name hint is the only link left.
	hints := banhints.New(filepath.Join(t.TempDir(), "ban-hints.json"))
	hints.Remember("0123456789abcdef0123456789abcdef", "Mira")

	offline := &fakeClient{bans: bans}
	ban, label, err = resolveBan(offline, "MIRA", hints)
	if err != nil || ban.ID != 3 {
		t.Fatalf("resolveBan(hint) = (%d, %q, %v), want ban 3", ban.ID, label, err)
	}
	if !strings.Contains(label, "Mira") {
		t.Fatalf("label = %q, want the player name", label)
	}
}

func TestKickBanSayActions(t *testing.T) {
	_ = setupConfig(t)

	orig := newClient
	defer func() { newClient = orig }()

	fake := &fakeClient{players: []intrcon.Player{
		{ID: 1, IP: "1.2.3.4", Port: "2302", GUID: "76561198000000001", Name: "PlayerOne"},
		{ID: 2, IP: "5.6.7.8", Port: "2302", GUID: "76561198000000002", Name: "Other Player"},
	}}
	newClient = func(port int, password string) Client { return fake }

	// Kick and ban accept any of id, GUID, IP or name.
	for _, ref := range []string{"1", "76561198000000001", "1.2.3.4", "playerone"} {
		fake.kickedID = -1
		if err := KickAction("testinst", []string{ref, "afk"}); err != nil {
			t.Fatalf("KickAction(%q) error: %v", ref, err)
		}
		if fake.kickedID != 1 {
			t.Fatalf("KickAction(%q) id = %d, want 1", ref, fake.kickedID)
		}
	}

	for _, ref := range []string{"2", "76561198000000002", "5.6.7.8:2302", "Other Player"} {
		fake.bannedID = -1
		if err := BanAction("testinst", []string{ref, "10"}); err != nil {
			t.Fatalf("BanAction(%q) error: %v", ref, err)
		}
		if fake.bannedID != 2 {
			t.Fatalf("BanAction(%q) id = %d, want 2", ref, fake.bannedID)
		}
	}

	if err := SayAction("testinst", []string{"hi"}); err != nil {
		t.Fatalf("SayAction error: %v", err)
	}
	if err := ShutdownAction("testinst", nil); err != nil {
		t.Fatalf("ShutdownAction error: %v", err)
	}
}

// A server that never answers `unban` but does drop the ban is a success; the
// list is the authority, not the missing reply.
func TestUnbanNoReplyConfirmedByBanList(t *testing.T) {
	cfg := setupConfig(t)
	inst := &cfg.Instances[0]

	fake := &fakeClient{
		bans:     []intrcon.Ban{{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}},
		unbanErr: intrcon.ErrNoResponse,
	}

	out := captureStdout(func() {
		if err := processUnbanForInstance(inst, "0123456789abcdef0123456789abcdef", func(int, string) Client { return fake }, 1); err != nil {
			t.Fatalf("processUnbanForInstance error: %v", err)
		}
	})
	if !strings.Contains(out, "Removed") {
		t.Fatalf("output = %q, want a removal", out)
	}
	if fake.unbanned != 0 {
		t.Fatalf("unban id = %d, want 0", fake.unbanned)
	}
}

// Some builds ignore `unban <id>`; the GUID form is tried next.
func TestUnbanFallsBackToGUID(t *testing.T) {
	cfg := setupConfig(t)
	inst := &cfg.Instances[0]

	fake := &fakeClient{
		bans:     []intrcon.Ban{{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}},
		guidOnly: true,
	}

	out := captureStdout(func() {
		if err := processUnbanForInstance(inst, "0123456789abcdef0123456789abcdef", func(int, string) Client { return fake }, 1); err != nil {
			t.Fatalf("processUnbanForInstance error: %v", err)
		}
	})
	if !strings.Contains(out, "Removed") {
		t.Fatalf("output = %q, want a removal", out)
	}

	found := false
	for _, cmd := range fake.sent {
		if cmd == "unban 0123456789abcdef0123456789abcdef" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent commands = %v, want the GUID form", fake.sent)
	}
}

// A build that does not know `unban` is probed with the other spellings until
// one of them verifiably removes the entry.
func TestUnbanTriesCommandVariants(t *testing.T) {
	cfg := setupConfig(t)
	inst := &cfg.Instances[0]

	fake := &fakeClient{
		bans:           []intrcon.Ban{{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}},
		unbanNoEffect:  true,
		removeBanWorks: true,
	}

	out := captureStdout(func() {
		if err := processUnbanForInstance(inst, "0123456789abcdef0123456789abcdef", func(int, string) Client { return fake }, 1); err != nil {
			t.Fatalf("processUnbanForInstance error: %v", err)
		}
	})
	if !strings.Contains(out, "Removed") {
		t.Fatalf("output = %q, want a removal", out)
	}

	found := false
	for _, cmd := range fake.sent {
		if cmd == "removeBan 0" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sent commands = %v, want the removeBan attempt", fake.sent)
	}
}

// If no spelling removes the entry, the failure lists exactly what the server
// answered for each attempt.
func TestUnbanReportsAttempts(t *testing.T) {
	cfg := setupConfig(t)
	inst := &cfg.Instances[0]

	fake := &fakeClient{
		bans:          []intrcon.Ban{{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}},
		unbanNoEffect: true,
	}

	err := processUnbanForInstance(inst, "0123456789abcdef0123456789abcdef", func(int, string) Client { return fake }, 1)
	if err == nil {
		t.Fatal("expected an error when no command removes the ban")
	}
	for _, want := range []string{"unban 0 ->", "removeBan 0123456789abcdef0123456789abcdef ->"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to include %q", err, want)
		}
	}
}
func TestUnbanReportsBanStillListed(t *testing.T) {
	cfg := setupConfig(t)
	inst := &cfg.Instances[0]

	fake := &fakeClient{
		bans:          []intrcon.Ban{{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}},
		unbanNoEffect: true,
		sendNoEffect:  true,
	}

	err := processUnbanForInstance(inst, "0123456789abcdef0123456789abcdef", func(int, string) Client { return fake }, 1)
	if err == nil {
		t.Fatal("expected an error when the ban is still listed")
	}
	if !strings.Contains(err.Error(), "could not remove") {
		t.Fatalf("error = %q, want the still-listed message", err)
	}
}

func TestResolvePlayerRefUnknown(t *testing.T) {
	fake := &fakeClient{players: []intrcon.Player{
		{ID: 1, IP: "1.2.3.4", GUID: "76561198000000001", Name: "PlayerOne"},
	}}

	_, err := resolvePlayerRef(fake, "Nobody")
	if err == nil {
		t.Fatal("expected an error for an unknown player")
	}
	if !strings.Contains(err.Error(), "PlayerOne (#1)") {
		t.Fatalf("error = %q, want it to list online players", err)
	}
}
