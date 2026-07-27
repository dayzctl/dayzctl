package systemd

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/kabroxiko/dayzctl/internal/config"
	"github.com/kabroxiko/dayzctl/internal/logger"
)

//go:embed templates/update.service.tmpl
var updateServiceTemplate string

//go:embed templates/update.timer.tmpl
var updateTimerTemplate string

//go:embed templates/prune.service.tmpl
var pruneServiceTemplate string

//go:embed templates/prune.timer.tmpl
var pruneTimerTemplate string

//go:embed templates/instance.service.tmpl
var instanceServiceTemplate string

type Systemd struct{}

func New() *Systemd {
	return &Systemd{}
}

func (s *Systemd) Start(unit string) error {
	return s.run("start", unit)
}

func (s *Systemd) Stop(unit string) error {
	return s.run("stop", unit)
}

func (s *Systemd) Restart(unit string) error {
	return s.run("restart", unit)
}

func (s *Systemd) Status(unit string) (string, error) {
	cmd := exec.Command("systemctl", "status", unit)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (s *Systemd) Reload() error {
	return s.run("daemon-reload", "")
}

func (s *Systemd) Enable(unit string) error {
	return s.run("enable", unit)
}

func (s *Systemd) Disable(unit string) error {
	return s.run("disable", unit)
}

func (s *Systemd) ListRunningInstances() ([]string, error) {
	cmd := exec.Command("systemctl", "list-units",
		"--type=service",
		"--state=running",
		"dayz@*.service",
		"--no-legend",
		"--no-pager",
	)
	output, err := cmd.Output()
	if err != nil {
		return nil, nil
	}

	var instances []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "dayz@") {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				name := strings.TrimPrefix(parts[0], "dayz@")
				name = strings.TrimSuffix(name, ".service")
				instances = append(instances, name)
			}
		}
	}
	return instances, nil
}

// getCPUCount returns the number of CPU cores
func getCPUCount() int {
	return runtime.NumCPU()
}

type InstanceData struct {
	Name        string
	Port        int
	ServerDir   string
	BattlEyeDir string
	Mods        string
	ServerMods  string
	CPUCount    int
}

// formatModsForSystemd formats mods for systemd with the path
func formatModsForSystemd(modRefs []config.ModRef) string {
	if len(modRefs) == 0 {
		return ""
	}
	prefixed := make([]string, len(modRefs))
	for i, mod := range modRefs {
		name := mod.Name
		if name == "" {
			name = mod.ID
		}
		name = strings.TrimPrefix(name, "@")
		name = strings.ToLower(name)
		name = strings.ReplaceAll(name, " ", "_")
		prefixed[i] = "mods/@" + name
	}
	return "\"" + strings.Join(prefixed, ";") + "\""
}

func (s *Systemd) GenerateUnits(cfg *config.ServerConfig) error {
	units := map[string]string{
		"dayz-update.service": updateServiceTemplate,
		"dayz-update.timer":   updateTimerTemplate,
		"dayz-prune.service":  pruneServiceTemplate,
		"dayz-prune.timer":    pruneTimerTemplate,
	}

	for name, content := range units {
		path := "/etc/systemd/system/" + name
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	svcTmpl, err := template.New("instance").Parse(instanceServiceTemplate)
	if err != nil {
		return err
	}

	installDir := cfg.GetInstallDir()
	logger.Debug("Generating systemd units", "installDir", installDir)

	cpuCount := getCPUCount()
	logger.Info("Detected CPU cores", "count", cpuCount)

	for _, instance := range cfg.GetEnabledInstances() {
		beDir := filepath.Join(installDir, "battleye-"+instance.Name)
		data := InstanceData{
			Name:        instance.Name,
			Port:        instance.Port,
			ServerDir:   installDir,
			BattlEyeDir: beDir,
			Mods:        formatModsForSystemd(instance.Mods),
			ServerMods:  formatModsForSystemd(instance.ServerMods),
			CPUCount:    cpuCount,
		}
		logger.Debug("Instance unit data", "instance", instance.Name, "beDir", beDir, "cpuCount", cpuCount)

		var svcBuf bytes.Buffer
		if err := svcTmpl.Execute(&svcBuf, data); err != nil {
			return err
		}
		svcPath := fmt.Sprintf("/etc/systemd/system/dayz@%s.service", instance.Name)
		if err := os.WriteFile(svcPath, svcBuf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write service for %s: %w", instance.Name, err)
		}
	}

	return nil
}

func (s *Systemd) run(action, unit string) error {
	args := []string{action}
	if unit != "" {
		args = append(args, unit)
	}
	cmd := exec.Command("systemctl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s %s failed: %w", action, unit, err)
	}
	return nil
}
