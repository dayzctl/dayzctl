package rcon

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
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
	kickedID int
	bannedID int
	unbanned int
}

func (f *fakeClient) Open() error  { return nil }
func (f *fakeClient) Close() error { return nil }

func (f *fakeClient) Players() ([]intrcon.Player, error) { return f.players, nil }
func (f *fakeClient) Bans() ([]intrcon.Ban, error)       { return f.bans, nil }
func (f *fakeClient) Send(cmd string) (string, error)    { return f.sendResp, nil }
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
	return "ok", f.unbanErr
}
func (f *fakeClient) Say(msg string) (string, error) { return "ok", nil }
func (f *fakeClient) Shutdown() (string, error)      { return "ok", nil }

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
	if fake.unbanned != 3 {
		t.Fatalf("unban id = %d, want 3", fake.unbanned)
	}
	if !strings.Contains(out, "ban #3") {
		t.Fatalf("output = %q, want the ban label", out)
	}

	// A plain number is used as the ban id, no list lookup needed.
	fake.unbanned = -1
	_ = UnbanAction("testinst", []string{"7"})
	if fake.unbanned != 7 {
		t.Fatalf("unban id = %d, want 7", fake.unbanned)
	}
}

func TestResolveBanIDReportsKnownBans(t *testing.T) {
	fake := &fakeClient{bans: []intrcon.Ban{
		{ID: 3, GUID: "0123456789abcdef0123456789abcdef"},
	}}

	_, _, err := resolveBanID(fake, "deadbeef")
	if err == nil {
		t.Fatal("expected an error for an unknown GUID")
	}
	if !strings.Contains(err.Error(), "#3 0123456789abcdef0123456789abcdef") {
		t.Fatalf("error = %q, want it to list the known ban", err)
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

// A server that never answers `unban` should not look like a failure: the
// command was sent, so the CLI warns instead of exiting with an error.
func TestUnbanNoReplyIsReportedAsSent(t *testing.T) {
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
	if !strings.Contains(out, "did not confirm") {
		t.Fatalf("output = %q, want a no-confirmation warning", out)
	}
	if fake.unbanned != 0 {
		t.Fatalf("unban id = %d, want 0", fake.unbanned)
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
