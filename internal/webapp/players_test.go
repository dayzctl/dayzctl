package webapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
)

// fakeRCON records the action it was asked to perform.
type fakeRCON struct {
	players    []intrcon.Player
	rawPlayers string
	bans       []intrcon.Ban
	kickErr    error

	kickID  int
	kickWhy string

	banID   int
	banMins int
	banWhy  string

	unbanID int
}

func (f *fakeRCON) Open() error  { return nil }
func (f *fakeRCON) Close() error { return nil }

func (f *fakeRCON) Players() ([]intrcon.Player, error) { return f.players, nil }
func (f *fakeRCON) PlayersRaw() (string, error)        { return f.rawPlayers, nil }
func (f *fakeRCON) Bans() ([]intrcon.Ban, error)       { return f.bans, nil }

func (f *fakeRCON) Kick(id int, reason string) (string, error) {
	f.kickID, f.kickWhy = id, reason
	return "", f.kickErr
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

// A `players` layout the column parser does not recognise must still resolve
// the player from the raw reply instead of failing the kick.
func TestKickFallsBackToRawPlayers(t *testing.T) {
	fake := &fakeRCON{rawPlayers: "Players on server:\nMira  id 7  42 ping\n(1 players in total)"}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "kick", map[string]any{"name": "Mira", "reason": "afk"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.kickID != 7 {
		t.Errorf("kick id = %d, want 7", fake.kickID)
	}
}

// A kick that produced no reply was still sent: report it as unconfirmed
// instead of as a failure, because some BattlEye builds never answer kick.
func TestKickWithoutReplyIsReportedAsSent(t *testing.T) {
	fake := &fakeRCON{
		players: []intrcon.Player{{ID: 3, Name: "Mira"}},
		kickErr: intrcon.ErrNoResponse,
	}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "kick", map[string]any{"name": "Mira"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["warning"] != true {
		t.Errorf("warning = %v, want true (payload %v)", payload["warning"], payload)
	}
}

// The ban list often carries a GUID that never appears in the ADM log, so the
// "Active bans" card unbans by ban-list id instead of by player.
func TestUnbanByBanID(t *testing.T) {
	fake := &fakeRCON{bans: []intrcon.Ban{
		{ID: 0, GUID: "0123456789abcdef0123456789abcdef", Minutes: 59, Reason: "griefing"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "unban", map[string]any{
		"name":  "0123456789abcdef0123456789abcdef",
		"banId": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.unbanID != 0 {
		t.Errorf("unban id = %d, want 0", fake.unbanID)
	}
}

func TestUnbanByBanIDMissing(t *testing.T) {
	srv := newRCONTestServer(t, &fakeRCON{bans: []intrcon.Ban{{ID: 1, GUID: "0123456789abcdef0123456789abcdef"}}})

	rec := postAction(t, srv, "unban", map[string]any{"name": "x", "banId": 7})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

// A ban placed from the web UI records which player it belongs to, so the row
// can offer Unban once the player is offline (the ban only stores a GUID).
func TestBanRecordsPlayerLink(t *testing.T) {
	fake := &fakeRCON{players: []intrcon.Player{
		{ID: 3, GUID: "0123456789abcdef0123456789abcdef", Name: "Mira"},
	}}
	srv := newRCONTestServer(t, fake)

	rec := postAction(t, srv, "ban", map[string]any{"name": "Mira", "minutes": 60})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	name := srv.banHints.player("0123456789abcdef0123456789abcdef")
	if name != "Mira" {
		t.Fatalf("ban hint = %q, want Mira", name)
	}
}

// The ban list response carries the player name when the link is known, which
// is what puts the Unban button on the player's own row.
func TestBansEndpointIncludesPlayerName(t *testing.T) {
	fake := &fakeRCON{bans: []intrcon.Ban{
		{ID: 0, GUID: "0123456789abcdef0123456789abcdef", Minutes: 59, Reason: "griefing"},
	}}
	srv := newRCONTestServer(t, fake)
	srv.banHints.remember("0123456789abcdef0123456789abcdef", "Mira")

	rec := do(t, srv, http.MethodGet, "/api/instances/test/bans", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Bans []struct {
			ID     int    `json:"id"`
			GUID   string `json:"guid"`
			Player string `json:"player"`
		} `json:"bans"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Bans) != 1 || payload.Bans[0].Player != "Mira" {
		t.Fatalf("bans = %+v", payload.Bans)
	}
}

func TestBanLinksPersistAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ban-hints.json")

	first := newBanHints(path)
	first.remember("0123456789ABCDEF0123456789ABCDEF", "Mira")
	first.remember("76561198000000005", "Ox")

	second := newBanHints(path)
	if name := second.player("0123456789abcdef0123456789abcdef"); name != "Mira" {
		t.Fatalf("reloaded hint = %q, want Mira", name)
	}

	// Only the ban that still exists survives the prune.
	second.prune(map[string]struct{}{"0123456789abcdef0123456789abcdef": {}})
	if name := second.player("76561198000000005"); name != "" {
		t.Fatalf("pruned hint still present: %q", name)
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
