package rcon

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/kabroxiko/dayzctl/internal/logger"
	intrcon "github.com/kabroxiko/dayzctl/internal/rcon"
	"github.com/kabroxiko/dayzctl/internal/systemd"
)

// Client is the subset of rcon client methods used by actions; defined
// as an interface to allow tests to inject fakes.
type Client interface {
	Players() ([]intrcon.Player, error)
	Send(command string) (string, error)
	Kick(id int, reason string) (string, error)
	Ban(id int, minutes int, reason string) (string, error)
	Say(msg string) (string, error)
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
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		sysd := newSystemd()
		runningList, _ := sysd.ListRunningInstances()
		running := make(map[string]bool)
		for _, r := range runningList {
			running[r] = true
		}

		for _, inst := range instances {
			if !inst.Enabled {
				msg := fmt.Sprintf("Instance %s is disabled; skipping RCON", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				fmt.Println(msg)
				continue
			}
			if len(runningList) > 0 && !running[inst.Name] {
				msg := fmt.Sprintf("Instance %s is not running; skipping RCON", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				fmt.Println(msg)
				continue
			}

			client := newClient(inst.RCON.Port, inst.RCON.Password)
			players, err := client.Players()
			if err != nil {
				if len(instances) > 1 {
					fmt.Printf("RCON players failed for instance %s: %v\n", inst.Name, err)
					continue
				}
				return err
			}
			if len(instances) > 1 {
				fmt.Printf("Instance: %s\n", inst.Name)
			}
			if len(players) == 0 {
				fmt.Println("No players connected")
				continue
			}
			for _, p := range players {
				fmt.Printf("%d %s %s %s %s %s\n", p.ID, p.IP, p.Port, p.Ping, p.GUID, p.Name)
			}
		}
		return nil
	})
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
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}
		cmd := strings.Join(args, " ")

		sysd := systemd.New()
		runningList, _ := sysd.ListRunningInstances()
		running := make(map[string]bool)
		for _, r := range runningList {
			running[r] = true
		}

		for _, inst := range instances {
			if !inst.Enabled {
				msg := fmt.Sprintf("Instance %s is disabled; skipping RCON", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				fmt.Println(msg)
				continue
			}
			if len(runningList) > 0 && !running[inst.Name] {
				msg := fmt.Sprintf("Instance %s is not running; skipping RCON", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				fmt.Println(msg)
				continue
			}

			client := newClient(inst.RCON.Port, inst.RCON.Password)
			resp, err := client.Send(cmd)
			if err != nil {
				if len(instances) > 1 {
					fmt.Printf("RCON send failed for instance %s: %v\n", inst.Name, err)
					continue
				}
				return err
			}
			if len(instances) > 1 {
				fmt.Printf("Instance: %s\n", inst.Name)
			}
			fmt.Println(resp)
		}
		return nil
	})
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
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		runningList, _ := sysd.ListRunningInstances()
		running := make(map[string]bool)
		for _, r := range runningList {
			running[r] = true
		}

		for _, inst := range instances {
			if !inst.Enabled {
				msg := fmt.Sprintf("instance %s is disabled; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}
			if len(runningList) > 0 && !running[inst.Name] {
				msg := fmt.Sprintf("instance %s is not running; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}

			client := newClient(inst.RCON.Port, inst.RCON.Password)
			_, err = client.Kick(id, reason)
			if err != nil {
				if len(instances) > 1 {
					fmt.Printf("RCON kick failed for instance %s: %v\n", inst.Name, err)
					continue
				}
				return err
			}
			logger.Info("Kicked player", "id", id, "instance", inst.Name)
		}
		return nil
	})
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
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		runningList, _ := sysd.ListRunningInstances()
		running := make(map[string]bool)
		for _, r := range runningList {
			running[r] = true
		}

		for _, inst := range instances {
			if !inst.Enabled {
				msg := fmt.Sprintf("instance %s is disabled; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}
			if len(runningList) > 0 && !running[inst.Name] {
				msg := fmt.Sprintf("instance %s is not running; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}

			client := newClient(inst.RCON.Port, inst.RCON.Password)
			_, err = client.Ban(id, minutes, reason)
			if err != nil {
				if len(instances) > 1 {
					fmt.Printf("RCON ban failed for instance %s: %v\n", inst.Name, err)
					continue
				}
				return err
			}
			logger.Info("Banned player", "id", id, "instance", inst.Name)
		}
		return nil
	})
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
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		runningList, _ := sysd.ListRunningInstances()
		running := make(map[string]bool)
		for _, r := range runningList {
			running[r] = true
		}

		for _, inst := range instances {
			if !inst.Enabled {
				msg := fmt.Sprintf("instance %s is disabled; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}
			if len(runningList) > 0 && !running[inst.Name] {
				msg := fmt.Sprintf("instance %s is not running; skipping rcon", inst.Name)
				if len(instances) == 1 {
					fmt.Println(msg)
					return nil
				}
				logger.Warn(msg)
				continue
			}

			client := newClient(inst.RCON.Port, inst.RCON.Password)
			_, err = client.Say(msg)
			if err != nil {
				if len(instances) > 1 {
					fmt.Printf("RCON say failed for instance %s: %v\n", inst.Name, err)
					continue
				}
				return err
			}
			logger.Info("Sent message", "msg", msg, "instance", inst.Name)
		}
		return nil
	})
	return nil
}
