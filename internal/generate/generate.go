package generate

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"text/template"

	"github.com/dayzctl/dayzctl/internal/config"
)

//go:embed templates/serverDZ.cfg.tmpl
var serverDZTemplate string

//go:embed templates/BEServer.cfg.tmpl
var beserverTemplate string

// GenerateAll generates all server config files
func GenerateAll(cfg *config.ServerConfig) error {
	if err := GenerateServerConfig(cfg, serverDZTemplate); err != nil {
		return fmt.Errorf("failed to generate server configs: %w", err)
	}

	if err := GenerateBattlEyeConfig(cfg, beserverTemplate); err != nil {
		return fmt.Errorf("failed to generate battleye configs: %w", err)
	}

	return nil
}

// GenerateForInstances generates server and battleye configs for a specific set of instances
func GenerateForInstances(cfg *config.ServerConfig, instances []*config.Instance) error {
	// Server configs
	tmplServer, err := template.New("serverDZ").Parse(serverDZTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse serverDZ template: %w", err)
	}

	installDir := cfg.GetInstallDir()
	for _, instance := range instances {
		data := buildServerData(cfg, *instance)

		var buf bytes.Buffer
		if err := tmplServer.Execute(&buf, data); err != nil {
			return fmt.Errorf("failed to generate server config for %s: %w", instance.Name, err)
		}

		cfgPath := fmt.Sprintf("%s/serverDZ-%s.cfg", installDir, instance.Name)
		if err := os.WriteFile(cfgPath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write server config for %s: %w", instance.Name, err)
		}
	}

	// BattlEye configs
	tmplBE, err := template.New("BEServer").Parse(beserverTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse BEServer template: %w", err)
	}

	for _, instance := range instances {
		data := buildServerData(cfg, *instance)
		if err := generateInstanceBattlEye(installDir, *instance, data, tmplBE); err != nil {
			return fmt.Errorf("failed to generate battleye config for %s: %w", instance.Name, err)
		}
	}

	return nil
}
