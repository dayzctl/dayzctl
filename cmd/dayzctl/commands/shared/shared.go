package shared

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/generate"
	"github.com/kabroxiko/dayzctl/internal/logger"
	"github.com/kabroxiko/dayzctl/internal/systemd"
	"gopkg.in/yaml.v3"
)

var (
	Config *config.ServerConfig
)

func GetInstance(name string) (*config.Instance, error) {
	if name == "all" {
		return nil, fmt.Errorf("'all' is a reserved keyword for targeting all instances")
	}
	if Config == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	for i := range Config.Instances {
		if Config.Instances[i].Name == name {
			return &Config.Instances[i], nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", name)
}

func GetInstances(name string) ([]*config.Instance, error) {
	if name == "all" {
		var instances []*config.Instance
		for i := range Config.Instances {
			if Config.Instances[i].Enabled {
				instances = append(instances, &Config.Instances[i])
			}
		}
		if len(instances) == 0 {
			return nil, fmt.Errorf("no enabled instances found")
		}
		return instances, nil
	}
	instance, err := GetInstance(name)
	if err != nil {
		return nil, err
	}
	return []*config.Instance{instance}, nil
}

func RunCommand(fn func() error) {
	if err := fn(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func SaveConfig() error {
	if Config == nil {
		return fmt.Errorf("config not loaded")
	}

	data, err := yaml.Marshal(Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	configPath := config.DefaultConfigPath()
	if Config.Paths.Base != "" {
		configPath = Config.Paths.Base + "/config/config.yaml"
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func UpdateServerConfig(instance *config.Instance) error {
	configPath := fmt.Sprintf("%s/serverDZ-%s.cfg", Config.GetInstallDir(), instance.Name)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return fmt.Errorf("server config not found: %s", configPath)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	content := string(data)

	modNames := make([]string, len(instance.Mods))
	for i, mod := range instance.Mods {
		name := mod.Name
		if name == "" {
			name = mod.ID
		}
		name = strings.TrimPrefix(name, "@")
		modNames[i] = name
	}
	modsList := strings.Join(modNames, ";")

	if modsList != "" {
		modParam := fmt.Sprintf("mod = %s;", modsList)
		re := regexp.MustCompile(`(?m)^\s*mod\s*=\s*[^;]*;`)
		if re.MatchString(content) {
			content = re.ReplaceAllString(content, modParam)
		} else {
			content = strings.Replace(content, "}", modParam+"\n}", 1)
		}
	} else {
		re := regexp.MustCompile(`(?m)^\s*mod\s*=\s*[^;]*;\s*\n`)
		content = re.ReplaceAllString(content, "")
	}

	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func RestartInstance(instanceName string) error {
	cmd := exec.Command("systemctl", "restart", "dayz@"+instanceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart instance %s: %w", instanceName, err)
	}
	return nil
}

func ApplyConfig() error {
	logger.Info("Applying configuration changes...")

	if err := generate.GenerateAll(Config); err != nil {
		return fmt.Errorf("failed to generate server configs: %w", err)
	}

	sysd := systemd.New()
	if err := sysd.GenerateUnits(Config); err != nil {
		return fmt.Errorf("failed to generate units: %w", err)
	}
	if err := sysd.Reload(); err != nil {
		return fmt.Errorf("failed to reload systemd: %w", err)
	}

	logger.Info("Configuration applied successfully")
	return nil
}
