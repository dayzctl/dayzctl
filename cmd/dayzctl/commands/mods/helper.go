package mods

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/kabroxiko/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/mods"
	"github.com/kabroxiko/dayzctl/internal/steamcmd"
	"github.com/kabroxiko/dayzctl/internal/utils"
)

func AddModToInstance(instance *config.Instance, modID string, isServerMod bool) error {
	modManager := mods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())

	fmt.Printf("Downloading mod %s\n", modID)
	if shared.Config.GetSteamcmdBin() == "" {
		return fmt.Errorf("steamcmd path not configured; set 'paths.steamcmd_bin' in the config or install SteamCMD via the installer")
	}
	steam := steamcmd.New(shared.Config.GetSteamUser(), shared.Config.GetInstallDir(), shared.Config.GetSteamcmdBin(), shared.Config.GetWorkshopDir())
	if err := steam.DownloadMod(modID); err != nil {
		return fmt.Errorf("failed to download mod %s: %w", modID, err)
	}

	modInfo, err := modManager.GetModInfo(modID)
	if err != nil {
		fmt.Printf("Failed to get mod info for %s, using ID as name: %v\n", modID, err)
		modInfo = mods.Mod{ID: modID, Name: modID}
		modInfo.Path = filepath.Join(shared.Config.GetWorkshopDir(), modID)
	}

	// If the mod contains Keys/, copy them into the server keys directory
	srcKeys := filepath.Join(modInfo.Path, "Keys")
	if fi, err := os.Stat(srcKeys); err == nil && fi.IsDir() {
		destKeys := filepath.Join(shared.Config.GetInstallDir(), "keys")
		if err := utils.EnsureDir(destKeys, 0755); err != nil {
			fmt.Printf("Failed to ensure server keys dir %s: %v\n", destKeys, err)
		} else {
			entries, err := os.ReadDir(srcKeys)
			if err != nil {
				fmt.Printf("Failed to read Keys dir %s: %v\n", srcKeys, err)
			} else {
				for _, e := range entries {
					if e.IsDir() {
						continue
					}
					srcFile := filepath.Join(srcKeys, e.Name())
					destFile := filepath.Join(destKeys, e.Name())
					in, err := os.Open(srcFile)
					if err != nil {
						fmt.Printf("Failed to open key file %s: %v\n", srcFile, err)
						continue
					}
					out, err := os.Create(destFile)
					if err != nil {
						if cerr := in.Close(); cerr != nil {
							fmt.Printf("Failed to close source key file %s after dest create error: %v\n", srcFile, cerr)
						}
						fmt.Printf("Failed to create dest key %s: %v\n", destFile, err)
						continue
					}
					if _, err := io.Copy(out, in); err != nil {
						fmt.Printf("Failed to copy key %s -> %s: %v\n", srcFile, destFile, err)
					}
					if cerr := in.Close(); cerr != nil {
						fmt.Printf("Failed to close source key file %s: %v\n", srcFile, cerr)
					}
					if cerr := out.Close(); cerr != nil {
						fmt.Printf("Failed to close dest key file %s: %v\n", destFile, cerr)
					}
					_ = utils.ChownPath(destFile)
				}
			}
		}
	}

	if isServerMod {
		instance.ServerMods = append(instance.ServerMods, config.ModRef{
			ID:   modID,
			Name: modInfo.Name,
		})
		fmt.Printf("Adding server mod %s (%s) to instance %s\n", modID, modInfo.Name, instance.Name)
	} else {
		instance.Mods = append(instance.Mods, config.ModRef{
			ID:   modID,
			Name: modInfo.Name,
		})
		fmt.Printf("Adding client mod %s (%s) to instance %s\n", modID, modInfo.Name, instance.Name)
	}

	if err := shared.SaveConfig(); err != nil {
		fmt.Printf("Failed to save config: %v\n", err)
	}

	if err := modManager.SyncMods(instance.Mods, instance.ServerMods); err != nil {
		fmt.Printf("Failed to sync mods: %v\n", err)
	}

	if err := shared.UpdateServerConfig(instance); err != nil {
		fmt.Printf("Failed to update server config: %v\n", err)
	}

	if err := shared.ApplyConfig(); err != nil {
		fmt.Printf("Failed to apply config changes: %v\n", err)
	}

	modType := "client"
	if isServerMod {
		modType = "server"
	}
	fmt.Printf("Added %s mod %s (%s) to instance %s\n", modType, modID, modInfo.Name, instance.Name)
	return nil
}

// runRemoveMods removes mod references from the instance configuration and optionally deletes files.
func runRemoveMods(instanceName string, modIDs []string, deleteFiles bool) error {
	shared.RunCommand(func() error {
		if instanceName == "" {
			return fmt.Errorf("instance name required")
		}
		instances, err := shared.GetInstances(instanceName)
		if err != nil {
			return err
		}

		modManager := mods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())

		// helper to remove from slice
		removeFrom := func(slice []config.ModRef, id string) ([]config.ModRef, bool) {
			var out []config.ModRef
			removed := false
			for _, m := range slice {
				if m.ID == id {
					removed = true
					continue
				}
				out = append(out, m)
			}
			return out, removed
		}

		for _, instance := range instances {
			for _, id := range modIDs {
				var removedAny bool
				instance.Mods, removedAny = removeFrom(instance.Mods, id)
				if removedAny {
					// remove symlink
					_ = modManager.RemoveMod(config.ModRef{ID: id}, false)
					if deleteFiles {
						// remove workshop files
						_ = os.RemoveAll(modManager.GetModPath(config.ModRef{ID: id}, false))
					}
				}
				instance.ServerMods, removedAny = removeFrom(instance.ServerMods, id)
				if removedAny {
					_ = modManager.RemoveMod(config.ModRef{ID: id}, true)
					if deleteFiles {
						_ = os.RemoveAll(modManager.GetModPath(config.ModRef{ID: id}, true))
					}
				}
			}
		}

		if err := shared.SaveConfig(); err != nil {
			return err
		}

		return nil
	})
	return nil
}

// runDeleteMods is an alias to runRemoveMods for now (keeps semantic clarity)
func runDeleteMods(instanceName string, modIDs []string, deleteFiles bool) error {
	return runRemoveMods(instanceName, modIDs, deleteFiles)
}
