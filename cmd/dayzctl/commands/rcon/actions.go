package rcon

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/banhints"
	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
	"github.com/dayzctl/dayzctl/internal/systemd"
)

// Client is the subset of rcon client methods used by actions; defined
// as an interface to allow tests to inject fakes.
type Client interface {
	Open() error
	Close() error
	Players() ([]intrcon.Player, error)
	Bans() ([]intrcon.Ban, error)
	Send(command string) (string, error)
	Kick(id int, reason string) (string, error)
	Ban(id int, minutes int, reason string) (string, error)
	Unban(banID int) (string, error)
	Say(msg string) (string, error)
	Shutdown() (string, error)
}

// withSession opens one authenticated session for a whole action. Looking a
// player up and then kicking them must not use two short-lived connections:
// some servers only answer the session they just authenticated (the second
// command then times out).
func withSession(client Client, fn func() error) error {
	if err := client.Open(); err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	return fn()
}

// sentWithoutReply reports a command the server never acknowledged. UDP has no
// delivery guarantee and some BattlEye builds do not answer kick/ban/unban, so
// the command may well have run: warn instead of failing.
func sentWithoutReply(action, target, instance string, err error) {
	logger.Warn("Command sent without a reply", "action", action, "target", target, "instance", instance, "error", err)
	fmt.Printf("%s sent for %s (the server did not confirm it)\n", action, target)
}

// newClient is a factory for creating Client instances. Tests may override
// this to inject a fake client.
var newClient = func(port int, password string) Client {
	return intrcon.New(port, password)
}

// newSystemd is a factory for creating a systemd helper. Tests may override this to inject fakes.
var newSystemd = func() *systemd.Systemd {
	return systemd.New()
}

// PlayersAction lists players for the given instance
func PlayersAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processPlayersForInstance(inst, newClient, total)
		})
	})
	return nil
}

// getRunningMap queries systemd and returns the raw list and a lookup map.
func getRunningMap() ([]string, map[string]bool) {
	sysd := newSystemd()
	runningList, _ := sysd.ListRunningInstances()
	running := make(map[string]bool)
	for _, r := range runningList {
		running[r] = true
	}
	return runningList, running
}

// precheckInstance returns (skip, stop). skip==true means caller should
// continue to next instance. stop==true means caller should return nil
// immediately (used when only a single instance was requested).
func precheckInstance(inst *config.Instance, running map[string]bool, totalInstances int, runningListLen int) (bool, bool) {
	if !inst.Enabled {
		msg := fmt.Sprintf("Instance %s is disabled; skipping RCON", inst.Name)
		if totalInstances == 1 {
			fmt.Println(msg)
			return false, true
		}
		fmt.Println(msg)
		return true, false
	}
	if runningListLen > 0 && !running[inst.Name] {
		msg := fmt.Sprintf("Instance %s is not running; skipping RCON", inst.Name)
		if totalInstances == 1 {
			fmt.Println(msg)
			return false, true
		}
		fmt.Println(msg)
		return true, false
	}
	return false, false
}

// processPlayersForInstance performs the Players query and prints results.
func processPlayersForInstance(inst *config.Instance, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	players, err := client.Players()
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON players failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	if totalInstances > 1 {
		fmt.Printf("Instance: %s\n", inst.Name)
	}
	if len(players) == 0 {
		fmt.Println("No players connected")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tIP\tPORT\tPING\tGUID\tNAME")
	for _, p := range players {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", p.ID, p.IP, p.Port, p.Ping, p.GUID, p.Name)
	}
	_ = tw.Flush()
	return nil
}

// SendAction sends a raw rcon command
func SendAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("command required")
		}
		cmd := strings.Join(args, " ")
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processSendForInstance(inst, cmd, newClient, total)
		})
	})
	return nil
}

// forEachInstance runs the provided action for each matching instance after
// performing the common pre-checks. The action receives the instance and the
// total number of instances requested.
func forEachInstance(instanceName string, action func(*config.Instance, int) error) error {
	instances, err := shared.GetInstances(instanceName)
	if err != nil {
		return err
	}
	runningList, running := getRunningMap()
	total := len(instances)

	for _, inst := range instances {
		skip, stop := precheckInstance(inst, running, total, len(runningList))
		if stop {
			return nil
		}
		if skip {
			continue
		}
		if err := action(inst, total); err != nil {
			return err
		}
	}
	return nil
}

// processSendForInstance sends a command via RCON and prints the response.
func processSendForInstance(inst *config.Instance, cmd string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	// If the operator sent a shutdown command, treat it like the higher-level
	// graceful shutdown flow: send the shutdown, wait for the unit to exit,
	// and then call systemd stop to clear any pending restart jobs.
	cmdTrim := strings.TrimSpace(cmd)
	lower := strings.ToLower(cmdTrim)
	if lower == "#shutdown" || lower == "shutdown" {
		_, err := client.Shutdown()
		if err != nil {
			if totalInstances > 1 {
				fmt.Printf("RCON shutdown failed for instance %s: %v\n", inst.Name, err)
				return nil
			}
			return err
		}
		logger.Info("Requested graceful shutdown", "instance", inst.Name)

		sysd := newSystemd()
		unit := "dayz@" + inst.Name
		// Wait up to 60s for the instance to exit on its own, polling every 2s.
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if !sysd.IsActive(unit) {
				if err := sysd.Stop(unit); err != nil {
					logger.Warn("Failed to stop unit after graceful exit", "name", inst.Name, "error", err)
				} else {
					logger.Info("Instance stopped gracefully", "name", inst.Name)
				}
				return nil
			}
			time.Sleep(2 * time.Second)
		}

		logger.Warn("Graceful shutdown timed out, forcing stop", "name", inst.Name)
		if err := sysd.Stop(unit); err != nil {
			if totalInstances > 1 {
				fmt.Printf("RCON send failed to stop instance %s: %v\n", inst.Name, err)
				return nil
			}
			return err
		}
		return nil
	}

	resp, err := client.Send(cmd)
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON send failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	if totalInstances > 1 {
		fmt.Printf("Instance: %s\n", inst.Name)
	}
	fmt.Println(resp)
	return nil
}

// KickAction kicks a player by id, GUID, IP or name
func KickAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("player id, GUID, IP or name required")
		}
		ref := strings.TrimSpace(args[0])
		if ref == "" {
			return fmt.Errorf("player id, GUID, IP or name required")
		}
		reason := ""
		if len(args) > 1 {
			reason = strings.Join(args[1:], " ")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processKickForInstance(inst, ref, reason, newClient, total)
		})
	})
	return nil
}

// processKickForInstance resolves the player and kicks them.
func processKickForInstance(inst *config.Instance, ref string, reason string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)

	err := withSession(client, func() error {
		player, err := resolvePlayerRef(client, ref)
		if err != nil {
			return err
		}

		if _, err := client.Kick(player.ID, reason); err != nil {
			if errors.Is(err, intrcon.ErrNoResponse) {
				sentWithoutReply("Kick", player.Name, inst.Name, err)
				return nil
			}
			return err
		}
		logger.Info("Kicked player", "player", player.Name, "id", player.ID, "instance", inst.Name)
		return nil
	})
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON kick failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	return nil
}

// BanAction bans a player by id, GUID, IP or name for minutes
func BanAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("player id, GUID, IP or name required")
		}
		ref := strings.TrimSpace(args[0])
		if ref == "" {
			return fmt.Errorf("player id, GUID, IP or name required")
		}
		minutes := 0
		if len(args) > 1 {
			minutes, _ = strconv.Atoi(args[1])
		}
		reason := ""
		if len(args) > 2 {
			reason = strings.Join(args[2:], " ")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processBanForInstance(inst, ref, minutes, reason, newClient, total)
		})
	})
	return nil
}

// processBanForInstance resolves the player and bans them.
func processBanForInstance(inst *config.Instance, ref string, minutes int, reason string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)

	err := withSession(client, func() error {
		player, err := resolvePlayerRef(client, ref)
		if err != nil {
			return err
		}

		if _, err := client.Ban(player.ID, minutes, reason); err != nil {
			if errors.Is(err, intrcon.ErrNoResponse) {
				sentWithoutReply("Ban", player.Name, inst.Name, err)
				return nil
			}
			return err
		}
		logger.Info("Banned player", "player", player.Name, "id", player.ID, "instance", inst.Name)
		return nil
	})
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON ban failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	return nil
}

// resolvePlayerRef turns an id, GUID, IP (with or without port) or name into
// the matching connected player. A number is tried as an id first, then as a
// GUID, so SteamIDs keep working.
func resolvePlayerRef(client Client, ref string) (intrcon.Player, error) {
	players, err := client.Players()
	if err != nil {
		return intrcon.Player{}, err
	}

	ref = strings.TrimSpace(ref)
	if id, err := strconv.Atoi(ref); err == nil {
		for _, p := range players {
			if p.ID == id {
				return p, nil
			}
		}
	}

	want := normalizePlayerName(ref)
	for _, p := range players {
		switch {
		case strings.EqualFold(strings.TrimSpace(p.GUID), ref):
			return p, nil
		case p.IP != "" && strings.EqualFold(p.IP, ref):
			return p, nil
		case p.IP != "" && p.Port != "" && strings.EqualFold(p.IP+":"+p.Port, ref):
			return p, nil
		case want != "" && normalizePlayerName(p.Name) == want:
			return p, nil
		}
	}

	if len(players) == 0 {
		return intrcon.Player{}, fmt.Errorf("no player matches %q (nobody is connected)", ref)
	}
	names := make([]string, 0, len(players))
	for _, p := range players {
		names = append(names, fmt.Sprintf("%s (#%d)", p.Name, p.ID))
	}
	return intrcon.Player{}, fmt.Errorf("no player matches %q (online: %s)", ref, strings.Join(names, ", "))
}

// normalizePlayerName trims and drops the quotes some builds put around the
// name column, so "Mira" and `"Mira"` compare equal.
func normalizePlayerName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return strings.ToLower(s)
}

// UnbanAction removes a ban by its ban-list id, or resolves a player GUID,
// IP or name to the matching entry first.
func UnbanAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("ban id, GUID, IP or name required")
		}
		ref := strings.TrimSpace(args[0])
		if ref == "" {
			return fmt.Errorf("ban id, GUID, IP or name required")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processUnbanForInstance(inst, ref, newClient, total)
		})
	})
	return nil
}

// processUnbanForInstance resolves the reference and removes that ban.
func processUnbanForInstance(inst *config.Instance, ref string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)

	// The web UI records GUID → player name when it places a ban; that is the
	// only way to reach the ban of a player who is no longer connected.
	var hints *banhints.Store
	if shared.Config != nil {
		hints = banhints.New(banhints.Path(shared.Config))
	}

	err := withSession(client, func() error {
		id, label, err := resolveBanID(client, ref, hints)
		if err != nil {
			return err
		}

		if _, err := client.Unban(id); err != nil {
			if errors.Is(err, intrcon.ErrNoResponse) {
				sentWithoutReply("Unban", label, inst.Name, err)
				return nil
			}
			return err
		}

		logger.Info("Unbanned player", "banId", id, "ref", ref, "instance", inst.Name)
		if totalInstances > 1 {
			fmt.Printf("Instance: %s\n", inst.Name)
		}
		fmt.Printf("Removed %s\n", label)
		return nil
	})
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON unban failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	return nil
}

// resolveBanID turns a ban-list id, GUID, IP or player name into the id
// `unban` takes. A name is resolved through the live player list (when the
// player is still connected) and through the GUID → name hints the web UI
// records when it places a ban.
func resolveBanID(client Client, ref string, hints *banhints.Store) (int, string, error) {
	// Ban ids are small indices; anything longer is a GUID, never an id.
	if len(ref) <= 6 && isDigits(ref) {
		id, err := strconv.Atoi(ref)
		if err == nil {
			return id, fmt.Sprintf("ban #%d", id), nil
		}
	}

	candidates := map[string]struct{}{}
	add := func(value string) {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			candidates[value] = struct{}{}
		}
	}
	add(ref)

	if players, err := client.Players(); err == nil {
		for _, p := range players {
			if playerMatchesRef(p, ref) {
				add(p.GUID)
				add(p.IP)
				if p.IP != "" && p.Port != "" {
					add(p.IP + ":" + p.Port)
				}
			}
		}
	}
	if hints != nil {
		for _, guid := range hints.GUIDSByName(ref) {
			add(guid)
		}
	}

	bans, err := client.Bans()
	if err != nil {
		return 0, "", fmt.Errorf("cannot read the ban list: %w", err)
	}

	for _, ban := range bans {
		if hasCandidate(candidates, ban.GUID) || hasCandidate(candidates, ban.IP) {
			return ban.ID, banResolvedLabel(ban, hints), nil
		}
	}

	if len(bans) == 0 {
		return 0, "", fmt.Errorf("no ban matches %q (the ban list is empty)", ref)
	}
	known := make([]string, 0, len(bans))
	for _, ban := range bans {
		known = append(known, banLabel(ban, hints))
	}
	return 0, "", fmt.Errorf("no ban matches %q; known bans: %s", ref, strings.Join(known, ", "))
}

// playerMatchesRef reports whether a connected player answers to a name, GUID
// or IP reference. Session ids are deliberately not matched here: a short
// number is a ban id in this command.
func playerMatchesRef(p intrcon.Player, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if p.GUID != "" && strings.EqualFold(strings.TrimSpace(p.GUID), ref) {
		return true
	}
	if p.IP != "" {
		if strings.EqualFold(p.IP, ref) {
			return true
		}
		if p.Port != "" && strings.EqualFold(p.IP+":"+p.Port, ref) {
			return true
		}
	}
	return normalizePlayerName(p.Name) == normalizePlayerName(ref)
}

func hasCandidate(set map[string]struct{}, value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return false
	}
	_, ok := set[value]
	return ok
}

// banTarget is the GUID, or the IP for an address ban.
func banTarget(ban intrcon.Ban) string {
	if ban.GUID != "" {
		return ban.GUID
	}
	return ban.IP
}

// banResolvedLabel names the entry the command is about to remove, including
// the player when the hints know it.
func banResolvedLabel(ban intrcon.Ban, hints *banhints.Store) string {
	label := fmt.Sprintf("ban #%d (%s", ban.ID, banTarget(ban))
	if hints != nil && ban.GUID != "" {
		if name := hints.Player(ban.GUID); name != "" {
			label += " — " + name
		}
	}
	return label + ")"
}

// banLabel is the human-readable form of a ban entry, with the player name
// when the hints know it.
func banLabel(ban intrcon.Ban, hints *banhints.Store) string {
	label := fmt.Sprintf("#%d %s", ban.ID, banTarget(ban))
	if hints != nil && ban.GUID != "" {
		if name := hints.Player(ban.GUID); name != "" {
			label += " (" + name + ")"
		}
	}
	return label
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// SayAction sends a global message
func SayAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("message required")
		}
		msg := strings.Join(args, " ")
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processSayForInstance(inst, msg, newClient, total)
		})
	})
	return nil
}

// processSayForInstance performs a Say via RCON for a single instance.
func processSayForInstance(inst *config.Instance, msg string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	_, err := client.Say(msg)
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON say failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	logger.Info("Sent message", "msg", msg, "instance", inst.Name)
	return nil
}

// ShutdownAction requests a graceful shutdown of the DayZ server process via
// the BattlEye "#shutdown" RCON command. The server saves state and exits
// cleanly; the systemd unit is left to report as stopped once the process
// exits (it is not stopped by systemctl here).
func ShutdownAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processShutdownForInstance(inst, newClient, total)
		})
	})
	return nil
}

// processShutdownForInstance sends a graceful #shutdown via RCON for a single instance.
func processShutdownForInstance(inst *config.Instance, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	_, err := client.Shutdown()
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON shutdown failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	logger.Info("Requested graceful shutdown", "instance", inst.Name)
	return nil
}
