package config

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dayzctl/dayzctl/internal/logger"
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
	Web          Web           `yaml:"web"`
	// Messages are the DayZ server messages written to the mission of every
	// map in use. DayZ reads messages.xml from the mission folder, not per
	// instance, so the block is defined once here and generated per mission.
	Messages Messages `yaml:"messages,omitempty"`
	// ShutdownMessages is the pre-2.x list form, kept only so existing configs
	// keep working: it is translated into Messages at load time.
	ShutdownMessages []LegacyShutdownMessage `yaml:"shutdown_messages,omitempty"`
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
	Name           string   `yaml:"name"`
	InstanceID     int      `yaml:"instanceId"`
	Port           int      `yaml:"port"`
	SteamQueryPort int      `yaml:"steam_query_port"`
	Template       string   `yaml:"template"`
	Hostname       string   `yaml:"hostname"`
	MaxPlayers     int      `yaml:"max_players"`
	Enabled        bool     `yaml:"enabled"`
	RCON           RCON     `yaml:"rcon"`
	Mods           []ModRef `yaml:"mods"`
	ServerMods     []ModRef `yaml:"servermods"`
	// Messages overrides the top-level messages for this instance's mission.
	// Deprecated: messages.xml is stored per mission (map), so instances
	// running the same template share a single file.
	Messages Messages `yaml:"messages,omitempty"`
	// ShutdownMessages is the pre-2.x list form; translated into Messages.
	ShutdownMessages []LegacyShutdownMessage `yaml:"shutdown_messages"`
	// MapImage is an optional background image for the log analyzer's map.
	// Relative paths are resolved against the install directory. When empty,
	// map.jpg / map.png is picked up from the profiles or install folder.
	MapImage string `yaml:"map_image,omitempty"`
	// Map selects the community map used for tiles and place names, e.g.
	// "nasdara", "chernarusplus" or "livonia". It defaults to the instance
	// name, so an instance called "nasdara" needs no configuration.
	Map string `yaml:"map,omitempty"`
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
// Messages groups the DayZ server messages written to the mission's
// db/messages.xml (see "DayZ: Server Messages"). The file is read from the
// mission folder, so it is generated once per map in use rather than per
// instance. The three sections mirror the three kinds of message DayZ knows.
type Messages struct {
	// Welcome is sent once to every player that connects.
	Welcome *WelcomeMessage `yaml:"welcome,omitempty"`
	// Cyclic messages are broadcast to all players at a fixed interval.
	Cyclic []CyclicMessage `yaml:"cyclic,omitempty"`
	// Shutdown is the restart countdown (and optional graceful exit).
	Shutdown *ShutdownMessage `yaml:"shutdown,omitempty"`
}

// WelcomeMessage is shown once to every connecting player.
type WelcomeMessage struct {
	// Text is the message sent to the player. DayZ supports the #name and
	// #port placeholders and caps the text at 160 characters.
	Text string `yaml:"text"`
	// Delay is the number of minutes to wait after the player connects
	// before sending the message; 0 sends it immediately.
	Delay int `yaml:"delay,omitempty"`
}

// CyclicMessage is broadcast to everyone repeatedly, and optionally to every
// player that connects as well (DayZ sends the same message on both events).
type CyclicMessage struct {
	// Every is the interval in minutes between broadcasts.
	Every int `yaml:"every"`
	// Text is the broadcast message (#name, #port placeholders).
	Text string `yaml:"text"`
	// OnConnect also sends this message to every player that connects.
	OnConnect bool `yaml:"on_connect,omitempty"`
	// Delay is the number of minutes to wait after a player connects before
	// sending the on-connect copy; ignored without OnConnect.
	Delay int `yaml:"delay,omitempty"`
}

// ShutdownMessage is the restart countdown. DayZ announces it at 90, 60, 45,
// 30, 20, 15, 10, 5, 2 and 1 minutes before Deadline, measured from server
// start, and - when StopServer is set - saves the world and exits once the
// deadline is reached. That is the server's own "gentle" shutdown mechanism,
// distinct from killing the process externally.
type ShutdownMessage struct {
	// Deadline is the number of minutes after server start at which the
	// countdown reaches zero.
	Deadline int `yaml:"deadline"`
	// Text is the countdown message; #tmin expands to the minutes remaining.
	Text string `yaml:"text"`
	// StopServer saves and exits the server when the countdown reaches zero.
	StopServer bool `yaml:"stop_server,omitempty"`
}

// Empty reports whether no message is configured at all.
func (m Messages) Empty() bool {
	return m.Welcome == nil && len(m.Cyclic) == 0 && m.Shutdown == nil
}

// LegacyShutdownMessage is a single entry of the pre-2.x 'shutdown_messages'
// list. It mixes all three message kinds into one struct and is only kept so
// existing configs keep working; it is translated into Messages on load.
type LegacyShutdownMessage struct {
	Deadline    int    `yaml:"deadline"`
	Text        string `yaml:"text"`
	Shutdown    bool   `yaml:"shutdown"`
	Repeat      int    `yaml:"repeat,omitempty"`
	PlayerCount int    `yaml:"player_count,omitempty"`
	OnConnect   bool   `yaml:"on_connect,omitempty"`
	Delay       int    `yaml:"delay,omitempty"`
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

// Web configures the embedded web UI and log analysis API.
//
// The server only reads logs; it never writes to the DayZ install. Bind to
// 127.0.0.1 unless you enable auth or put an authenticating reverse proxy in
// front of it.
type Web struct {
	Enabled bool    `yaml:"enabled"`
	Address string  `yaml:"address,omitempty"`
	Port    int     `yaml:"port,omitempty"`
	Auth    WebAuth `yaml:"auth,omitempty"`
}

// WebAuth is the login switch for the web UI and its JSON API.
//
// Off by default. When enabled, every request needs a session cookie obtained
// by posting the username and password to /login; either a plain password or a
// SHA-256 hex digest (password_hash) can be configured.
type WebAuth struct {
	Enabled      bool   `yaml:"enabled"`
	Username     string `yaml:"username,omitempty"`
	Password     string `yaml:"password,omitempty"`
	PasswordHash string `yaml:"password_hash,omitempty"`
	SessionHours int    `yaml:"session_hours,omitempty"`
}

// Active reports whether a login is actually required. A half-configured block
// (enabled without credentials) is rejected during validation, so this only
// answers "is there something to protect with".
func (a WebAuth) Active() bool {
	return a.Enabled && a.Username != "" && (a.Password != "" || a.PasswordHash != "")
}

// SessionDuration is how long a login stays valid.
func (a WebAuth) SessionDuration() time.Duration {
	hours := a.SessionHours
	if hours <= 0 {
		hours = 12
	}
	return time.Duration(hours) * time.Hour
}

// CheckCredentials reports whether user/password may sign in.
func (a WebAuth) CheckCredentials(user, password string) bool {
	if !a.Active() {
		return true
	}
	if subtle.ConstantTimeCompare([]byte(user), []byte(a.Username)) != 1 {
		return false
	}
	if a.PasswordHash != "" {
		sum := sha256.Sum256([]byte(password))
		want := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(a.PasswordHash), "sha256:"))
		return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) == 1
	}
	return subtle.ConstantTimeCompare([]byte(password), []byte(a.Password)) == 1
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

// MapsDirEnv lets a test harness or a wrapper script point at map data without
// editing the configuration.
const MapsDirEnv = "DAYZCTL_MAPS_DIR"

// GetMapsDir returns the directory holding the log analyzer's map data.
//
// The location is fixed rather than configurable: next to the binary, then the
// two system directories below. An empty result means no map directory exists
// and the web server falls back to the data compiled into the binary.
func (c *ServerConfig) GetMapsDir() string {
	if dir := strings.TrimSpace(os.Getenv(MapsDirEnv)); dir != "" && isDir(dir) {
		return dir
	}

	for _, dir := range mapsCandidates() {
		if isDir(dir) {
			return dir
		}
	}
	return ""
}

// mapsCandidates lists the hardcoded map locations, in priority order:
//
//   - next to the binary, for a portable/tarball install;
//   - /usr/local/share/dayzctl/maps, for a locally installed copy;
//   - /usr/share/dayzctl/maps, where the .deb puts it.
func mapsCandidates() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "maps"))
		// Follow symlinks so /usr/local/bin/dayzctl -> /opt/dayzctl/dayzctl
		// also finds /opt/dayzctl/maps.
		if resolved, err := filepath.EvalSymlinks(exe); err == nil && resolved != exe {
			dirs = append(dirs, filepath.Join(filepath.Dir(resolved), "maps"))
		}
	}
	return append(dirs,
		"/usr/local/share/dayzctl/maps",
		"/usr/share/dayzctl/maps",
	)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
		return nil, fmt.Errorf("failed to parse config YAML: %w%s", err, yamlContext(data, err))
	}

	cfg.normalizeMessages()
	cfg.SetDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// normalizeMessages translates the deprecated 'shutdown_messages' list (which
// mixed the three kinds of DayZ message into one entry each) into the typed
// 'messages' block, so older configs keep working unchanged.
func (c *ServerConfig) normalizeMessages() {
	if len(c.ShutdownMessages) > 0 {
		if c.Messages.Empty() {
			c.Messages = messagesFromLegacy(c.ShutdownMessages)
			logger.Warn("'shutdown_messages' is deprecated; it was translated into the new 'messages' block",
				"key", "shutdown_messages", "replacement", "messages")
		} else {
			logger.Warn("'shutdown_messages' is deprecated and ignored because 'messages' is configured",
				"key", "shutdown_messages")
		}
		c.ShutdownMessages = nil
	}

	for i := range c.Instances {
		inst := &c.Instances[i]
		if len(inst.ShutdownMessages) == 0 {
			continue
		}
		inst.Messages = messagesFromLegacy(inst.ShutdownMessages)
		inst.ShutdownMessages = nil
		logger.Warn("instance-level 'shutdown_messages' is deprecated; messages.xml is per mission (map), so move the block to the top-level 'messages'",
			"instance", inst.Name)
	}
}

// messagesFromLegacy maps a legacy entry list onto the three message kinds.
// Entries that would do nothing (no deadline, repeat or on-connect flag) are
// dropped with a warning instead of producing a silent no-op in-game.
func messagesFromLegacy(list []LegacyShutdownMessage) Messages {
	var messages Messages
	for _, entry := range list {
		switch {
		case entry.OnConnect && entry.Repeat > 0:
			// Same message broadcast repeatedly and sent to every player that
			// connects - exactly what a cyclic message with on_connect does.
			messages.Cyclic = append(messages.Cyclic, CyclicMessage{
				Every:     entry.Repeat,
				Text:      entry.Text,
				OnConnect: true,
				Delay:     entry.Delay,
			})
		case entry.OnConnect:
			if messages.Welcome == nil {
				messages.Welcome = &WelcomeMessage{Text: entry.Text, Delay: entry.Delay}
			}
		case entry.Deadline > 0:
			if messages.Shutdown == nil {
				messages.Shutdown = &ShutdownMessage{
					Deadline:   entry.Deadline,
					Text:       entry.Text,
					StopServer: entry.Shutdown,
				}
			}
		case entry.Repeat > 0:
			messages.Cyclic = append(messages.Cyclic, CyclicMessage{Every: entry.Repeat, Text: entry.Text})
		default:
			logger.Warn("Ignoring 'shutdown_messages' entry without deadline, repeat or on_connect",
				"text", entry.Text)
		}
	}
	return messages
}

var yamlLineRE = regexp.MustCompile(`(?:^|\s)line (\d+):`)

// yamlErrorLine pulls the line number out of a yaml.v3 error message.
func yamlErrorLine(msg string) int {
	m := yamlLineRE.FindStringSubmatch(msg)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

// yamlContext quotes the line a YAML error points at, plus a little context.
// yaml.v3 only reports "line 63: did not find expected key", which is hard to
// act on in a config that is a hundred lines long.
func yamlContext(data []byte, err error) string {
	line := yamlErrorLine(err.Error())
	if line <= 0 {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if line > len(lines) {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")
	// yaml.v3 often blames the key that opens a block rather than the line that
	// is actually malformed, so show a few lines either side of it.
	for n := max(1, line-3); n <= min(len(lines), line+5); n++ {
		marker := "  "
		if n == line {
			marker = "> "
		}
		fmt.Fprintf(&b, "  %s%4d | %s\n", marker, n, strings.TrimRight(lines[n-1], " \t\r"))
	}
	if tab := firstTabIndentLine(lines); tab > 0 {
		fmt.Fprintf(&b, "  hint: line %d is indented with a tab; YAML indentation must be spaces\n", tab)
	}
	return strings.TrimRight(b.String(), "\n")
}

// firstTabIndentLine returns the first line whose indentation contains a tab, or
// 0 when the file indents with spaces only. YAML rejects tabs for indentation,
// and the resulting error ("did not find expected key") does not say why.
func firstTabIndentLine(lines []string) int {
	for i, line := range lines {
		for _, r := range line {
			if r == ' ' {
				continue
			}
			if r == '\t' {
				return i + 1
			}
			break
		}
	}
	return 0
}
