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

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
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

// The template file is named after what it renders (a web unit); the unit
// itself is installed as WebUnitName (dayz-web.service).
//
//go:embed templates/web.service.tmpl
var webServiceTemplate string

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

// IsActive reports whether the given systemd unit is currently active
// (running). Used to poll a unit's state while waiting for a graceful
// shutdown to complete.
func (s *Systemd) IsActive(unit string) bool {
	cmd := exec.Command("systemctl", "is-active", unit)
	output, _ := cmd.Output()
	return strings.TrimSpace(string(output)) == "active"
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

// systemdDir is where every generated unit is written. It is a variable so the
// tests can point it at a temporary directory.
var systemdDir = "/etc/systemd/system"

// unitFile is one unit to write, already rendered or taken from an embedded
// template.
type unitFile struct {
	name    string
	content []byte
}

// GenerateUnits writes every unit the configuration implies and returns the
// names of the files that actually changed on disk. Files whose content already
// matches are left untouched, so a second `dayzctl apply` reports no change and
// does not need to reload systemd.
//
// The units are part of apply rather than of the package: nothing is shipped
// ready-made, so the files always match the configuration they were generated
// from and an upgrade cannot leave a stale service behind.
func (s *Systemd) GenerateUnits(cfg *config.ServerConfig) ([]string, error) {
	var units []unitFile

	units = append(units,
		unitFile{"dayz-update.service", []byte(updateServiceTemplate)},
		unitFile{"dayz-update.timer", []byte(updateTimerTemplate)},
		unitFile{"dayz-prune.service", []byte(pruneServiceTemplate)},
		unitFile{"dayz-prune.timer", []byte(pruneTimerTemplate)},
	)

	installDir := cfg.GetInstallDir()
	logger.Debug("Generating systemd units", "installDir", installDir)

	cpuCount := getCPUCount()
	logger.Info("Detected CPU cores", "count", cpuCount)

	svcTmpl, err := template.New("instance").Parse(instanceServiceTemplate)
	if err != nil {
		return nil, err
	}

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
			return nil, err
		}
		units = append(units, unitFile{fmt.Sprintf("dayz@%s.service", instance.Name), svcBuf.Bytes()})
	}

	var changed []string
	for _, u := range units {
		ok, err := writeIfChanged(filepath.Join(systemdDir, u.name), u.content)
		if err != nil {
			return changed, fmt.Errorf("failed to write %s: %w", u.name, err)
		}
		if ok {
			changed = append(changed, u.name)
		}
	}

	webChanged, err := s.reconcileWebUnit(cfg)
	if err != nil {
		return changed, err
	}
	if webChanged {
		changed = append(changed, WebUnitName)
	}

	return changed, nil
}

// writeIfChanged writes content to path only when it differs from what is
// already there, and reports whether it wrote.
func writeIfChanged(path string, content []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return false, nil
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return false, err
	}
	return true, nil
}

// WebUnitName is the systemd unit that runs the web UI. It is written and
// enabled by `dayzctl apply` — the package ships no unit files — so the name is
// defined once and every command refers to it.
const WebUnitName = "dayz-web.service"

// WebUnitPath is the absolute path of the generated web UI unit.
func WebUnitPath() string { return filepath.Join(systemdDir, WebUnitName) }

// WebData is the template input for web.service.tmpl (installed as
// dayz-web.service).
//
// The listen address and port are deliberately not part of the unit: the web
// command reads web.address / web.port from the config file, so an operator can
// change them without regenerating the unit.
type WebData struct {
	BinaryPath string
	WorkingDir string
}

// reconcileWebUnit writes dayz-web.service when the web UI is enabled and
// removes it when it is not, so a disabled service cannot fail at boot, and
// reports whether the file changed.
func (s *Systemd) reconcileWebUnit(cfg *config.ServerConfig) (bool, error) {
	path := filepath.Join(systemdDir, WebUnitName)

	if !cfg.Web.Enabled {
		if _, err := os.Stat(path); err != nil {
			return false, nil // nothing generated before
		}
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("failed to remove %s: %w", WebUnitName, err)
		}
		logger.Info("Removed web unit", "path", path)
		return true, nil
	}

	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "dayzctl"
	} else if resolved, evalErr := filepath.EvalSymlinks(binaryPath); evalErr == nil {
		binaryPath = resolved
	}

	content, err := RenderWebUnit(WebData{
		BinaryPath: binaryPath,
		WorkingDir: cfg.GetBaseDir(),
	})
	if err != nil {
		return false, err
	}

	changed, err := writeIfChanged(path, content)
	if err != nil {
		return false, fmt.Errorf("failed to write %s: %w", WebUnitName, err)
	}
	logger.Debug("Web unit", "path", path, "changed", changed,
		"address", cfg.Web.Address, "port", cfg.Web.Port)
	return changed, nil
}

// RenderWebUnit renders the web.service.tmpl template into the contents of
// dayz-web.service from the embedded copy.
func RenderWebUnit(data WebData) ([]byte, error) {
	tmpl, err := template.New("web").Parse(webServiceTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse web.service.tmpl template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", WebUnitName, err)
	}
	return buf.Bytes(), nil
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
