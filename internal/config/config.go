package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultConfigPath returns the default path to the config file
func DefaultConfigPath() string {
	return "/etc/dayzctl/config.yaml"
}

// ============================================================================
// CONFIGURATION STRUCTURES
// ============================================================================

// ServerConfig represents the full server configuration
type ServerConfig struct {
	Server       Server        `yaml:"server"`
	Steam        Steam         `yaml:"steam"`
	Paths        Paths         `yaml:"paths"`
	Instances    []Instance    `yaml:"instances"`
	ManagedFiles []ManagedFile `yaml:"managed_files"`
	ManagedDirs  []ManagedDir  `yaml:"managed_dirs"`
	Backup       Backup        `yaml:"backup"`
	Updates      Updates       `yaml:"updates"`
	Healthcheck  Healthcheck   `yaml:"healthcheck"`
	State        State         `yaml:"state"`
	// LoadedPath stores the path the configuration was loaded from.
	// It is not serialized to YAML and is used to ensure Save writes
	// back to the original file that was read by the process.
	LoadedPath string `yaml:"-"`
}

// Server represents server-wide settings
type Server struct {
	MaxPlayers                    int     `yaml:"max_players"`
	Password                      string  `yaml:"password"`
	PasswordAdmin                 string  `yaml:"password_admin"`
	SteamQueryPort                int     `yaml:"steam_query_port"`
	SteamPort                     int     `yaml:"steam_port"`
	ClientPort                    int     `yaml:"client_port"`
	VerifySignatures              int     `yaml:"verify_signatures"`
	ForceSameBuild                int     `yaml:"force_same_build"`
	BattlEye                      int     `yaml:"battleye"`
	EnableWhitelist               int     `yaml:"enable_whitelist"`
	DisableBanlist                bool    `yaml:"disable_banlist"`
	DisablePrioritylist           bool    `yaml:"disable_prioritylist"`
	Disable3rdPerson              int     `yaml:"disable_3rd_person"`
	DisableCrosshair              int     `yaml:"disable_crosshair"`
	DisableVoN                    int     `yaml:"disable_von"`
	DisablePersonalLight          int     `yaml:"disable_personal_light"`
	DisableBaseDamage             int     `yaml:"disable_base_damage"`
	DisableContainerDamage        int     `yaml:"disable_container_damage"`
	DisableRespawnDialog          int     `yaml:"disable_respawn_dialog"`
	DisableMultiAccountMitigation bool    `yaml:"disable_multi_account_mitigation"`
	VonCodecQuality               int     `yaml:"von_codec_quality"`
	ServerTime                    string  `yaml:"server_time"`
	ServerTimePersistent          int     `yaml:"server_time_persistent"`
	ServerTimeAcceleration        int     `yaml:"server_time_acceleration"`
	ServerNightTimeAcceleration   float64 `yaml:"server_night_time_acceleration"`
	LightingConfig                int     `yaml:"lighting_config"`
	LoginQueueConcurrentPlayers   int     `yaml:"login_queue_concurrent_players"`
	LoginQueueMaxPlayers          int     `yaml:"login_queue_max_players"`
	GuaranteedUpdates             int     `yaml:"guaranteed_updates"`
	NetworkRangeClose             int     `yaml:"network_range_close"`
	NetworkRangeNear              int     `yaml:"network_range_near"`
	NetworkRangeFar               int     `yaml:"network_range_far"`
	NetworkRangeDistantEffect     int     `yaml:"network_range_distant_effect"`
	SimulatedPlayersBatch         int     `yaml:"simulated_players_batch"`
	MultithreadedReplication      int     `yaml:"multithreaded_replication"`
	PingWarning                   int     `yaml:"ping_warning"`
	PingCritical                  int     `yaml:"ping_critical"`
	MaxPing                       int     `yaml:"max_ping"`
	ServerFpsWarning              int     `yaml:"server_fps_warning"`
	StorageAutoFix                int     `yaml:"storage_auto_fix"`
	StoreHouseStateDisabled       bool    `yaml:"store_house_state_disabled"`
	LootHistory                   int     `yaml:"loot_history"`
	StorageAutoDestroyFlags       int     `yaml:"storage_auto_destroy_flags"`
	StorageAutoDestroyInterval    int     `yaml:"storage_auto_destroy_interval"`
	RespawnTime                   int     `yaml:"respawn_time"`
	SpeedhackDetection            int     `yaml:"speedhack_detection"`
	TimeStampFormat               string  `yaml:"time_stamp_format"`
	LogAverageFps                 int     `yaml:"log_average_fps"`
	LogMemory                     int     `yaml:"log_memory"`
	LogPlayers                    int     `yaml:"log_players"`
	AdminLogPlayerHitsOnly        int     `yaml:"admin_log_player_hits_only"`
	AdminLogPlacement             int     `yaml:"admin_log_placement"`
	AdminLogBuildActions          int     `yaml:"admin_log_build_actions"`
	AdminLogPlayerList            int     `yaml:"admin_log_player_list"`
	EnableDebugMonitor            int     `yaml:"enable_debug_monitor"`
	AllowFilePatching             int     `yaml:"allow_file_patching"`
	DefaultVisibility             int     `yaml:"default_visibility"`
	DefaultObjectViewDistance     int     `yaml:"default_object_view_distance"`
	ShotValidation                int     `yaml:"shot_validation"`
}

// Steam represents Steam configuration
type Steam struct {
	Username string `yaml:"username"`
}

// Paths represents directory paths
type Paths struct {
	Base        string `yaml:"base"`
	InstallDir  string `yaml:"install_dir,omitempty"`
	WorkshopDir string `yaml:"workshop_dir,omitempty"`
	ModsDir     string `yaml:"mods_dir,omitempty"`
	BackupsDir  string `yaml:"backups_dir,omitempty"`
	StateDir    string `yaml:"state_dir,omitempty"`
	StorageDir  string `yaml:"storage_dir,omitempty"`
	SteamcmdBin string `yaml:"steamcmd_bin,omitempty"`
}

// Instance represents a server instance
type Instance struct {
	Name             string            `yaml:"name"`
	InstanceID       int               `yaml:"instanceId"`
	Port             int               `yaml:"port"`
	SteamQueryPort   int               `yaml:"steam_query_port"`
	Template         string            `yaml:"template"`
	Hostname         string            `yaml:"hostname"`
	MaxPlayers       int               `yaml:"max_players"`
	Enabled          bool              `yaml:"enabled"`
	RCON             RCON              `yaml:"rcon"`
	Mods             []ModRef          `yaml:"mods"`
	ServerMods       []ModRef          `yaml:"servermods"`
	ShutdownMessages []ShutdownMessage `yaml:"shutdown_messages"`
}

// ModRef represents a mod reference in the config
type ModRef struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// ShutdownMessage represents a single scheduled entry written to the
// mission's db/messages.xml. DayZ uses this file to broadcast countdown
// warnings to players before a scheduled restart and, when Shutdown is
// true, to perform the actual graceful shutdown (world/player state save,
// then exit) once Deadline is reached - this is the server's own "gentle"
// shutdown mechanism, distinct from killing the process externally.
type ShutdownMessage struct {
	// Deadline is the number of seconds after server start at which this
	// message fires (DayZ schedules these relative to server uptime, not
	// wall-clock time).
	Deadline int `yaml:"deadline"`
	// Text is the message broadcast to all connected players.
	Text string `yaml:"text"`
	// Shutdown, when true, tells the server to gracefully save and exit
	// once this message's deadline is reached.
	Shutdown bool `yaml:"shutdown"`
	// Repeat is the interval in seconds to repeat this message; 0 means it
	// fires only once.
	Repeat int `yaml:"repeat,omitempty"`
	// PlayerCount is the minimum number of online players required for the
	// message to be shown; 0 means always show it.
	PlayerCount int `yaml:"player_count,omitempty"`
}

// RCON represents RCON configuration
type RCON struct {
	Enabled  bool   `yaml:"enabled"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
}

// ManagedFile represents a managed file
type ManagedFile struct {
	Source string `yaml:"source"`
	Backup bool   `yaml:"backup"`
}

// ManagedDir represents a managed directory
type ManagedDir struct {
	Path   string `yaml:"path"`
	Backup bool   `yaml:"backup"`
}

// Backup represents backup configuration
type Backup struct {
	Enabled       bool `yaml:"enabled"`
	RetentionDays int  `yaml:"retention_days"`
	KeepDaily     int  `yaml:"keep_daily"`
	KeepWeekly    int  `yaml:"keep_weekly"`
}

// Updates represents update configuration
type Updates struct {
	Enabled  bool   `yaml:"enabled"`
	Schedule string `yaml:"schedule"`
}

// Healthcheck represents healthcheck configuration
type Healthcheck struct {
	Enabled        bool `yaml:"enabled"`
	StartupTimeout int  `yaml:"startup_timeout"`
}

// State represents state configuration
type State struct {
	InventoryEnabled bool `yaml:"inventory_enabled"`
}

// ============================================================================
// HELPER METHODS
// ============================================================================

// GetBaseDir returns the base directory from paths
func (c *ServerConfig) GetBaseDir() string {
	if c.Paths.Base != "" {
		return c.Paths.Base
	}
	return "/srv/dayz"
}

// GetSteamUser returns the Steam username
func (c *ServerConfig) GetSteamUser() string {
	return c.Steam.Username
}

// GetInstallDir returns the installation directory
func (c *ServerConfig) GetInstallDir() string {
	if c.Paths.InstallDir != "" {
		if filepath.IsAbs(c.Paths.InstallDir) {
			return c.Paths.InstallDir
		}
		return filepath.Join(c.GetBaseDir(), c.Paths.InstallDir)
	}
	return filepath.Join(c.GetBaseDir(), "server")
}

// GetBackupDir returns the backup directory
func (c *ServerConfig) GetBackupDir() string {
	if c.Paths.BackupsDir != "" {
		if filepath.IsAbs(c.Paths.BackupsDir) {
			return c.Paths.BackupsDir
		}
		return filepath.Join(c.GetBaseDir(), c.Paths.BackupsDir)
	}
	return filepath.Join(c.GetBaseDir(), "backups")
}

// GetWorkshopDir returns the workshop directory
func (c *ServerConfig) GetWorkshopDir() string {
	if c.Paths.WorkshopDir != "" {
		if filepath.IsAbs(c.Paths.WorkshopDir) {
			return c.Paths.WorkshopDir
		}
		return filepath.Join(c.GetBaseDir(), c.Paths.WorkshopDir)
	}
	return filepath.Join(c.GetBaseDir(), "workshop")
}

// GetSteamcmdBin returns the steamcmd binary path
func (c *ServerConfig) GetSteamcmdBin() string {
	if c.Paths.SteamcmdBin != "" {
		if filepath.IsAbs(c.Paths.SteamcmdBin) {
			return c.Paths.SteamcmdBin
		}
		return filepath.Join(c.GetBaseDir(), c.Paths.SteamcmdBin)
	}
	return filepath.Join(c.GetBaseDir(), "steamcmd/steamcmd.sh")
}

// GetEnabledInstances returns only enabled instances
func (c *ServerConfig) GetEnabledInstances() []Instance {
	var enabled []Instance
	for _, inst := range c.Instances {
		if inst.Enabled {
			enabled = append(enabled, inst)
		}
	}
	return enabled
}

// GetInstanceNames returns names of all enabled instances
func (c *ServerConfig) GetInstanceNames() []string {
	var names []string
	for _, inst := range c.Instances {
		if inst.Enabled {
			names = append(names, inst.Name)
		}
	}
	return names
}

// GetInstanceByName returns an instance by name
func (c *ServerConfig) GetInstanceByName(name string) (*Instance, error) {
	for i := range c.Instances {
		if c.Instances[i].Name == name {
			return &c.Instances[i], nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", name)
}

// HasInstance checks if an instance exists
func (c *ServerConfig) HasInstance(name string) bool {
	for _, inst := range c.Instances {
		if inst.Name == name {
			return true
		}
	}
	return false
}

// ============================================================================
// LOAD FUNCTION
// ============================================================================

// Load loads the configuration from a file
func Load(path string) (*ServerConfig, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	cfg.SetDefaults()

	// remember where we loaded the config from so callers can save back
	// to the same location later
	cfg.LoadedPath = path

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}
