package mods

import (
	"fmt"

	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/shared"
	intmods "github.com/kabroxiko/dayzctl/internal/mods"
)

// ListAction lists installed mods for an instance
func ListAction(instanceName string, args []string) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		modManager := intmods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())

		for _, inst := range instances {
			if len(instances) > 1 {
				fmt.Printf("Instance: %s\n", inst.Name)
			}

			// If mods are configured in the instance, prefer to show those
			if len(inst.Mods) > 0 || len(inst.ServerMods) > 0 {
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
				// continue to next instance
				continue
			}

			// Fallback to scanning workshop directory for installed mods
			installed, err := modManager.ListInstalled()
			if err != nil {
				return err
			}
			if len(installed) == 0 {
				fmt.Println("No mods installed")
				continue
			}
			fmt.Printf("Installed mods (%d):\n", len(installed))
			for _, mod := range installed {
				fmt.Printf("  ID: %-10s Name: %s\n", mod.ID, mod.Name)
			}
		}
		return nil
	})
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
			var toAdd []string
			for _, modID := range modIDs {
				found := false
				for _, m := range instance.Mods {
					if m.ID == modID {
						fmt.Printf("Mod %s already present in client mods for instance %s\n", modID, instance.Name)
						found = true
						break
					}
				}
				if !found {
					for _, m := range instance.ServerMods {
						if m.ID == modID {
							fmt.Printf("Mod %s already present as server mod; not adding to client mods\n", modID)
							found = true
							break
						}
					}
				}
				if !found {
					toAdd = append(toAdd, modID)
				}
			}

			if len(toAdd) == 0 {
				fmt.Printf("No new mods to add for instance %s\n", instance.Name)
				continue
			}

			fmt.Printf("Adding %d mod(s) to instance %s\n", len(toAdd), instance.Name)

			for _, modID := range toAdd {
				if err := AddModToInstance(instance, modID, false); err != nil {
					fmt.Printf("Failed to add mod %s: %v\n", modID, err)
				}
			}
		}

		return nil
	})
	return nil
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
			var toAdd []string
			for _, modID := range modIDs {
				found := false
				for _, m := range instance.ServerMods {
					if m.ID == modID {
						fmt.Printf("Mod %s already present in server mods for instance %s\n", modID, instance.Name)
						found = true
						break
					}
				}
				if !found {
					for _, m := range instance.Mods {
						if m.ID == modID {
							fmt.Printf("Mod %s already present as client mod; not adding to server mods\n", modID)
							found = true
							break
						}
					}
				}
				if !found {
					toAdd = append(toAdd, modID)
				}
			}

			if len(toAdd) == 0 {
				fmt.Printf("No new server mods to add for instance %s\n", instance.Name)
				continue
			}

			fmt.Printf("Adding %d server mod(s) to instance %s\n", len(toAdd), instance.Name)

			for _, modID := range toAdd {
				if err := AddModToInstance(instance, modID, true); err != nil {
					fmt.Printf("Failed to add server mod %s: %v\n", modID, err)
				}
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
			fmt.Printf("Syncing mods for instance %s\n", instance.Name)

			if len(instance.Mods) == 0 && len(instance.ServerMods) == 0 {
				fmt.Printf("No mods configured for instance %s\n", instance.Name)
				continue
			}

			if err := modManager.SyncMods(instance.Mods, instance.ServerMods); err != nil {
				fmt.Printf("Failed to sync mods for instance %s: %v\n", instance.Name, err)
				continue
			}

			if err := shared.UpdateServerConfig(instance); err != nil {
				fmt.Printf("Failed to update server config: %v\n", err)
			}

			if err := shared.ApplyConfig(); err != nil {
				fmt.Printf("Failed to apply config changes: %v\n", err)
			}

			fmt.Printf("Mods synced successfully for instance %s (total=%d)\n", instance.Name, len(instance.Mods)+len(instance.ServerMods))
		}
		return nil
	})
	return nil
}
