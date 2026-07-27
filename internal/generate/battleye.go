package generate

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/logger"
	"github.com/kabroxiko/dayzctl/internal/utils"
)

// GenerateBattlEyeConfig generates beserver_x64.cfg for all instances
func GenerateBattlEyeConfig(cfg *config.ServerConfig, tmplContent string) error {
	tmpl, err := template.New("BEServer").Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("failed to parse BEServer template: %w", err)
	}

	installDir := cfg.GetInstallDir()

	for _, instance := range cfg.GetEnabledInstances() {
		data := buildServerData(cfg, instance)

		if err := generateInstanceBattlEye(installDir, instance, data, tmpl); err != nil {
			return fmt.Errorf("failed to generate battleye config for %s: %w", instance.Name, err)
		}
	}

	return nil
}

// generateInstanceBattlEye generates BattlEye config for a single instance
func generateInstanceBattlEye(installDir string, instance config.Instance, data ServerConfigData, tmpl *template.Template) error {
	beDir := filepath.Join(installDir, "battleye-"+instance.Name)

	logger.Debug("Generating BattlEye config", "installDir", installDir, "beDir", beDir, "instance", instance.Name)

	if err := os.MkdirAll(beDir, 0755); err != nil {
		return fmt.Errorf("failed to create battleye directory: %w", err)
	}

	// Generate config content
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to generate battleye config: %w", err)
	}
	content := buf.Bytes()

	// Write beserver_x64.cfg (lowercase - used by DayZServer with -BEpath)
	beserverPath := filepath.Join(beDir, "beserver_x64.cfg")
	if err := os.WriteFile(beserverPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write beserver_x64.cfg: %w", err)
	}
	logger.Debug("Wrote beserver_x64.cfg", "path", beserverPath)

	// Create symlink for .so file (beserver_x64.so - lowercase)
	linkPath := filepath.Join(beDir, "beserver_x64.so")

	// Remove existing file/link if it exists
	if err := os.Remove(linkPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing symlink: %w", err)
	}

	// Create symlink to main battleye directory
	symlinkTarget := "../battleye/beserver_x64.so"
	if err := os.Symlink(symlinkTarget, linkPath); err != nil {
		return fmt.Errorf("failed to create symlink for beserver_x64.so: %w", err)
	}
	logger.Debug("Created symlink for beserver_x64.so", "link", linkPath, "target", symlinkTarget)

	// Chown the symlink only (not the target)
	if err := utils.ChownSymlink(linkPath); err != nil {
		logger.Warn("Failed to chown battleye symlink", "path", linkPath, "error", err)
	}

	return nil
}
