package webapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
)

// rconClient is the subset of the BattlEye client the player actions need.
// Declaring it as an interface keeps the handlers testable without UDP.
type rconClient interface {
	Players() ([]intrcon.Player, error)
	Bans() ([]intrcon.Ban, error)
	Kick(id int, reason string) (string, error)
	Ban(id int, minutes int, reason string) (string, error)
	Unban(banID int) (string, error)
}

// newRCONClient dials the instance. Tests replace it with a fake.
var newRCONClient = func(port int, password string) rconClient {
	return intrcon.New(port, password)
}

// realSteamID matches a 64-bit Steam account id. This is the only key the ADM
// log and the RCON ban list have in common, so it is what an unban matches on.
var realSteamID = regexp.MustCompile(`^7656119\d{10}$`)

// errPlayerOffline means the name is not in the RCON session list, so there is
// nothing to kick or ban right now.
var errPlayerOffline = errors.New("player not connected")

// playerActionRequest is the body of a kick/ban/unban request. IDs carries the
// SteamIDs the ADM log recorded for that player; it is only used by unban.
type playerActionRequest struct {
	Name    string   `json:"name"`
	IDs     []string `json:"ids,omitempty"`
	Minutes int      `json:"minutes,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// playerActionResponse is what the UI shows as a confirmation toast.
type playerActionResponse struct {
	Action  string `json:"action"`
	Player  string `json:"player"`
	ID      int    `json:"id,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
	Message string `json:"message"`
}

// handlePlayerAction runs one RCON action (kick, ban, unban) for a player name
// read from the analysed log. Kick and ban look the name up in the live RCON
// session list, so they only work while the player is connected; unban matches
// the ban list by SteamID and therefore works for anybody.
func (s *Server) handlePlayerAction(w http.ResponseWriter, r *http.Request) {
	instance := r.PathValue("instance")
	inst := s.findInstance(instance)
	if inst == nil {
		writeError(w, http.StatusNotFound, errInstanceNotFound+instance)
		return
	}

	action := r.PathValue("action")
	if action != "kick" && action != "ban" && action != "unban" {
		writeError(w, http.StatusNotFound, "unknown player action: "+action)
		return
	}

	if !inst.RCON.Enabled || inst.RCON.Port == 0 || inst.RCON.Password == "" {
		writeError(w, http.StatusBadRequest, "RCON is not enabled for instance "+inst.Name)
		return
	}

	var req playerActionRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "player name required")
		return
	}

	client := newRCONClient(inst.RCON.Port, inst.RCON.Password)
	reason := strings.TrimSpace(req.Reason)

	switch action {
	case "kick":
		s.kickPlayer(w, inst, client, name, reason)
	case "ban":
		s.banPlayer(w, inst, client, name, req.Minutes, reason)
	case "unban":
		s.unbanPlayer(w, inst, client, name, req.IDs)
	}
}

func (s *Server) kickPlayer(w http.ResponseWriter, inst *config.Instance, client rconClient, name, reason string) {
	id, err := findOnlinePlayer(client, name)
	if err != nil {
		writeRCONError(w, err, name)
		return
	}

	if _, err := client.Kick(id, reason); err != nil {
		writeError(w, http.StatusBadGateway, "RCON kick failed: "+err.Error())
		return
	}

	logger.Info("Kicked player from the web UI", "player", name, "id", id, "instance", inst.Name)
	writeJSON(w, http.StatusOK, playerActionResponse{
		Action:  "kick",
		Player:  name,
		ID:      id,
		Message: fmt.Sprintf("Kicked %s", name),
	})
}

func (s *Server) banPlayer(w http.ResponseWriter, inst *config.Instance, client rconClient, name string, minutes int, reason string) {
	if minutes < 0 {
		writeError(w, http.StatusBadRequest, "ban duration cannot be negative")
		return
	}

	id, err := findOnlinePlayer(client, name)
	if err != nil {
		writeRCONError(w, err, name)
		return
	}

	if _, err := client.Ban(id, minutes, reason); err != nil {
		writeError(w, http.StatusBadGateway, "RCON ban failed: "+err.Error())
		return
	}

	logger.Info("Banned player from the web UI",
		"player", name, "id", id, "minutes", minutes, "instance", inst.Name)

	message := fmt.Sprintf("Banned %s", name)
	if minutes > 0 {
		message += fmt.Sprintf(" for %d minutes", minutes)
	} else {
		message += " permanently"
	}

	writeJSON(w, http.StatusOK, playerActionResponse{
		Action:  "ban",
		Player:  name,
		ID:      id,
		Minutes: minutes,
		Message: message,
	})
}

func (s *Server) unbanPlayer(w http.ResponseWriter, inst *config.Instance, client rconClient, name string, ids []string) {
	guids := steamIDs(ids)
	if len(guids) == 0 {
		writeError(w, http.StatusUnprocessableEntity,
			"the analysed ADM log has no SteamID for "+name+
				", so there is nothing to match the ban list against")
		return
	}

	bans, err := client.Bans()
	if err != nil {
		writeError(w, http.StatusBadGateway, "RCON ban list failed: "+err.Error())
		return
	}

	ban, ok := findBan(bans, guids)
	if !ok {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("no ban on this server matches %s (%s)", name, strings.Join(guids, ", ")))
		return
	}

	if _, err := client.Unban(ban.ID); err != nil {
		writeError(w, http.StatusBadGateway, "RCON unban failed: "+err.Error())
		return
	}

	logger.Info("Unbanned player from the web UI",
		"player", name, "banId", ban.ID, "guid", ban.GUID, "instance", inst.Name)

	writeJSON(w, http.StatusOK, playerActionResponse{
		Action:  "unban",
		Player:  name,
		ID:      ban.ID,
		Message: fmt.Sprintf("Unbanned %s", name),
	})
}

// findOnlinePlayer resolves the live RCON id for a name. The ADM log only has
// names, and the id in the log is a per-session token, so the session list is
// the only reliable source.
func findOnlinePlayer(client rconClient, name string) (int, error) {
	players, err := client.Players()
	if err != nil {
		return 0, err
	}
	for _, p := range players {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: %s", errPlayerOffline, name)
}

// steamIDs keeps only the values that are real Steam account ids.
func steamIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !realSteamID.MatchString(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// findBan returns the first ban whose GUID is one of the given SteamIDs.
func findBan(bans []intrcon.Ban, guids []string) (intrcon.Ban, bool) {
	for _, ban := range bans {
		for _, guid := range guids {
			if strings.EqualFold(ban.GUID, guid) {
				return ban, true
			}
		}
	}
	return intrcon.Ban{}, false
}

// writeRCONError maps the RCON failures to statuses the UI can act on.
func writeRCONError(w http.ResponseWriter, err error, name string) {
	if errors.Is(err, errPlayerOffline) {
		writeError(w, http.StatusConflict, fmt.Sprintf("%s is not connected to the server", name))
		return
	}
	writeError(w, http.StatusBadGateway, "RCON failed: "+err.Error())
}
