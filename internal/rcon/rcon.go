package rcon

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/internal/logger"
)

// ErrNoResponse means the command was written but the server sent nothing
// back. BattlEye gives no delivery guarantee over UDP, and some builds simply
// do not answer kick/ban, so callers may treat this as "sent, unconfirmed".
var ErrNoResponse = errors.New("server did not respond")

// Client implements BattlEye RCon protocol matching bercon-cli
type Client struct {
	conn     *net.UDPConn
	password string
	seq      byte
	port     int
}

// New creates a new RCON client
func New(port int, password string) *Client {
	return &Client{
		password: password,
		seq:      0,
		port:     port,
	}
}

// Connect establishes a UDP connection
func (c *Client) Connect(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("failed to resolve address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	c.conn = conn

	if err := c.conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return fmt.Errorf("failed to set deadline: %w", err)
	}

	return nil
}

// Close closes the connection
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// Open connects and logs in, leaving the session open so several commands can
// run on it. Use it when one action needs more than one command (the web UI
// looks a player up and then kicks them): some servers only answer commands
// reliably within the session that was just authenticated.
func (c *Client) Open() error {
	if err := c.Connect(c.port); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	if err := c.Login(); err != nil {
		_ = c.Close()
		return fmt.Errorf("login failed: %w", err)
	}
	return nil
}

// Login authenticates with the server
func (c *Client) Login() error {
	payload := append([]byte{0x00}, []byte(c.password)...)
	packet := c.buildPacket(payload)

	if _, err := c.conn.Write(packet); err != nil {
		return fmt.Errorf("failed to send login: %w", err)
	}

	buf := make([]byte, 1024)
	n, err := c.conn.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read login response: %w", err)
	}

	if !c.verifyHeader(buf[:n]) {
		return fmt.Errorf("invalid packet header")
	}

	if n < 8 {
		return fmt.Errorf("response too short")
	}

	response := buf[7:n]
	if len(response) < 2 {
		return fmt.Errorf("invalid response")
	}

	if response[1] != 0x01 {
		return fmt.Errorf("login failed: incorrect password")
	}

	return nil
}

// maxResponsePackets bounds a fragmented RCON response so a misbehaving
// server cannot make the client spin forever.
const maxResponsePackets = 1024

// SendCommand sends a command and returns the response.
//
// BattlEye may split a response over several datagrams: every packet except
// the last carries type 0x02, the last one carries 0x01. Reading a single
// datagram (as this used to) silently dropped the whole `players`/`bans`
// output whenever the server fragmented it, which is why a kick by id worked
// from the CLI while a kick by name from the web UI could not find the player.
func (c *Client) SendCommand(command string) (string, error) {
	payload := append([]byte{0x01, c.seq}, []byte(command)...)
	c.seq++

	packet := c.buildPacket(payload)
	if _, err := c.conn.Write(packet); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	var out strings.Builder
	buf := make([]byte, 4096)
	for packets := 0; ; packets++ {
		// A server that never sends the final 0x01 packet must not hang the
		// handler or grow memory without bound.
		if packets >= maxResponsePackets {
			return "", fmt.Errorf("response too large (%d packets)", packets)
		}
		// The connection deadline set in Connect only covers the login; a
		// fragmented response can span several reads.
		if err := c.conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return "", fmt.Errorf("failed to set deadline: %w", err)
		}

		n, err := c.conn.Read(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return "", fmt.Errorf("%w: %v", ErrNoResponse, err)
			}
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		if !c.verifyHeader(buf[:n]) {
			return "", fmt.Errorf("invalid packet header")
		}

		if n < 8 {
			return "", fmt.Errorf("response too short")
		}

		response := buf[7:n]
		if len(response) < 2 {
			return "", fmt.Errorf("invalid response")
		}

		// Type 0x00 is a login-style packet, never a command answer. A
		// leftover one (some servers answer the login twice) must not be
		// mistaken for the command output — that used to make `players` look
		// empty even though the kick itself worked.
		if response[0] == 0x00 {
			continue
		}

		out.Write(response[2:])

		// 0x01 marks the last packet, 0x02 that more will follow.
		if response[0] != 0x02 {
			break
		}
	}

	respStr := out.String()
	logger.Debug("RCON SendCommand: raw response", "cmd", command, "resp", respStr)
	return respStr, nil
}

// Send runs one command on a short-lived session.
func (c *Client) Send(command string) (string, error) {
	if err := c.Open(); err != nil {
		return "", err
	}
	defer func() {
		if err := c.Close(); err != nil {
			_ = err
		}
	}()

	return c.SendCommand(command)
}

// exec runs a command on the open session when there is one, otherwise on a
// short-lived connection (the CLI's one-command-per-client behaviour).
func (c *Client) exec(command string) (string, error) {
	if c.conn != nil {
		return c.SendCommand(command)
	}
	return c.Send(command)
}

// Run executes a raw command on the open session. When no session is open it
// behaves like Send (connect, log in, run, close).
func (c *Client) Run(command string) (string, error) {
	return c.exec(command)
}

// buildPacket builds a packet with BE header matching bercon-cli
func (c *Client) buildPacket(payload []byte) []byte {
	packet := make([]byte, 0, 7+len(payload))
	packet = append(packet, 'B', 'E')
	packet = append(packet, 0, 0, 0, 0)
	packet = append(packet, 0xFF)
	packet = append(packet, payload...)

	crcPayload := append([]byte{0xFF}, payload...)
	crc := crc32(crcPayload)
	binary.LittleEndian.PutUint32(packet[2:6], crc)

	return packet
}

// verifyHeader verifies the BE header
func (c *Client) verifyHeader(buf []byte) bool {
	if len(buf) < 7 {
		return false
	}

	if buf[0] != 'B' || buf[1] != 'E' {
		return false
	}

	if buf[6] != 0xFF {
		return false
	}

	payload := buf[7:]
	crcPayload := append([]byte{0xFF}, payload...)
	expected := binary.LittleEndian.Uint32(buf[2:6])
	actual := crc32(crcPayload)

	return expected == actual
}

// crc32 calculates CRC32 checksum matching bercon-cli
func crc32(data []byte) uint32 {
	var crc uint32 = 0xFFFFFFFF
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xEDB88320
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

// ============================================================================
// HIGH-LEVEL RCON COMMANDS
// ============================================================================

// Players returns the list of connected players.
func (c *Client) Players() ([]Player, error) {
	resp, err := c.PlayersRaw()
	if err != nil {
		return nil, err
	}
	return parsePlayers(resp), nil
}

// PlayersRaw returns the unparsed `players` response. Callers use it to log or
// tolerate a column layout the parser does not recognise.
func (c *Client) PlayersRaw() (string, error) {
	resp, err := c.exec("players")
	if err != nil {
		return "", err
	}
	logger.Debug("RCON Players: raw response", "resp", resp)
	return resp, nil
}

// parsePlayers extracts the player rows from a `players` response.
func parsePlayers(resp string) []Player {
	// Normalize and split into lines
	lines := strings.Split(strings.TrimSpace(resp), "\n")
	var players []Player

	// Find the start of the players table (if present)
	start := 0
	for i, l := range lines {
		if strings.Contains(l, "Players on server:") {
			start = i + 1
			break
		}
	}

	for _, line := range lines[start:] {
		// stop at footer
		if strings.Contains(line, "players in total") {
			break
		}
		if pl, ok := parsePlayerLine(line); ok {
			players = append(players, pl)
		}
	}

	return players
}

// parsePlayerLine parses a single line of `players` output into a Player.
// Returns (Player, true) on success, (Player{}, false) when the line should be skipped.
func parsePlayerLine(line string) (Player, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Player{}, false
	}
	// skip header/separator lines
	if strings.Contains(line, "[#]") || strings.Contains(line, "---") {
		return Player{}, false
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Player{}, false
	}

	id, err := strconv.Atoi(fields[0])
	if err != nil {
		return Player{}, false
	}

	var p Player
	p.ID = id

	// Common formats:
	// 1) id ip:port ping guid name...
	// 2) id ip port ping guid name...
	if strings.Contains(fields[1], ":") {
		// ip:port
		hostport := strings.SplitN(fields[1], ":", 2)
		p.IP = hostport[0]
		if len(hostport) > 1 {
			p.Port = hostport[1]
		}
		if len(fields) >= 5 {
			p.Ping = fields[2]
			p.GUID = fields[3]
			p.Name = strings.Join(fields[4:], " ")
		} else if len(fields) == 4 {
			p.Ping = fields[2]
			p.GUID = fields[3]
		}
		return p, true
	}

	// fallback: assume space-separated ip and port
	if len(fields) >= 6 {
		p.IP = fields[1]
		p.Port = fields[2]
		p.Ping = fields[3]
		p.GUID = fields[4]
		p.Name = strings.Join(fields[5:], " ")
		return p, true
	}

	// if we only have minimal columns, try best-effort mapping
	if len(fields) == 5 {
		p.IP = fields[1]
		p.Port = ""
		p.Ping = fields[2]
		p.GUID = fields[3]
		p.Name = fields[4]
		return p, true
	}

	return Player{}, false
}

// Kick kicks a player by ID
func (c *Client) Kick(playerID int, reason string) (string, error) {
	if reason != "" {
		return c.exec(fmt.Sprintf("kick %d %s", playerID, reason))
	}
	return c.exec(fmt.Sprintf("kick %d", playerID))
}

// Ban bans a player by ID
func (c *Client) Ban(playerID int, minutes int, reason string) (string, error) {
	if reason != "" {
		return c.exec(fmt.Sprintf("ban %d %d %s", playerID, minutes, reason))
	}
	return c.exec(fmt.Sprintf("ban %d %d", playerID, minutes))
}

// Bans returns the server's ban list.
func (c *Client) Bans() ([]Ban, error) {
	resp, err := c.exec("bans")
	if err != nil {
		return nil, err
	}
	logger.Debug("RCON Bans: raw response", "resp", resp)

	lines := strings.Split(strings.TrimSpace(resp), "\n")

	// Find the start of the ban table (if present).
	start := 0
	for i, l := range lines {
		if strings.Contains(l, "Bans on server:") {
			start = i + 1
			break
		}
	}

	var bans []Ban
	for _, line := range lines[start:] {
		if b, ok := parseBanLine(line); ok {
			bans = append(bans, b)
		}
	}
	return bans, nil
}

// parseBanLine parses a single line of `bans` output into a Ban. The columns
// seen in the wild are "#", "GUID/IP", "time left" and "reason" in that order,
// but the time-left column is free-form ("0 (perm)", "1440", "12:00"), so the
// numbers are picked out rather than read positionally.
func parseBanLine(line string) (Ban, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.Contains(line, "[#]") || strings.Contains(line, "---") {
		return Ban{}, false
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Ban{}, false
	}

	id, err := strconv.Atoi(fields[0])
	if err != nil {
		return Ban{}, false
	}

	ban := Ban{ID: id}
	var reason []string
	haveTime := false

	for _, field := range fields[1:] {
		switch {
		case isGUID(field):
			ban.GUID = field
		case isIP(field):
			ban.IP = field
		case strings.Contains(strings.ToLower(field), "perm"):
			ban.Minutes = 0
			haveTime = true
		case isNumber(field) && !haveTime:
			ban.Minutes, _ = strconv.Atoi(field)
			haveTime = true
		default:
			reason = append(reason, field)
		}
	}

	ban.Reason = strings.Join(reason, " ")
	return ban, true
}

// isGUID reports whether a field looks like a player GUID. DayZ's ban list
// prints BattlEye's 32-hex GUID (e.g. 0123456789abcdef0123456789abcdef), while
// `players` and older ADM logs use a 64-bit SteamID.
func isGUID(field string) bool {
	if len(field) == 32 {
		return isHex(field)
	}
	return len(field) >= 17 && strings.HasPrefix(field, "7656119") && isNumber(field)
}

func isHex(field string) bool {
	for _, r := range field {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return field != ""
}

// isIP reports whether a field looks like an IPv4 address, with or without a
// port suffix.
func isIP(field string) bool {
	host := field
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

func isNumber(field string) bool {
	if field == "" {
		return false
	}
	for _, r := range field {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Unban removes a ban by its ban-list id (the first column of `bans`), not by
// the player's session id. DayZ's BattlEye build answers "Unknown command" to
// this, so prefer RemoveBan, which verifies the result.
func (c *Client) Unban(banID int) (string, error) {
	return c.exec(fmt.Sprintf("unban %d", banID))
}

// ErrBanNotRemoved means every known unban spelling was sent and the entry was
// still in the ban list afterwards.
var ErrBanNotRemoved = errors.New("no known unban command changed the ban list")

// UnbanCommands lists the spellings different BattlEye builds accept for
// removing a ban: the documented `unban <id>`, DayZ's game-side `#unban`, and
// `removeBan`, each for the ban-list id and for the player GUID.
func UnbanCommands(ban Ban) []string {
	cmds := []string{
		fmt.Sprintf("unban %d", ban.ID),
		fmt.Sprintf("#unban %d", ban.ID),
		fmt.Sprintf("removeBan %d", ban.ID),
	}
	if ban.GUID != "" {
		cmds = append(cmds,
			"unban "+ban.GUID,
			"#unban "+ban.GUID,
			"removeBan "+ban.GUID,
		)
	}
	return cmds
}

// BanStillListed reports whether the ban list still contains target. GUID and
// IP are matched first because ban-list ids shift after any removal, so an id
// alone can point at a different ban.
func BanStillListed(bans []Ban, target Ban) bool {
	for _, ban := range bans {
		if target.GUID != "" && strings.EqualFold(strings.TrimSpace(ban.GUID), strings.TrimSpace(target.GUID)) {
			return true
		}
		if target.IP != "" && strings.EqualFold(strings.TrimSpace(ban.IP), strings.TrimSpace(target.IP)) {
			return true
		}
	}
	if target.GUID == "" && target.IP == "" {
		for _, ban := range bans {
			if ban.ID == target.ID {
				return true
			}
		}
	}
	return false
}

// RemoveBan removes ban from the server, trying the spellings in UnbanCommands
// until one of them actually changes the ban list. Nothing is reported as
// removed unless a fresh `bans` read confirms it; a missing reply does not
// count as failure either, since some builds apply the command silently.
//
// It returns the per-attempt replies so the caller can show what the server
// said, and ErrBanNotRemoved (wrapping any transport error) when nothing
// worked.
func (c *Client) RemoveBan(ban Ban) ([]string, error) {
	var attempts []string
	var lastErr error

	for _, cmd := range UnbanCommands(ban) {
		reply, err := c.Run(cmd)
		switch {
		case err == nil:
			attempts = append(attempts, fmt.Sprintf("%s -> %q", cmd, strings.TrimSpace(reply)))
		case errors.Is(err, ErrNoResponse):
			attempts = append(attempts, cmd+" -> (no reply)")
		default:
			attempts = append(attempts, fmt.Sprintf("%s -> error: %v", cmd, err))
		}

		// The ban list is the authority, not the reply.
		bans, listErr := c.Bans()
		if listErr != nil {
			lastErr = listErr
			continue
		}
		if !BanStillListed(bans, ban) {
			return attempts, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr != nil && !errors.Is(lastErr, ErrNoResponse) {
		return attempts, fmt.Errorf("%w: %v", ErrBanNotRemoved, lastErr)
	}
	return attempts, ErrBanNotRemoved
}

// Say sends a message to all players
func (c *Client) Say(message string) (string, error) {
	return c.exec(fmt.Sprintf("say -1 %s", message))
}

// Shutdown requests a graceful server shutdown via the BattlEye "#shutdown"
// admin command. Unlike killing the process (e.g. via SIGTERM/SIGKILL from
// systemd), this lets the DayZ server save the world/player state and exit
// cleanly before the process terminates.
func (c *Client) Shutdown() (string, error) {
	return c.exec("#shutdown")
}

// Player represents a connected player
type Player struct {
	ID   int
	IP   string
	Port string
	Ping string
	GUID string
	Name string
}

// Ban represents one entry of the server ban list.
type Ban struct {
	// ID is the ban's index, which is what `unban` takes.
	ID int `json:"id"`
	// GUID is the banned SteamID when the ban was placed on one.
	GUID string `json:"guid,omitempty"`
	// IP is set when the ban was placed on an address instead.
	IP string `json:"ip,omitempty"`
	// Minutes is the time left; 0 means permanent.
	Minutes int    `json:"minutes"`
	Reason  string `json:"reason,omitempty"`
}
