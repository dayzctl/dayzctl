package mods

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/logger"
	"github.com/kabroxiko/dayzctl/internal/utils"
)

// Mod represents a mod with its ID and metadata
type Mod struct {
	ID   string
	Name string
	Path string
}

// Manager handles mod operations
type Manager struct {
	installDir  string
	workshopDir string
}

// New creates a new mod manager
func New(installDir, workshopDir string) *Manager {
	return &Manager{
		installDir:  installDir,
		workshopDir: workshopDir,
	}
}

// ListInstalled returns a list of installed mods with their names
func (m *Manager) ListInstalled() ([]Mod, error) {
	var mods []Mod

	entries, err := os.ReadDir(m.workshopDir)
	if err != nil {
		if os.IsNotExist(err) {
			return mods, nil
		}
		return nil, fmt.Errorf("failed to read workshop directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := fmt.Sscanf(entry.Name(), "%d", new(int)); err == nil {
			modPath := filepath.Join(m.workshopDir, entry.Name())
			name := m.getModName(modPath)
			if name == "" {
				name = entry.Name()
			}
			mods = append(mods, Mod{
				ID:   entry.Name(),
				Name: name,
				Path: modPath,
			})
		}
	}

	return mods, nil
}

// getModName reads the mod name from meta.cpp
func (m *Manager) getModName(modPath string) string {
	metaPath := filepath.Join(modPath, "meta.cpp")
	if _, err := os.Stat(metaPath); err == nil {
		return m.parseMetaFile(metaPath)
	}

	metaPath = filepath.Join(modPath, "meta.hpp")
	if _, err := os.Stat(metaPath); err == nil {
		return m.parseMetaFile(metaPath)
	}

	metaPath = filepath.Join(modPath, "mod.cpp")
	if _, err := os.Stat(metaPath); err == nil {
		return m.parseMetaFile(metaPath)
	}

	metaPath = filepath.Join(modPath, "config.cpp")
	if _, err := os.Stat(metaPath); err == nil {
		return m.parseMetaFile(metaPath)
	}

	return ""
}

// parseMetaFile parses the meta file to extract the mod name
func (m *Manager) parseMetaFile(filePath string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}

	content := string(data)

	patterns := []string{
		`name\s*=\s*"([^"]+)"`,
		`name\s*=\s*'([^']+)'`,
		`name\s*=\s*([^;]+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(content)
		if len(matches) > 1 {
			name := strings.TrimSpace(matches[1])
			name = strings.TrimSuffix(name, ";")
			name = strings.Trim(name, `"'`)
			if name != "" {
				name = strings.ReplaceAll(name, " ", "_")
				return name
			}
		}
	}

	return ""
}

// getSymlinkName returns the symlink name with @ prefix
func (m *Manager) getSymlinkName(modRef config.ModRef) string {
	name := modRef.Name
	if name == "" {
		name = modRef.ID
	}
	return utils.FormatModName(name)
}

// SyncMods creates symlinks for the specified mods
func (m *Manager) SyncMods(modRefs []config.ModRef, serverModRefs []config.ModRef) error {
	modsDir := filepath.Join(m.installDir, "mods")
	if err := utils.EnsureDir(modsDir, 0755); err != nil {
		return fmt.Errorf("failed to create mods directory: %w", err)
	}

	for _, modRef := range modRefs {
		if err := m.syncMod(modRef, false); err != nil {
			return fmt.Errorf("failed to sync mod %s: %w", modRef.ID, err)
		}
	}

	serverModsDir := filepath.Join(m.installDir, "servermods")
	if err := utils.EnsureDir(serverModsDir, 0755); err != nil {
		return fmt.Errorf("failed to create servermods directory: %w", err)
	}

	for _, modRef := range serverModRefs {
		if err := m.syncMod(modRef, true); err != nil {
			return fmt.Errorf("failed to sync server mod %s: %w", modRef.ID, err)
		}
	}

	return nil
}

// syncMod creates a symlink
func (m *Manager) syncMod(modRef config.ModRef, isServerMod bool) error {
	srcPath := filepath.Join(m.workshopDir, modRef.ID)

	linkName := m.getSymlinkName(modRef)

	destDir := filepath.Join(m.installDir, "mods")
	if isServerMod {
		destDir = filepath.Join(m.installDir, "servermods")
	}

	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("mod %s not found in workshop at %s", modRef.ID, srcPath)
		}
		return fmt.Errorf("failed to stat mod %s: %w", modRef.ID, err)
	}

	destPath := filepath.Join(destDir, linkName)

	if err := os.Remove(destPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing symlink: %w", err)
	}

	if err := os.Symlink(srcPath, destPath); err != nil {
		return fmt.Errorf("failed to create symlink from %s to %s: %w", srcPath, destPath, err)
	}

	// Use ChownSymlink to change only the symlink itself, not the target
	if err := utils.ChownSymlink(destPath); err != nil {
		logger.Warn("Failed to chown symlink", "path", destPath, "error", err)
	}

	logger.Debug("Created symlink", "from", destPath, "to", srcPath)
	return nil
}

// RemoveMod removes a mod symlink
func (m *Manager) RemoveMod(modRef config.ModRef, isServerMod bool) error {
	linkName := m.getSymlinkName(modRef)

	destDir := filepath.Join(m.installDir, "mods")
	if isServerMod {
		destDir = filepath.Join(m.installDir, "servermods")
	}

	destPath := filepath.Join(destDir, linkName)

	if _, err := os.Lstat(destPath); err != nil {
		if os.IsNotExist(err) {
			logger.Debug("Symlink already removed", "path", destPath)
			return nil
		}
		return fmt.Errorf("failed to stat symlink: %w", err)
	}

	if err := os.Remove(destPath); err != nil {
		return fmt.Errorf("failed to remove symlink: %w", err)
	}

	logger.Debug("Removed symlink", "path", destPath)
	return nil
}

// GetModPath returns the path to a mod
func (m *Manager) GetModPath(modRef config.ModRef, isServerMod bool) string {
	linkName := m.getSymlinkName(modRef)

	if isServerMod {
		return filepath.Join(m.installDir, "servermods", linkName)
	}
	return filepath.Join(m.installDir, "mods", linkName)
}

// GetModInfo returns the mod info for a given ID
func (m *Manager) GetModInfo(modID string) (Mod, error) {
	modPath := filepath.Join(m.workshopDir, modID)

	if _, err := os.Stat(modPath); err != nil {
		return Mod{}, fmt.Errorf("mod %s not found: %w", modID, err)
	}

	name := m.getModName(modPath)
	if name == "" {
		name = modID
	}

	return Mod{
		ID:   modID,
		Name: name,
		Path: modPath,
	}, nil
}
