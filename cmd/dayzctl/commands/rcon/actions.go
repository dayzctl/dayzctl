package rcon

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
	intrcon "github.com/dayzctl/dayzctl/internal/rcon"
	"github.com/dayzctl/dayzctl/internal/systemd"
)

// Client is the subset of rcon client methods used by actions; defined
// as an interface to allow tests to inject fakes.
type Client interface {
	Players() ([]intrcon.Player, error)
	Send(command string) (string, error)
	Kick(id int, reason string) (string, error)
	Ban(id int, minutes int, reason string) (string, error)
	Say(msg string) (string, error)
	Shutdown() (string, error)
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
	for _, p := range players {
		fmt.Printf("%d %s %s %s %s %s\n", p.ID, p.IP, p.Port, p.Ping, p.GUID, p.Name)
	}
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

// KickAction kicks a player by ID
func KickAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("player id required")
		}
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid player id: %w", err)
		}
		reason := ""
		if len(args) > 1 {
			reason = strings.Join(args[1:], " ")
		}
		return forEachInstance(instanceName, func(inst *config.Instance, total int) error {
			return processKickForInstance(inst, id, reason, newClient, total)
		})
	})
	return nil
}

// processKickForInstance performs a kick via RCON for a single instance.
func processKickForInstance(inst *config.Instance, id int, reason string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	_, err := client.Kick(id, reason)
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON kick failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	logger.Info("Kicked player", "id", id, "instance", inst.Name)
	return nil
}

// BanAction bans a player by ID for minutes
func BanAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		if len(args) == 0 {
			return fmt.Errorf("player id required")
		}
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid player id: %w", err)
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
			return processBanForInstance(inst, id, minutes, reason, newClient, total)
		})
	})
	return nil
}

// processBanForInstance performs a ban via RCON for a single instance.
func processBanForInstance(inst *config.Instance, id int, minutes int, reason string, clientFactory func(int, string) Client, totalInstances int) error {
	client := clientFactory(inst.RCON.Port, inst.RCON.Password)
	_, err := client.Ban(id, minutes, reason)
	if err != nil {
		if totalInstances > 1 {
			fmt.Printf("RCON ban failed for instance %s: %v\n", inst.Name, err)
			return nil
		}
		return err
	}
	logger.Info("Banned player", "id", id, "instance", inst.Name)
	return nil
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
