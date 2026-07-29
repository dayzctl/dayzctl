package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/generate"
	"github.com/dayzctl/dayzctl/internal/lock"
	"github.com/dayzctl/dayzctl/internal/logger"
	"github.com/dayzctl/dayzctl/internal/mods"
	"github.com/dayzctl/dayzctl/internal/steamcmd"
	"github.com/dayzctl/dayzctl/internal/systemd"
	"github.com/dayzctl/dayzctl/internal/version"
)

// StartAction starts an instance or all
func StartAction(target string) error {
	shared.RunCommand(func() error {
		instances, err := shared.GetInstances(target)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		for _, instance := range instances {
			if err := sysd.Start("dayz@" + instance.Name); err != nil {
				logger.Warn("Failed to start instance", "name", instance.Name, "error", err)
				continue
			}
			logger.Info("Started instance", "name", instance.Name)
		}
		return nil
	})
	return nil
}

// StopAction stops an instance or all
func StopAction(target string) error {
	shared.RunCommand(func() error {
		instances, err := shared.GetInstances(target)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		for _, instance := range instances {
			if err := sysd.Stop("dayz@" + instance.Name); err != nil {
				logger.Warn("Failed to stop instance", "name", instance.Name, "error", err)
				continue
			}
			logger.Info("Stopped instance", "name", instance.Name)
		}
		return nil
	})
	return nil
}

// RestartAction restarts an instance or all
func RestartAction(target string) error {
	shared.RunCommand(func() error {
		instances, err := shared.GetInstances(target)
		if err != nil {
			return err
		}

		sysd := systemd.New()
		for _, instance := range instances {
			if err := sysd.Restart("dayz@" + instance.Name); err != nil {
				logger.Warn("Failed to restart instance", "name", instance.Name, "error", err)
				continue
			}
			logger.Info("Restarted instance", "name", instance.Name)
		}
		return nil
	})
	return nil
}

// StatusAction shows status
func StatusAction(arg string) error {
	shared.RunCommand(func() error {
		sysd := systemd.New()
		if arg == "" || arg == "all" {
			return printAllStatus(sysd)
		}
		instance, err := shared.GetInstance(arg)
		if err != nil {
			return err
		}
		return printSingleStatus(sysd, instance)
	})
	return nil
}

// printAllStatus prints the overall server status, enabled instances, and timers.
func printAllStatus(sysd *systemd.Systemd) error {
	fmt.Print("\n=== DayZ Server Status ===\n\n")

	fmt.Printf("Configured instances: %v\n", shared.Config.GetInstanceNames())

	running, _ := sysd.ListRunningInstances()
	fmt.Printf("Running instances: %v\n", running)

	for _, inst := range shared.Config.Instances {
		if !inst.Enabled {
			continue
		}
		printInstanceSummary(inst, running)
	}

	fmt.Print("\n=== Timers ===\n")
	if err := printTimers(sysd); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// printInstanceSummary prints a short status summary for a single instance.
func printInstanceSummary(inst config.Instance, running []string) {
	isRunning := false
	for _, r := range running {
		if r == inst.Name {
			isRunning = true
			break
		}
	}
	status := "stopped"
	if isRunning {
		status = "running"
	}
	fmt.Printf("\nInstance: %s\n", inst.Name)
	fmt.Printf("  Port: %d\n", inst.Port)
	fmt.Printf("  Status: %s\n", status)
	fmt.Printf("  Mods: %d\n", len(inst.Mods))
	if inst.RCON.Enabled {
		fmt.Printf("  RCON: enabled on port %d\n", inst.RCON.Port)
	}
}

// printTimers prints selected systemd timer statuses.
func printTimers(sysd *systemd.Systemd) error {
	for _, timer := range []string{"dayz-update.timer", "dayz-prune.timer"} {
		status, err := sysd.Status(timer)
		if err != nil {
			// Non-fatal: continue to next timer
			continue
		}
		for _, line := range strings.Split(status, "\n") {
			if strings.Contains(line, "Active:") || strings.Contains(line, "Trigger:") {
				fmt.Println("  " + strings.TrimSpace(line))
			}
		}
	}
	return nil
}

// printSingleStatus prints the full systemd status for a single instance unit.
func printSingleStatus(sysd *systemd.Systemd, inst *config.Instance) error {
	status, err := sysd.Status("dayz@" + inst.Name)
	if err != nil {
		return err
	}
	fmt.Println(status)
	return nil
}

// ApplyAction applies config
func ApplyAction(target string) error {
	shared.RunCommand(func() error {
		return applyOrchestrator(target)
	})
	return nil
}

// applyOrchestrator performs the high-level apply steps by composing smaller helpers.
func applyOrchestrator(target string) error {
	instances, err := collectApplyInstances(target)
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		return fmt.Errorf("no instances selected for apply")
	}

	fmt.Println("Generating server and BattlEye configs...")
	if err := generate.GenerateForInstances(shared.Config, instances); err != nil {
		return fmt.Errorf("failed to generate server configs: %w", err)
	}

	sysd := systemd.New()
	if err := generateAndReloadSystemd(sysd); err != nil {
		return err
	}

	enableInstances(sysd, instances)

	if shared.Config.Updates.Enabled {
		enableUpdateTimer(sysd)
	}

	fmt.Println("Configuration applied successfully")
	fmt.Println("Services are NOT started/stopped/restarted. Use 'dayzctl start/stop/restart' to control services.")
	return nil
}

// collectApplyInstances returns the set of instances to operate on based on target.
func collectApplyInstances(target string) ([]*config.Instance, error) {
	if target == "" || target == "all" {
		enabled := shared.Config.GetEnabledInstances()
		instances := make([]*config.Instance, len(enabled))
		for i := range enabled {
			instances[i] = &enabled[i]
		}
		return instances, nil
	}
	return shared.GetInstances(target)
}

// generateAndReloadSystemd generates systemd unit files and reloads systemd.
func generateAndReloadSystemd(sysd *systemd.Systemd) error {
	fmt.Println("Generating systemd units...")
	if err := sysd.GenerateUnits(shared.Config); err != nil {
		return fmt.Errorf("failed to generate units: %w", err)
	}
	if err := sysd.Reload(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}
	return nil
}

// enableInstances enables systemd services for enabled instances.
func enableInstances(sysd *systemd.Systemd, instances []*config.Instance) {
	for _, instance := range instances {
		if instance.Enabled {
			fmt.Printf("Enabling instance %s\n", instance.Name)
			if err := sysd.Enable("dayz@" + instance.Name); err != nil {
				fmt.Printf("Failed to enable instance %s: %v\n", instance.Name, err)
			}
		}
	}
}

// enableUpdateTimer enables the periodic update timer.
func enableUpdateTimer(sysd *systemd.Systemd) {
	fmt.Println("Enabling update timer...")
	if err := sysd.Enable("dayz-update.timer"); err != nil {
		fmt.Printf("Failed to enable update timer: %v\n", err)
	}
}

// UpdateAction runs update
func UpdateAction() error {
	shared.RunCommand(func() error {
		return updateOrchestrator()
	})
	return nil
}

// updateOrchestrator performs the update flow previously in UpdateAction
// but broken out to improve readability and testability.
func updateOrchestrator() error {
	// Acquire lock
	l, err := acquireUpdateLock()
	if err != nil {
		return err
	}
	defer func() {
		if err := l.Release(); err != nil {
			logger.Warn("Failed to release lock", "error", err)
		}
	}()

	if !checkUpdatesEnabled() {
		logger.Info("Updates are disabled in config")
		return nil
	}

	steam, err := ensureSteamcmd()
	if err != nil {
		return err
	}

	buildID, err := getBuildID(steam)
	if err != nil {
		return err
	}
	logger.Info("Current build", "build_id", buildID)

	needs, err := checkNeedsUpdate(steam)
	if err != nil {
		return err
	}
	if !needs {
		logger.Info("Server is already up to date - no update available")
		return nil
	}

	logger.Info("Update available! Proceeding with update...")

	sysd := systemd.New()
	running, _ := listRunningInstances(sysd)

	if err := stopRunningInstances(sysd, running); err != nil {
		return err
	}

	if err := performUpdate(steam); err != nil {
		return err
	}

	syncModsForAll()

	if err := restartInstances(sysd, running); err != nil {
		return err
	}

	logger.Info("Update process completed successfully")
	return nil
}

func acquireUpdateLock() (*lock.Lock, error) {
	l, err := lock.New("/run/dayzctl.lock")
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	return l, nil
}

func checkUpdatesEnabled() bool {
	return shared.Config.Updates.Enabled
}

func ensureSteamcmd() (*steamcmd.SteamCmd, error) {
	if shared.Config.GetSteamcmdBin() == "" {
		return nil, fmt.Errorf("steamcmd path not configured; set 'paths.steamcmd_bin' in the config or install SteamCMD via the installer")
	}
	return steamcmd.New(shared.Config.GetSteamUser(), shared.Config.GetInstallDir(), shared.Config.GetSteamcmdBin(), shared.Config.GetWorkshopDir()), nil
}

func getBuildID(steam *steamcmd.SteamCmd) (string, error) {
	buildID, err := steam.GetBuildID()
	if err != nil {
		if steamcmd.IsRateLimitError(err) {
			logger.Warn("Rate limit hit, please wait before retrying")
			return "", err
		}
		return "", fmt.Errorf("failed to check build: %w", err)
	}
	return buildID, nil
}

func checkNeedsUpdate(steam *steamcmd.SteamCmd) (bool, error) {
	needsUpdate, err := steam.NeedsUpdate()
	if err != nil {
		return false, fmt.Errorf("failed to check update status: %w", err)
	}
	return needsUpdate, nil
}

func listRunningInstances(sysd *systemd.Systemd) ([]string, error) {
	instances, err := sysd.ListRunningInstances()
	if err != nil {
		logger.Warn("Failed to list running instances", "error", err)
		return []string{}, nil
	}
	return instances, nil
}

func stopRunningInstances(sysd *systemd.Systemd, instances []string) error {
	if len(instances) == 0 {
		logger.Info("No running instances to stop")
		return nil
	}
	logger.Info("Stopping running instances...")
	for _, instance := range instances {
		logger.Info("Stopping instance", "name", instance)
		if err := sysd.Stop("dayz@" + instance); err != nil {
			return fmt.Errorf("failed to stop %s: %w", instance, err)
		}
	}
	return nil
}

func performUpdate(steam *steamcmd.SteamCmd) error {
	logger.Info("Updating DayZ server as dayz user...")
	if err := steam.Update(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	logger.Info("Update completed successfully")
	return nil
}

func syncModsForAll() {
	modManager := mods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())
	for _, instance := range shared.Config.Instances {
		if instance.Enabled {
			allMods := append(instance.Mods, instance.ServerMods...)
			if len(allMods) > 0 {
				logger.Info("Syncing mods for instance", "name", instance.Name, "count", len(allMods))
				if err := modManager.SyncMods(allMods, instance.ServerMods); err != nil {
					logger.Warn("Failed to sync mods", "instance", instance.Name, "error", err)
				}
			}
		}
	}
}

func restartInstances(sysd *systemd.Systemd, instances []string) error {
	if len(instances) == 0 {
		logger.Info("No instances to restart (were not running before update)")
		return nil
	}
	logger.Info("Restarting previously running instances...")
	for _, instance := range instances {
		logger.Info("Starting instance", "name", instance)
		if err := sysd.Start("dayz@" + instance); err != nil {
			return fmt.Errorf("failed to start %s: %w", instance, err)
		}
	}
	return nil
}

// VersionAction prints version information
func VersionAction() error {
	fmt.Printf("dayzctl version %s (built %s)\n", version.Version, version.BuildTime)
	return nil
}

// ValidateConfigAction logs config details
func ValidateConfigAction() error {
	logger.Info("Config loaded successfully")
	logger.Info("Steam user", "user", shared.Config.GetSteamUser())
	logger.Info("Base directory", "path", shared.Config.GetBaseDir())
	logger.Info("Install directory", "path", shared.Config.GetInstallDir())
	logger.Info("Instances", "count", len(shared.Config.Instances))
	for _, inst := range shared.Config.Instances {
		logger.Info("Instance",
			"name", inst.Name,
			"port", inst.Port,
			"enabled", inst.Enabled,
			"mods", len(inst.Mods),
			"servermods", len(inst.ServerMods))
	}
	return nil
}

// RenderConfigAction renders server config for an instance (dry-run)
func RenderConfigAction(instanceName string) error {
	shared.RunCommand(func() error {
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		for _, instance := range instances {
			logger.Info("Rendering config for instance", "name", instance.Name)
			configPath := fmt.Sprintf("%s/serverDZ-%s.cfg", shared.Config.GetInstallDir(), instance.Name)
			logger.Info("Config would be written to", "path", configPath)
		}

		return nil
	})
	return nil
}

// CheckSpaceAction checks disk space for install dir
func CheckSpaceAction() error {
	shared.RunCommand(func() error {
		installDir := shared.Config.GetInstallDir()

		// Try preferred `df -BG`, but fall back to `df -h` or plain `df` for portability
		var output []byte
		var err error
		tryArgs := [][]string{{"-BG"}, {"-h"}, {}}
		for _, args := range tryArgs {
			cmdArgs := append(args, installDir)
			execCmd := exec.Command("df", cmdArgs...)
			output, err = execCmd.CombinedOutput()
			if err == nil {
				break
			}
		}
		if err != nil {
			return err
		}

		lines := strings.Split(string(output), "\n")
		if len(lines) < 2 {
			return fmt.Errorf("unexpected df output")
		}

		fields := strings.Fields(lines[1])
		if len(fields) < 4 {
			return fmt.Errorf("unexpected df output format")
		}

		available := fields[3]
		usePercent := fields[4]

		fmt.Printf("Disk usage for %s:\n", installDir)
		fmt.Printf("  Available: %s\n", available)
		fmt.Printf("  Used: %s\n", usePercent)

		return nil
	})
	return nil
}

// SteamLoginAction performs interactive steam login
func SteamLoginAction() error {
	shared.RunCommand(func() error {
		if shared.Config.GetSteamcmdBin() == "" {
			return fmt.Errorf("steamcmd path not configured; set 'paths.steamcmd_bin' in the config or install SteamCMD via the installer")
		}
		steam := steamcmd.New(shared.Config.GetSteamUser(), shared.Config.GetInstallDir(), shared.Config.GetSteamcmdBin(), shared.Config.GetWorkshopDir())
		return steam.InteractiveLogin()
	})
	return nil
}

// ListInstancesAction lists configured instances
func ListInstancesAction() error {
	fmt.Printf("Configured instances:\n")
	for _, inst := range shared.Config.Instances {
		status := "disabled"
		if inst.Enabled {
			status = "enabled"
		}
		fmt.Printf("  %s: port=%d, mods=%d, status=%s\n", inst.Name, inst.Port, len(inst.Mods), status)
	}
	return nil
}
