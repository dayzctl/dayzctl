package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
)

// fakeRCON records the action it was asked to perform.
type fakeRCON struct {
	players []intrcon.Player
	bans    []intrcon.Ban

	kickID  int
	kickWhy string

	banID   int
	banMins int
	banWhy  string

	unbanID int
}

func (f *fakeRCON) Players() ([]intrcon.Player, error) { return f.players, nil }
func (f *fakeRCON) Bans() ([]intrcon.Ban, error)       { return f.bans, nil }

func (f *fakeRCON) Kick(id int, reason string) (string, error) {
	f.kickID, f.kickWhy = id, reason
	return "", nil
}

func (f *fakeRCON) Ban(id, minutes int, reason string) (string, error) {
	f.banID, f.banMins, f.banWhy = id, minutes, reason
	return "", nil
}

func (f *fakeRCON) Unban(banID int) (string, error) {
	f.unbanID = banID
	return "", nil
}

// newRCONTestServer builds a server whose instance has RCON configured, and
// swaps the real client for the given fake.
func newRCONTestServer(t *testing.T, fake *fakeRCON) *Server {
	t.Helper()

	cfg := &config.ServerConfig{
		Paths: config.Paths{InstallDir: t.TempDir()},
		Instances: []config.Instance{{
			Name:    "test",
			Enabled: true,
			RCON:    config.RCON{Enabled: true, Port: 2306, Password: "secret"},
		}},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	old := newRCONClient
	t.Cleanup(func() { newRCONClient = old })
	newRCONClient = func(int, string) rconClient { return fake }

	return srv
}

func postAction(t *testing.T, srv *Server, action string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := do(t, srv, http.MethodPost, "/api/instances/test/players/"+action, raw)
	return rec
}

func TestKickPlayerAction(t *testing.T) {
	fake := &fakeRCON{players: []intrcon.Player{
		{ID: 1, Name: "Other"},
		{ID: 7, Name: "Mira"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "kick", map[string]any{"name": "mira", "reason": "test"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.kickID != 7 || fake.kickWhy != "test" {
		t.Errorf("kick = (%d, %q), want (7, %q)", fake.kickID, fake.kickWhy, "test")
	}
}

func TestKickOfflinePlayer(t *testing.T) {
	srv := newRCONTestServer(t, &fakeRCON{players: []intrcon.Player{{ID: 1, Name: "Other"}}})

	rec := postAction(t, srv, "kick", map[string]any{"name": "Mira"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", rec.Code, rec.Body.String())
	}
}

// The ADM name can be stale (the player renamed since), but the SteamID comes
// from the log and the RCON session list prints it too.
func TestKickPlayerActionMatchesSteamID(t *testing.T) {
	fake := &fakeRCON{players: []intrcon.Player{
		{ID: 9, GUID: "76561198000000005", Name: "Renamed"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "kick", map[string]any{
		"name": "Mira",
		"ids":  []string{"76561198000000005"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.kickID != 9 {
		t.Errorf("kick id = %d, want 9", fake.kickID)
	}
}

func TestBansEndpoint(t *testing.T) {
	fake := &fakeRCON{bans: []intrcon.Ban{
		{ID: 3, GUID: "76561198000000005", Minutes: 60, Reason: "cheating"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := do(t, srv, http.MethodGet, "/api/instances/test/bans", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Bans []intrcon.Ban `json:"bans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Bans) != 1 || payload.Bans[0].GUID != "76561198000000005" {
		t.Fatalf("bans = %+v", payload.Bans)
	}
}

func TestBanPlayerAction(t *testing.T) {
	fake := &fakeRCON{players: []intrcon.Player{{ID: 3, Name: "Mira"}}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "ban", map[string]any{"name": "Mira", "minutes": 60, "reason": "cheating"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.banID != 3 || fake.banMins != 60 || fake.banWhy != "cheating" {
		t.Errorf("ban = (%d, %d, %q), want (3, 60, %q)", fake.banID, fake.banMins, fake.banWhy, "cheating")
	}
}

func TestUnbanPlayerActionMatchesSteamID(t *testing.T) {
	fake := &fakeRCON{bans: []intrcon.Ban{
		{ID: 2, GUID: "76561198000000002", Reason: "griefing"},
		{ID: 5, GUID: "76561198000000005", Reason: "cheating"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "unban", map[string]any{
		"name": "Mira",
		// A session token from the ADM must not be mistaken for a SteamID.
		"ids": []string{"session-token", "76561198000000005"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.unbanID != 5 {
		t.Errorf("unban id = %d, want 5", fake.unbanID)
	}
}

func TestUnbanWithoutSteamID(t *testing.T) {
	srv := newRCONTestServer(t, &fakeRCON{})

	rec := postAction(t, srv, "unban", map[string]any{"name": "Mira", "ids": []string{"session-token"}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", rec.Code, rec.Body.String())
	}
}

func TestUnbanWithoutMatchingBan(t *testing.T) {
	srv := newRCONTestServer(t, &fakeRCON{bans: []intrcon.Ban{{ID: 1, GUID: "76561198000000001"}}})

	rec := postAction(t, srv, "unban", map[string]any{
		"name": "Mira",
		"ids":  []string{"76561198000000009"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

func TestPlayerActionRequiresRCON(t *testing.T) {
	cfg := &config.ServerConfig{
		Paths:     config.Paths{InstallDir: t.TempDir()},
		Instances: []config.Instance{{Name: "test", Enabled: true}},
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	rec := postAction(t, srv, "kick", map[string]any{"name": "Mira"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}
