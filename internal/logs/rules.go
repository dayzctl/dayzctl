package logs

import (
	"regexp"
	"strings"
)

// Rule turns a recurring engine message into something actionable.
//
// Vanilla marks messages a stock, unmodded server also writes — those are
// Bohemia's own noise and should not be chased by admins.
type Rule struct {
	ID       string
	Re       *regexp.Regexp
	Severity Severity
	Category string
	Title    string
	Explain  string
	Fix      string
	Vanilla  bool
}

// Verdict is the classification of a single engine message.
type Verdict struct {
	RuleID   string
	Severity Severity
	Title    string
	Explain  string
	Fix      string
	Category string
	Vanilla  bool
}

// Rules is evaluated in order; the first match wins. Keep specific patterns
// above the generic fallbacks.
var Rules = []Rule{
	{
		ID:       "server-telemetry",
		Re:       regexp.MustCompile(`(?i)^(?:Average server FPS:|Used memory:|Players: \d+ in total)`),
		Severity: SeverityInfo,
		Category: "telemetry",
		Vanilla:  true,
		Title:    "Routine server telemetry",
		Explain:  "DayZ writes FPS, memory usage and the player count once per second. These lines are pure statistics and mostly just make the file large.",
		Fix:      "Nothing to do. To shrink logs, turn these off with logAverageFps, logMemory and logPlayers in serverDZ.cfg.",
	},
	{
		ID:       "entity-warning",
		Re:       regexp.MustCompile(`(?i)\bENTITY\s+\(([WE])\)\s*:\s*(.*)$`),
		Severity: SeverityWarning,
		Category: "models",
		Vanilla:  true,
		Title:    "Model or config problem in the entity loader",
		Explain:  "The engine reported a problem while loading an object: missing geometry components, a bad selection name, or a config class that does not exist. Stock DayZ models produce a burst of these on every start.",
		Fix:      "Nothing to do when the path starts with dz\\ or dz\\structures. If the message names a mod folder, update or remove that mod.",
	},
	{
		ID:       "entity-trace",
		Re:       regexp.MustCompile(`(?i)\bENTITY\s*:\s*`),
		Severity: SeverityInfo,
		Category: "models",
		Vanilla:  true,
		Title:    "Entity loader trace",
		Explain:  "A line from the entity loader while the world spawns. Informational only.",
		Fix:      "Nothing to do.",
	},
	{
		ID:       "addon-requires-addon",
		Re:       regexp.MustCompile(`(?i)Addon '([^']+)' requires addon '([^']+)'`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A mod is missing one of its dependencies",
		Explain:  "The mod {0} declares that it needs {1}, but that mod is not loaded. The engine keeps running, but content from {0} will be missing or broken.",
		Fix:      "Add {1} to -mod= (before {0}, dependencies load first) or to the server's mod list, then restart. If you already have it, check the folder name matches the addon's required name exactly.",
	},
	{
		ID:       "cannot-open-object",
		Re:       regexp.MustCompile(`(?i)Cannot open object\s+([^\s:]+\.p3d)`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A model file is missing",
		Explain:  "The game tried to load {0} and the file is not in any loaded PBO. Objects using it will be invisible or replaced by an error placeholder.",
		Fix:      "Find the mod that ships {0} and make sure it is loaded (and up to date). If it came from a mod you removed, delete the leftover config entry that still references it.",
	},
	{
		ID:       "cannot-load-texture",
		Re:       regexp.MustCompile(`(?i)Cannot load texture\s+([^\s:]+\.(?:paa|tga|png))`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A texture file is missing",
		Explain:  "The material asked for {0} but the texture is not present. Affected items render untextured (white, pink or black).",
		Fix:      "Verify the mod files (re-download the PBO) and confirm the texture is inside a PBO, not just lying loose in the mod folder.",
	},
	{
		ID:       "missing-sound",
		Re:       regexp.MustCompile(`(?i)(?:Cannot open sound|Sound .* not found)[^\n]*\.(?:ogg|wss)`),
		Severity: SeverityWarning,
		Category: "mods",
		Title:    "A sound file is missing",
		Explain:  "An audio sample referenced by a config could not be loaded. You will hear silence instead of the sound.",
		Fix:      "Reinstall the mod that owns the sound. Audio drops are cosmetic and do not break gameplay.",
	},
	{
		ID:       "no-entry-config",
		Re:       regexp.MustCompile(`(?i)No entry 'config\.bin/([^']+)'`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A config entry that something references does not exist",
		Explain:  "Something points at {0}, but that class is not defined in any loaded config. Spawns, traders or scripts using it will fail.",
		Fix:      "This is almost always a version mismatch: update the mod that defines {0}, or remove the config that references it. Check that you are not mixing a mod update with an outdated mission file.",
	},
	{
		ID:       "class-not-found",
		Re:       regexp.MustCompile(`(?i)Class '([^']+)' not found`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A referenced class is not defined",
		Explain:  "A config or script referenced class {0}, which no loaded addon provides.",
		Fix:      "Load the addon that defines {0}, or fix the typo in the config that references it.",
	},
	{
		ID:       "unknown-string",
		Re:       regexp.MustCompile(`(?i)Cannot register unknown string\s+(\S+)`),
		Severity: SeverityWarning,
		Category: "mods",
		Title:    "A localization key is missing",
		Explain:  "The key {0} is used somewhere but has no text in the loaded language files. Players see the raw key instead of readable text.",
		Fix:      "The mod author forgot a stringtable entry. Update the mod; you cannot fix it server-side without editing the PBO.",
	},
	{
		ID:       "unknown-enum",
		Re:       regexp.MustCompile(`(?i)Unknown enumeration value '([^']+)' in config`),
		Severity: SeverityWarning,
		Category: "mods",
		Title:    "A config uses an enum value this game version does not know",
		Explain:  "Config value {0} is not valid in the running engine version. It is usually a mod written for a different DayZ build.",
		Fix:      "Update the mod to a version matching your server build. If it still appears, check the mod's changelog for a known issue.",
	},
	{
		ID:       "strange-convex",
		Re:       regexp.MustCompile(`(?i)Strange convex component`),
		Severity: SeverityInfo,
		Category: "vanilla",
		Vanilla:  true,
		Title:    "Model geometry warning (harmless)",
		Explain:  "A model has an unusually shaped collision component. This is written by many stock DayZ models and does not affect gameplay.",
		Fix:      "Nothing to do. Ignore this unless you are the one authoring the model.",
	},
	{
		ID:       "vertex-buffer",
		Re:       regexp.MustCompile(`(?i)Vertex buffer|index buffer|not locked`),
		Severity: SeverityWarning,
		Category: "graphics",
		Title:    "Graphics buffer warning",
		Explain:  "The renderer reported a buffer problem. Occasional single lines are normal; repeated bursts usually precede a crash.",
		Fix:      "Update your GPU driver and verify the game files. If it repeats, lower texture quality or reduce the mod count.",
	},
	{
		ID:       "dxgi-device-removed",
		Re:       regexp.MustCompile(`(?i)DXGI_ERROR_DEVICE_(?:REMOVED|HUNG|RESET)|D3D11 error|Device removed`),
		Severity: SeverityCritical,
		Category: "graphics",
		Title:    "The GPU device was lost",
		Explain:  "Windows reset the graphics device — driver crash, overclock instability, or the GPU running out of memory. The game or server stops rendering and usually exits.",
		Fix:      "Remove GPU overclocks, update or clean-install the driver, and check temperatures. On servers with many mods, lower texture memory pressure.",
	},
	{
		ID:       "out-of-memory",
		Re:       regexp.MustCompile(`(?i)Out of memory|bad_alloc|Cannot allocate memory`),
		Severity: SeverityCritical,
		Category: "performance",
		Title:    "The process ran out of memory",
		Explain:  "The engine could not allocate more memory and will usually terminate or start dropping content.",
		Fix:      "Raise the server's memory limit, reduce map/object count, or trim mods. On client crashes, increase the Windows page file.",
	},
	{
		ID:       "exception-code",
		Re:       regexp.MustCompile(`(?i)Exception code[:\s]+(0x[0-9A-Fa-f]+)|Application crashed|Access violation|referenced memory at`),
		Severity: SeverityCritical,
		Category: "crash",
		Title:    "The game crashed",
		Explain:  "The engine hit a fatal error and stopped. The crash code points at the fault type, not necessarily at a mod.",
		Fix:      "Read the last lines before the crash: they usually name the mod or asset being loaded. Disable mods in halves to bisect, and check the .mdmp if one was written.",
	},
	{
		ID:       "script-error",
		Re:       regexp.MustCompile(`(?i)Script (?:error|exception)|SCRIPT\s*:\s*\(E\)`),
		Severity: SeverityError,
		Category: "scripting",
		Title:    "A script threw an error",
		Explain:  "A mod or mission script raised an error. The engine logs it and keeps going, so the feature that script drives silently stops working.",
		Fix:      "Look at the function name in the message: it belongs to the mod with that prefix. Update that mod, or disable it if the error repeats every frame.",
	},
	{
		ID:       "cannot-register-object",
		Re:       regexp.MustCompile(`(?i)Cannot register object|Cannot create (?:object|entity)`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "An object could not be created",
		Explain:  "The engine refused to instantiate an object, usually because its config is incomplete or its model is missing.",
		Fix:      "Check the lines above for the object class name, then update or remove the mod providing it.",
	},
	{
		ID:       "central-economy",
		Re:       regexp.MustCompile(`(?i)central economy|CE\b.*(?:error|fail)|types\.xml|cfgeconomycore`),
		Severity: SeverityCritical,
		Category: "economy",
		Title:    "The central economy (loot) failed to load",
		Explain:  "Loot spawning depends on types.xml, cfgeconomycore.xml and the mapgroupproto. If these fail, items do not spawn or loot is wiped.",
		Fix:      "Validate your XML (well-formed, no duplicate class names, correct usage flags) and confirm the mission folder actually contains the economy files. Restore a known-good types.xml to test.",
	},
	{
		ID:       "central-economy-trace",
		Re:       regexp.MustCompile(`(?i)(?:^|!!!\s*)\[CE\]`),
		Severity: SeverityInfo,
		Category: "economy",
		Vanilla:  true,
		Title:    "Central economy bookkeeping",
		Explain:  "Loot respawn, group-link and container messages from the central economy. A running server writes these continuously; they are normal.",
		Fix:      "Nothing to do unless the line mentions a file that failed to load or a loot value mismatch you want to tune.",
	},
	{
		ID:       "engine-notable",
		Re:       regexp.MustCompile(`(?i)^!!!`),
		Severity: SeverityWarning,
		Category: "economy",
		Vanilla:  true,
		Title:    "Engine marked this line as notable",
		Explain:  "The engine prefixes lines it considers worth attention with `!!!`. Most come from the loot economy (removed points, spawn mismatches) and appear on stock servers too.",
		Fix:      "Read the text: if it names an event or container whose loot values disagree, adjust the event in cfgeventspawns.xml or the container in types.xml.",
	},
	{
		ID:       "persistence",
		Re:       regexp.MustCompile(`(?i)persistence|storage.*(?:fail|corrupt|error)|Cannot load.*\.(?:save|bin)`),
		Severity: SeverityCritical,
		Category: "storage",
		Title:    "Save/storage problem",
		Explain:  "The server could not read or write persistent data. Bases and character progress can be lost on the next restart.",
		Fix:      "Check disk space and write permissions on the profiles/storage folder, then restore the last good backup. Never copy a live storage folder while the server is running.",
	},
	{
		ID:       "battleye",
		Re:       regexp.MustCompile(`(?i)BattlEye.*(?:fail|error|cannot|unable)|BE (?:service|filter).*fail`),
		Severity: SeverityCritical,
		Category: "security",
		Title:    "BattlEye is not running correctly",
		Explain:  "The anti-cheat layer failed to start or lost contact. Players may be kicked on join, and the server offers no cheat protection.",
		Fix:      "Make sure the server can write to its BattlEye folder, that the BE service/ports are reachable (UDP 2344 by default), and that you did not rename the BE binaries.",
	},
	{
		ID:       "network-timeout",
		Re:       regexp.MustCompile(`(?i)Network.*timeout|connection (?:lost|closed|reset)|Steam.*(?:timeout|fail)`),
		Severity: SeverityWarning,
		Category: "network",
		Title:    "Network or Steam connectivity issue",
		Explain:  "A connection timed out. Occasional drops are normal; frequent ones point at the host or DDoS protection.",
		Fix:      "Check the host's network, firewall rules for the game ports, and whether Steam had an outage at that time.",
	},
	{
		ID:       "player-not-in-map",
		Re:       regexp.MustCompile(`(?i)is not in the map|Player .* not found in (?:map|world)|Teleport.*fail`),
		Severity: SeverityWarning,
		Category: "gameplay",
		Title:    "A player position could not be resolved",
		Explain:  "The engine could not place a player in the world — usually a transient state during connect or after a teleport.",
		Fix:      "Harmless in isolation. If it repeats for the same player, that character's save may be corrupted; test with a fresh character.",
	},
	{
		ID:       "mission-not-found",
		Re:       regexp.MustCompile(`(?i)Cannot find mission|Mission .* not found|mission not found|Unable to open mission`),
		Severity: SeverityCritical,
		Category: "startup",
		Title:    "The mission folder could not be loaded",
		Explain:  "The server was pointed at a mission that does not exist or is missing its init files.",
		Fix:      "Check the -mission= path (case-sensitive on Linux), and confirm the folder contains init.c and the map's economy files.",
	},
	{
		ID:       "content-not-owned",
		Re:       regexp.MustCompile(`(?i)downloadable content|DLC|You cannot play/edit this mission`),
		Severity: SeverityError,
		Category: "startup",
		Title:    "The mission depends on content that is not loaded",
		Explain:  "A required addon or DLC is missing, so the mission cannot run as configured.",
		Fix:      "Load the required mods in the same order as the mission expects, then restart.",
	},
	{
		ID:       "malformed-config",
		Re:       regexp.MustCompile(`(?i)Malformed|unexpected token|Error reading config|config\.bin.*(?:invalid|corrupt)`),
		Severity: SeverityError,
		Category: "mods",
		Title:    "A config file is malformed",
		Explain:  "The engine failed to parse a config. Anything after the syntax error is ignored, so config-dependent features vanish.",
		Fix:      "Validate the XML/config you last edited. A single missing brace silently truncates the rest of the file.",
	},
	{
		ID:       "login-state-error",
		Re:       regexp.MustCompile(`(?i)\[StateMachine\].*State Error`),
		Severity: SeverityError,
		Category: "network",
		Title:    "A player's login state machine failed",
		Explain:  "The connection state machine rejected a transition while a player was joining, usually because the Steam handshake or the character load did not finish.",
		Fix:      "The player normally reconnects fine. If it happens to everyone, check Steam connectivity and whether a mod overrides the login queue or player spawn.",
	},
	{
		ID:       "script-action-error",
		Re:       regexp.MustCompile(`(?i)::\s*\[ERROR\]\s*::`),
		Severity: SeverityError,
		Category: "scripting",
		Title:    "A script action reported an error",
		Explain:  "A gameplay action (take, craft, mix, …) refused to run and logged why. Only that action is affected.",
		Fix:      "Read the reason after the prefix. If it names a mod action, update that mod; stock action failures are usually harmless state mismatches.",
	},
	{
		ID:       "connection-lifecycle",
		Re:       regexp.MustCompile(`(?i)^\[(?:Disconnect|Connect|Login)\]`),
		Severity: SeverityInfo,
		Category: "lifecycle",
		Vanilla:  true,
		Title:    "Connection lifecycle",
		Explain:  "Step-by-step logging of a player joining or leaving. Several lines belong to a single connection.",
		Fix:      "Nothing to do.",
	},
	{
		ID:       "connection-block-detail",
		Re:       regexp.MustCompile(`(?i)^(?:charID|playerID|dpnid|steamID|uid|<LOAD EXISTING CHAR>)`),
		Severity: SeverityInfo,
		Category: "lifecycle",
		Vanilla:  true,
		Title:    "Connection block detail",
		Explain:  "Continuation lines of the connection block above: character, player and session ids.",
		Fix:      "Nothing to do.",
	},
	{
		ID:       "engine-subsystem",
		Re:       regexp.MustCompile(`(?i)^(?:WORLD|NETWORK|HIVE|VEHICLE|AI|ANIM|SOUND)\s*:`),
		Severity: SeverityInfo,
		Category: "engine",
		Vanilla:  true,
		Title:    "Engine subsystem trace",
		Explain:  "Periodic trace from an engine subsystem while the world runs.",
		Fix:      "Nothing to do.",
	},
	{
		ID:       "shutdown",
		Re:       regexp.MustCompile(`(?i)(?:Exiting|Shutdown|Server (?:stopped|terminated))|Mission (?:read|terminated)`),
		Severity: SeverityInfo,
		Category: "lifecycle",
		Title:    "Server lifecycle marker",
		Explain:  "Normal start/stop bookkeeping written by the engine.",
		Fix:      "Nothing to do — this is expected output.",
	},
	{
		ID:       "player-connected",
		Re:       regexp.MustCompile(`(?i)Player "[^"]+"(?: \([^)]*\))*\s+(?:is connected|disconnected)`),
		Severity: SeverityInfo,
		Category: "lifecycle",
		Vanilla:  true,
		Title:    "Player connection event",
		Explain:  "A player joined or left. Server RPTs repeat these; the ADM has the full picture.",
		Fix:      "Nothing to do.",
	},
}

var (
	reFallbackError   = regexp.MustCompile(`(?i)(?:^|\W)(?:ErrorMessage|Error|\[ERROR\]|ERROR)(?:\W|$)`)
	reFallbackWarning = regexp.MustCompile(`(?i)(?:^|\W)(?:Warning|\[WARNING\]|WARNING)(?:\W|$)`)
)

// FallbackSeverity classifies a message that matched no rule.
func FallbackSeverity(message string) Severity {
	if reFallbackError.MatchString(message) {
		return SeverityError
	}
	if reFallbackWarning.MatchString(message) {
		return SeverityWarning
	}
	return SeverityInfo
}

// ClassifyMessage returns the verdict for one engine message.
func ClassifyMessage(message string) Verdict {
	for i := range Rules {
		rule := &Rules[i]
		if m := rule.Re.FindStringSubmatch(message); m != nil {
			return Verdict{
				RuleID:   rule.ID,
				Severity: rule.Severity,
				Title:    fillCaptures(rule.Title, m),
				Explain:  fillCaptures(rule.Explain, m),
				Fix:      fillCaptures(rule.Fix, m),
				Category: rule.Category,
				Vanilla:  rule.Vanilla,
			}
		}
	}

	severity := FallbackSeverity(message)
	title := "Engine message"
	explain := "This message has no known explanation yet. It is shown verbatim so you can search for it or compare with a working server."
	fix := "Usually informational. Compare the count with a normal restart to see whether it is new."
	if severity == SeverityError {
		title = "Unrecognised error message"
		fix = "Copy the message text and search for it together with the mod name it mentions. If it started after an update, roll that update back to confirm."
	}
	return Verdict{
		Severity: severity,
		Title:    title,
		Explain:  explain,
		Fix:      fix,
		Category: "other",
	}
}

// fillCaptures substitutes {0}, {1}, … with capture groups (m[1] is group 1).
func fillCaptures(text string, m []string) string {
	if text == "" || m == nil || !strings.ContainsRune(text, '{') {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '{' && i+2 < len(text) && text[i+2] == '}' && text[i+1] >= '0' && text[i+1] <= '9' {
			idx := int(text[i+1]-'0') + 1
			if idx < len(m) {
				b.WriteString(m[idx])
				i += 2
				continue
			}
		}
		b.WriteByte(text[i])
	}
	return b.String()
}
