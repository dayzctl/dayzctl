package mods

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dayzctl/dayzctl/cmd/dayzctl/commands/shared"
	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/mods"
	"github.com/dayzctl/dayzctl/internal/steamcmd"
	"github.com/dayzctl/dayzctl/internal/utils"
)

func AddModToInstance(instance *config.Instance, modID string, isServerMod bool) error {
	modManager := mods.New(shared.Config.GetInstallDir(), shared.Config.GetWorkshopDir())

	// download and get metadata
	modInfo, err := downloadModInfo(modManager, modID)
	if err != nil {
		return err
	}

	// copy any Keys/ into server keys dir
	if err := copyModKeys(modInfo.Path); err != nil {
		fmt.Printf("Warning: failed to copy keys for mod %s: %v\n", modID, err)
	}

	// append to instance config
	appendModRef(instance, modID, modInfo.Name, isServerMod)

	// finalize: save config, sync and apply
	if err := finalizeAdd(modManager, instance); err != nil {
		fmt.Printf("Failed to finalize add for mod %s: %v\n", modID, err)
		return err
	}

	modType := "client"
	if isServerMod {
		modType = "server"
	}
	fmt.Printf("Added %s mod %s (%s) to instance %s\n", modType, modID, modInfo.Name, instance.Name)
	return nil
}

// downloadModInfo ensures the mod is downloaded and returns a Mod info object.
func downloadModInfo(modManager *mods.Manager, modID string) (mods.Mod, error) {
	fmt.Printf("Downloading mod %s\n", modID)
	if shared.Config.GetSteamcmdBin() == "" {
		return mods.Mod{}, fmt.Errorf("steamcmd path not configured; set 'paths.steamcmd_bin' in the config or install SteamCMD via the installer")
	}
	steam := steamcmd.New(shared.Config.GetSteamUser(), shared.Config.GetInstallDir(), shared.Config.GetSteamcmdBin(), shared.Config.GetWorkshopDir())
	if err := steam.DownloadMod(modID); err != nil {
		return mods.Mod{}, fmt.Errorf("failed to download mod %s: %w", modID, err)
	}

	modInfo, err := modManager.GetModInfo(modID)
	if err != nil {
		fmt.Printf("Failed to get mod info for %s, using ID as name: %v\n", modID, err)
		modInfo = mods.Mod{ID: modID, Name: modID}
		modInfo.Path = filepath.Join(shared.Config.GetWorkshopDir(), modID)
	}
	return modInfo, nil
}

// copyModKeys copies any files from modPath/Keys into the server keys directory.
// Returns an error only if the operation cannot be initiated; file-level errors
// are logged and ignored.
func copyModKeys(modPath string) error {
	srcKeys := filepath.Join(modPath, "Keys")
	fi, err := os.Stat(srcKeys)
	if err != nil || !fi.IsDir() {
		return nil
	}

	destKeys := filepath.Join(shared.Config.GetInstallDir(), "keys")
	if err := utils.EnsureDir(destKeys, 0755); err != nil {
		return fmt.Errorf("failed to ensure server keys dir %s: %w", destKeys, err)
	}

	entries, err := os.ReadDir(srcKeys)
	if err != nil {
		return fmt.Errorf("failed to read Keys dir %s: %w", srcKeys, err)
	}

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
			_ = in.Close()
			fmt.Printf("Failed to create dest key %s: %v\n", destFile, err)
			continue
		}
		if _, err := io.Copy(out, in); err != nil {
			fmt.Printf("Failed to copy key %s -> %s: %v\n", srcFile, destFile, err)
		}
		_ = in.Close()
		_ = out.Close()
		_ = utils.ChownPath(destFile)
	}
	return nil
}

func appendModRef(instance *config.Instance, modID, name string, isServerMod bool) {
	if isServerMod {
		instance.ServerMods = append(instance.ServerMods, config.ModRef{ID: modID, Name: name})
		fmt.Printf("Adding server mod %s (%s) to instance %s\n", modID, name, instance.Name)
	} else {
		instance.Mods = append(instance.Mods, config.ModRef{ID: modID, Name: name})
		fmt.Printf("Adding client mod %s (%s) to instance %s\n", modID, name, instance.Name)
	}
}

func finalizeAdd(modManager *mods.Manager, instance *config.Instance) error {
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

		for _, instance := range instances {
			if err := processRemove(instance, modIDs, modManager, deleteFiles); err != nil {
				return err
			}
		}

		if err := shared.SaveConfig(); err != nil {
			return err
		}

		return nil
	})
	return nil
}

// removeFromSlice removes entries with matching id from slice and
// returns the new slice and whether any removal occurred.
func removeFromSlice(slice []config.ModRef, id string) ([]config.ModRef, bool) {
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

// processRemove handles removal of mod references for a single instance.
func processRemove(instance *config.Instance, ids []string, modManager *mods.Manager, deleteFiles bool) error {
	for _, id := range ids {
		var removedAny bool
		instance.Mods, removedAny = removeFromSlice(instance.Mods, id)
		if removedAny {
			_ = modManager.RemoveMod(config.ModRef{ID: id}, false)
			if deleteFiles {
				_ = os.RemoveAll(modManager.GetModPath(config.ModRef{ID: id}, false))
			}
		}
		instance.ServerMods, removedAny = removeFromSlice(instance.ServerMods, id)
		if removedAny {
			_ = modManager.RemoveMod(config.ModRef{ID: id}, true)
			if deleteFiles {
				_ = os.RemoveAll(modManager.GetModPath(config.ModRef{ID: id}, true))
			}
		}
	}
	return nil
}

// runDeleteMods is an alias to runRemoveMods for now (keeps semantic clarity)
func runDeleteMods(instanceName string, modIDs []string, deleteFiles bool) error {
	return runRemoveMods(instanceName, modIDs, deleteFiles)
}
