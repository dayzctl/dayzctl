package mods

import (
	"fmt"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/config"
	intmods "github.com/dayzctl/dayzctl/internal/mods"
)

// ListAction lists installed mods for an instance
func ListAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if err := validateInstanceName(instanceName); err != nil {
			return err
		}

		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		modManager := newModManager()

		for _, inst := range instances {
			if len(instances) > 1 {
				fmt.Printf("Instance: %s\n", inst.Name)
			}

			if printed := printConfiguredMods(inst, modManager); printed {
				continue
			}

			if err := printInstalledMods(modManager); err != nil {
				return err
			}
		}
		return nil
	})
	return nil
}

func validateInstanceName(name string) error {
	if name == "" {
		return fmt.Errorf("instance name required")
	}
	return nil
}

func newModManager() *intmods.Manager {
	return intmods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())
}

// printConfiguredMods prints instance-configured mods. Returns true when any
// configured mods were printed (so caller can skip fallback behavior).
func printConfiguredMods(inst *config.Instance, modManager *intmods.Manager) bool {
	if len(inst.Mods) == 0 && len(inst.ServerMods) == 0 {
		return false
	}
	var shown int
	fmt.Println("Configured mods:")
	for _, mref := range inst.Mods {
		info, err := modManager.GetModInfo(mref.ID)
		name := mref.Name
		if name == "" && err == nil {
			name = info.Name
		}
		if name == "" {
			name = mref.ID
		}
		fmt.Printf("  ID: %-10s Name: %s\n", mref.ID, name)
		shown++
	}
	if shown == 0 {
		fmt.Println("No mods configured")
	}
	return true
}

func printInstalledMods(modManager *intmods.Manager) error {
	installed, err := modManager.ListInstalled()
	if err != nil {
		return err
	}
	if len(installed) == 0 {
		fmt.Println("No mods installed")
		return nil
	}
	fmt.Printf("Installed mods (%d):\n", len(installed))
	for _, mod := range installed {
		fmt.Printf("  ID: %-10s Name: %s\n", mod.ID, mod.Name)
	}
	return nil
}

// AddAction adds client mods to an instance
func AddAction(instanceName string, modIDs []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		for _, instance := range instances {
			toAdd := computeToAdd(instance, modIDs, false)
			if len(toAdd) == 0 {
				fmt.Printf("No new mods to add for instance %s\n", instance.Name)
				continue
			}

			if err := processAdd(instance, toAdd, false); err != nil {
				fmt.Printf("Failed to add mods to instance %s: %v\n", instance.Name, err)
			}
		}

		return nil
	})
	return nil
}

// computeToAdd returns the subset of modIDs that are not already present
// as client or server mods (depending on isServer flag)
func computeToAdd(instance *config.Instance, modIDs []string, isServer bool) []string {
	var out []string
	for _, modID := range modIDs {
		if isServer {
			if hasServerMod(instance, modID) {
				fmt.Printf("Mod %s already present in server mods for instance %s\n", modID, instance.Name)
				continue
			}
			if hasClientMod(instance, modID) {
				fmt.Printf("Mod %s already present as client mod; not adding to server mods\n", modID)
				continue
			}
			out = append(out, modID)
			continue
		}

		// client mod path
		if hasClientMod(instance, modID) {
			fmt.Printf("Mod %s already present in client mods for instance %s\n", modID, instance.Name)
			continue
		}
		if hasServerMod(instance, modID) {
			fmt.Printf("Mod %s already present as server mod; not adding to client mods\n", modID)
			continue
		}
		out = append(out, modID)
	}
	return out
}

func hasClientMod(instance *config.Instance, modID string) bool {
	for _, m := range instance.Mods {
		if m.ID == modID {
			return true
		}
	}
	return false
}

func hasServerMod(instance *config.Instance, modID string) bool {
	for _, m := range instance.ServerMods {
		if m.ID == modID {
			return true
		}
	}
	return false
}

// processAdd downloads and adds the mods to the instance. Returns first error encountered.
func processAdd(instance *config.Instance, toAdd []string, isServer bool) error {
	fmt.Printf("Adding %d mod(s) to instance %s\n", len(toAdd), instance.Name)
	var firstErr error
	for _, modID := range toAdd {
		if err := AddModToInstance(instance, modID, isServer); err != nil {
			fmt.Printf("Failed to add mod %s: %v\n", modID, err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// AddServerAction adds server-side mods
func AddServerAction(instanceName string, modIDs []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		for _, instance := range instances {
			toAdd := computeToAdd(instance, modIDs, true)
			if len(toAdd) == 0 {
				fmt.Printf("No new server mods to add for instance %s\n", instance.Name)
				continue
			}

			if err := processAdd(instance, toAdd, true); err != nil {
				fmt.Printf("Failed to add server mods to instance %s: %v\n", instance.Name, err)
			}
		}

		return nil
	})
	return nil
}

// RemoveAction removes mods from an instance (config only)
func RemoveAction(instanceName string, modIDs []string) error {
	// reuse runRemoveMods
	return runRemoveMods(instanceName, modIDs, false)
}

// DeleteAction deletes mods from an instance, optionally deleting files
func DeleteAction(instanceName string, modIDs []string, deleteFiles bool) error {
	return runDeleteMods(instanceName, modIDs, deleteFiles)
}

// SyncAction syncs mods for an instance
func SyncAction(instanceName string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		modManager := intmods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())

		for _, instance := range instances {
			if err := processSync(instance, modManager); err != nil {
				fmt.Printf("Failed to sync mods for instance %s: %v\n", instance.Name, err)
				continue
			}
		}
		return nil
	})
	return nil
}

// processSync performs the sync steps for a single instance.
// It returns an error when the primary sync operation fails; secondary
// steps (config update / apply) are best-effort and only logged.
func processSync(instance *config.Instance, modManager *intmods.Manager) error {
	fmt.Printf("Syncing mods for instance %s\n", instance.Name)

	if len(instance.Mods) == 0 && len(instance.ServerMods) == 0 {
		fmt.Printf("No mods configured for instance %s\n", instance.Name)
		return nil
	}

	if err := modManager.SyncMods(instance.Mods, instance.ServerMods); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if err := shared.UpdateServerConfig(instance); err != nil {
		fmt.Printf("Failed to update server config: %v\n", err)
	}

	if err := shared.ApplyConfig(); err != nil {
		fmt.Printf("Failed to apply config changes: %v\n", err)
	}

	fmt.Printf("Mods synced successfully for instance %s (total=%d)\n", instance.Name, len(instance.Mods)+len(instance.ServerMods))
	return nil
}
