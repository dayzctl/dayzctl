package rcon

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/dayzctl/dayzctl/internal/logger"
)

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
	if c.conn != nil {
		return c.conn.Close()
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

// SendCommand sends a command and returns the response
func (c *Client) SendCommand(command string) (string, error) {
	payload := append([]byte{0x01, c.seq}, []byte(command)...)
	c.seq++

	packet := c.buildPacket(payload)
	if _, err := c.conn.Write(packet); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := c.conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if !c.verifyHeader(buf[:n]) {
		return "", fmt.Errorf("invalid packet header")
	}

	if n < 8 {
		return "", fmt.Errorf("response too short")
	}

	response := buf[7:n]
	if len(response) < 3 {
		logger.Debug("RCON SendCommand: response too short", "cmd", command, "len", len(response))
		return "", nil
	}

	if response[0] == 0x02 {
		return "", nil
	}

	respStr := string(response[2:])
	logger.Debug("RCON SendCommand: raw response", "cmd", command, "resp", respStr)
	return respStr, nil
}

// Send sends a command (simplified interface)
func (c *Client) Send(command string) (string, error) {
	if err := c.Connect(c.port); err != nil {
		return "", fmt.Errorf("connection failed: %w", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			_ = err
		}
	}()

	if err := c.Login(); err != nil {
		return "", fmt.Errorf("login failed: %w", err)
	}

	return c.SendCommand(command)
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

// Players returns the list of connected players
func (c *Client) Players() ([]Player, error) {
	resp, err := c.Send("players")
	if err != nil {
		return nil, err
	}
	logger.Debug("RCON Players: raw response", "resp", resp)

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

	return players, nil
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
		return c.Send(fmt.Sprintf("kick %d %s", playerID, reason))
	}
	return c.Send(fmt.Sprintf("kick %d", playerID))
}

// Ban bans a player by ID
func (c *Client) Ban(playerID int, minutes int, reason string) (string, error) {
	if reason != "" {
		return c.Send(fmt.Sprintf("ban %d %d %s", playerID, minutes, reason))
	}
	return c.Send(fmt.Sprintf("ban %d %d", playerID, minutes))
}

// Unban unbans a player by ID
func (c *Client) Unban(playerID int) (string, error) {
	return c.Send(fmt.Sprintf("unban %d", playerID))
}

// Say sends a message to all players
func (c *Client) Say(message string) (string, error) {
	return c.Send(fmt.Sprintf("say -1 %s", message))
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
