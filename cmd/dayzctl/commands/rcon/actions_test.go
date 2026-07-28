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
	sendResp string
}

func (f *fakeClient) Players() ([]intrcon.Player, error)                     { return f.players, nil }
func (f *fakeClient) Send(cmd string) (string, error)                        { return f.sendResp, nil }
func (f *fakeClient) Kick(id int, reason string) (string, error)             { return "ok", nil }
func (f *fakeClient) Ban(id int, minutes int, reason string) (string, error) { return "ok", nil }
func (f *fakeClient) Say(msg string) (string, error)                         { return "ok", nil }

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

	out2 := captureStdout(func() {
		if err := SendAction("testinst", []string{"say", "hello"}); err != nil {
			t.Fatalf("SendAction error: %v", err)
		}
	})
	if !strings.Contains(out2, "resp ok") {
		t.Fatalf("SendAction output unexpected: %s", out2)
	}
}

func TestKickBanSayActions(t *testing.T) {
	_ = setupConfig(t)

	orig := newClient
	defer func() { newClient = orig }()

	newClient = func(port int, password string) Client { return &fakeClient{} }

	if err := KickAction("testinst", []string{"1"}); err != nil {
		t.Fatalf("KickAction error: %v", err)
	}
	if err := BanAction("testinst", []string{"1", "10"}); err != nil {
		t.Fatalf("BanAction error: %v", err)
	}
	if err := SayAction("testinst", []string{"hi"}); err != nil {
		t.Fatalf("SayAction error: %v", err)
	}
}
