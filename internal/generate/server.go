package generate

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/kabroxiko/dayzctl/internal/config"
)

// ServerConfigData represents data for server config templates
type ServerConfigData struct {
	Name                          string
	Port                          int
	ServerDir                     string
	Hostname                      string
	MaxPlayers                    int
	InstanceId                    int
	Password                      string
	PasswordAdmin                 string
	SteamQueryPort                int
	SteamPort                     int
	ClientPort                    int
	VerifySignatures              int
	ForceSameBuild                int
	BattlEye                      int
	EnableWhitelist               int
	DisableBanlist                bool
	DisablePrioritylist           bool
	Disable3rdPerson              int
	DisableCrosshair              int
	DisableVoN                    int
	DisablePersonalLight          int
	DisableBaseDamage             int
	DisableContainerDamage        int
	DisableRespawnDialog          int
	DisableMultiAccountMitigation bool
	VonCodecQuality               int
	ServerTime                    string
	ServerTimePersistent          int
	ServerTimeAcceleration        int
	ServerNightTimeAcceleration   float64
	LightingConfig                int
	LoginQueueConcurrentPlayers   int
	LoginQueueMaxPlayers          int
	GuaranteedUpdates             int
	NetworkRangeClose             int
	NetworkRangeNear              int
	NetworkRangeFar               int
	NetworkRangeDistantEffect     int
	SimulatedPlayersBatch         int
	MultithreadedReplication      int
	PingWarning                   int
	PingCritical                  int
	MaxPing                       int
	ServerFpsWarning              int
	StorageAutoFix                int
	StoreHouseStateDisabled       bool
	LootHistory                   int
	StorageAutoDestroyFlags       int
	StorageAutoDestroyInterval    int
	RespawnTime                   int
	SpeedhackDetection            int
	TimeStampFormat               string
	LogAverageFps                 int
	LogMemory                     int
	LogPlayers                    int
	AdminLogPlayerHitsOnly        int
	AdminLogPlacement             int
	AdminLogBuildActions          int
	AdminLogPlayerList            int
	EnableDebugMonitor            int
	AllowFilePatching             int
	DefaultVisibility             int
	DefaultObjectViewDistance     int
	ShotValidation                int
	Mods                          string
	ServerMods                    string
	Template                      string
	RconPassword                  string
	RconPort                      int
	RconEnabled                   bool
}

// formatModsForConfig formats mods for the server config (lowercase, without @)
func formatModsForConfig(modRefs []config.ModRef) string {
	if len(modRefs) == 0 {
		return ""
	}
	names := make([]string, len(modRefs))
	for i, mod := range modRefs {
		name := mod.Name
		if name == "" {
			name = mod.ID
		}
		name = strings.TrimPrefix(name, "@")
		// Convert to lowercase (matching the script)
		name = strings.ToLower(name)
		// Replace spaces with underscores
		name = strings.ReplaceAll(name, " ", "_")
		names[i] = name
	}
	return strings.Join(names, ";")
}

// GenerateServerConfig generates serverDZ.cfg for all instances
func GenerateServerConfig(cfg *config.ServerConfig, tmplContent string) error {
	tmpl, err := template.New("serverDZ").Parse(tmplContent)
	if err != nil {
		return fmt.Errorf("failed to parse serverDZ template: %w", err)
	}

	installDir := cfg.GetInstallDir()

	for _, instance := range cfg.GetEnabledInstances() {
		data := buildServerData(cfg, instance)

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("failed to generate server config for %s: %w", instance.Name, err)
		}

		cfgPath := fmt.Sprintf("%s/serverDZ-%s.cfg", installDir, instance.Name)
		if err := os.WriteFile(cfgPath, buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write server config for %s: %w", instance.Name, err)
		}
	}

	return nil
}

// buildServerData builds ServerConfigData from config
func buildServerData(cfg *config.ServerConfig, instance config.Instance) ServerConfigData {
	return ServerConfigData{
		Name:                          instance.Name,
		Port:                          instance.Port,
		ServerDir:                     cfg.GetInstallDir(),
		Hostname:                      instance.Hostname,
		MaxPlayers:                    instance.MaxPlayers,
		InstanceId:                    instance.InstanceID,
		Password:                      cfg.Server.Password,
		PasswordAdmin:                 cfg.Server.PasswordAdmin,
		SteamQueryPort:                cfg.Server.SteamQueryPort,
		SteamPort:                     cfg.Server.SteamPort,
		ClientPort:                    cfg.Server.ClientPort,
		VerifySignatures:              cfg.Server.VerifySignatures,
		ForceSameBuild:                cfg.Server.ForceSameBuild,
		BattlEye:                      cfg.Server.BattlEye,
		EnableWhitelist:               cfg.Server.EnableWhitelist,
		DisableBanlist:                cfg.Server.DisableBanlist,
		DisablePrioritylist:           cfg.Server.DisablePrioritylist,
		Disable3rdPerson:              cfg.Server.Disable3rdPerson,
		DisableCrosshair:              cfg.Server.DisableCrosshair,
		DisableVoN:                    cfg.Server.DisableVoN,
		DisablePersonalLight:          cfg.Server.DisablePersonalLight,
		DisableBaseDamage:             cfg.Server.DisableBaseDamage,
		DisableContainerDamage:        cfg.Server.DisableContainerDamage,
		DisableRespawnDialog:          cfg.Server.DisableRespawnDialog,
		DisableMultiAccountMitigation: cfg.Server.DisableMultiAccountMitigation,
		VonCodecQuality:               cfg.Server.VonCodecQuality,
		ServerTime:                    cfg.Server.ServerTime,
		ServerTimePersistent:          cfg.Server.ServerTimePersistent,
		ServerTimeAcceleration:        cfg.Server.ServerTimeAcceleration,
		ServerNightTimeAcceleration:   cfg.Server.ServerNightTimeAcceleration,
		LightingConfig:                cfg.Server.LightingConfig,
		LoginQueueConcurrentPlayers:   cfg.Server.LoginQueueConcurrentPlayers,
		LoginQueueMaxPlayers:          cfg.Server.LoginQueueMaxPlayers,
		GuaranteedUpdates:             cfg.Server.GuaranteedUpdates,
		NetworkRangeClose:             cfg.Server.NetworkRangeClose,
		NetworkRangeNear:              cfg.Server.NetworkRangeNear,
		NetworkRangeFar:               cfg.Server.NetworkRangeFar,
		NetworkRangeDistantEffect:     cfg.Server.NetworkRangeDistantEffect,
		SimulatedPlayersBatch:         cfg.Server.SimulatedPlayersBatch,
		MultithreadedReplication:      cfg.Server.MultithreadedReplication,
		PingWarning:                   cfg.Server.PingWarning,
		PingCritical:                  cfg.Server.PingCritical,
		MaxPing:                       cfg.Server.MaxPing,
		ServerFpsWarning:              cfg.Server.ServerFpsWarning,
		StorageAutoFix:                cfg.Server.StorageAutoFix,
		StoreHouseStateDisabled:       cfg.Server.StoreHouseStateDisabled,
		LootHistory:                   cfg.Server.LootHistory,
		StorageAutoDestroyFlags:       cfg.Server.StorageAutoDestroyFlags,
		StorageAutoDestroyInterval:    cfg.Server.StorageAutoDestroyInterval,
		RespawnTime:                   cfg.Server.RespawnTime,
		SpeedhackDetection:            cfg.Server.SpeedhackDetection,
		TimeStampFormat:               cfg.Server.TimeStampFormat,
		LogAverageFps:                 cfg.Server.LogAverageFps,
		LogMemory:                     cfg.Server.LogMemory,
		LogPlayers:                    cfg.Server.LogPlayers,
		AdminLogPlayerHitsOnly:        cfg.Server.AdminLogPlayerHitsOnly,
		AdminLogPlacement:             cfg.Server.AdminLogPlacement,
		AdminLogBuildActions:          cfg.Server.AdminLogBuildActions,
		AdminLogPlayerList:            cfg.Server.AdminLogPlayerList,
		EnableDebugMonitor:            cfg.Server.EnableDebugMonitor,
		AllowFilePatching:             cfg.Server.AllowFilePatching,
		DefaultVisibility:             cfg.Server.DefaultVisibility,
		DefaultObjectViewDistance:     cfg.Server.DefaultObjectViewDistance,
		ShotValidation:                cfg.Server.ShotValidation,
		Mods:                          formatModsForConfig(instance.Mods),
		ServerMods:                    formatModsForConfig(instance.ServerMods),
		Template:                      instance.Template,
		RconPassword:                  instance.RCON.Password,
		RconPort:                      instance.RCON.Port,
		RconEnabled:                   instance.RCON.Enabled,
	}
}
