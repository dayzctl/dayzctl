package rcon

import (
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// fakeUDPServer answers the login with success and the next command with the
// given response packets.
func fakeUDPServer(t *testing.T, packets [][]byte) (*Client, <-chan string) {
	t.Helper()

	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	commands := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		server := New(0, "")

		// Login: reply type 0x00, status 0x01 (success). Sent twice like some
		// servers do, so the command reader has to skip the leftover.
		if _, from, err := conn.ReadFromUDP(buf); err == nil {
			reply := server.buildPacket([]byte{0x00, 0x01})
			_, _ = conn.WriteToUDP(reply, from)
			_, _ = conn.WriteToUDP(reply, from)
		}

		// Command: capture it, then send the canned fragments.
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		payload := buf[7:n]
		commands <- string(payload[2:])
		for _, pkt := range packets {
			_, _ = conn.WriteToUDP(server.buildPacket(pkt), from)
		}
	}()

	c := New(conn.LocalAddr().(*net.UDPAddr).Port, "secret")
	return c, commands
}

func TestSendCommandAssemblesFragmentedResponse(t *testing.T) {
	c, commands := fakeUDPServer(t, [][]byte{
		append([]byte{0x02, 0x00}, []byte("Players on server:\n")...),
		append([]byte{0x02, 0x00}, []byte("0  127.0.0.1:2304  42  76561198000000005  Mira\n")...),
		append([]byte{0x01, 0x00}, []byte("(1 players in total)")...),
	})

	resp, err := c.Send("players")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	want := "Players on server:\n0  127.0.0.1:2304  42  76561198000000005  Mira\n(1 players in total)"
	if resp != want {
		t.Fatalf("response = %q, want %q", resp, want)
	}

	select {
	case cmd := <-commands:
		if cmd != "players" {
			t.Errorf("command = %q, want %q", cmd, "players")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server never received the command")
	}
}

func TestBuildPacketAndVerifyHeader(t *testing.T) {
	c := New(2302, "secret")
	payload := append([]byte{0x01, c.seq}, []byte("status")...)

	packet := c.buildPacket(payload)
	if !c.verifyHeader(packet) {
		t.Fatalf("verifyHeader returned false for built packet")
	}

	expected := crc32(append([]byte{0xFF}, payload...))
	actual := binary.LittleEndian.Uint32(packet[2:6])
	if expected != actual {
		t.Fatalf("crc mismatch: expected=0x%x actual=0x%x", expected, actual)
	}
}

func TestVerifyHeaderDetectsCorruption(t *testing.T) {
	c := New(2302, "secret")
	payload := append([]byte{0x01, c.seq}, []byte("status")...)
	packet := c.buildPacket(payload)

	// Corrupt a byte in payload area
	packet[10] ^= 0xFF
	if c.verifyHeader(packet) {
		t.Fatalf("verifyHeader should have failed for corrupted packet")
	}
}

func TestParseBanLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantID   int
		wantGUID string
		wantIP   string
		wantMin  int
		wantWhy  string
	}{
		{
			name:     "permanent guid ban",
			line:     `0  76561198012345678  0 (perm)  cheating`,
			wantOK:   true,
			wantID:   0,
			wantGUID: "76561198012345678",
			wantWhy:  "cheating",
		},
		{
			// DayZ ban lists print BattlEye's 32-hex GUID, not a SteamID.
			name:     "battleye hex guid",
			line:     `0  0123456789abcdef0123456789abcdef  59  griefing`,
			wantOK:   true,
			wantID:   0,
			wantGUID: "0123456789abcdef0123456789abcdef",
			wantMin:  59,
			wantWhy:  "griefing",
		},
		{
			name:    "timed ip ban",
			line:    `4  203.0.113.9:2304  1440  griefing`,
			wantOK:  true,
			wantID:  4,
			wantIP:  "203.0.113.9:2304",
			wantMin: 1440,
			wantWhy: "griefing",
		},
		{
			name:   "header",
			line:   `[#] [GUID/IP] [Time left] [Reason]`,
			wantOK: false,
		},
		{
			name:   "separator",
			line:   `---`,
			wantOK: false,
		},
		{
			name:   "empty",
			line:   `   `,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ban, ok := parseBanLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if ban.ID != tc.wantID {
				t.Errorf("id = %d, want %d", ban.ID, tc.wantID)
			}
			if ban.GUID != tc.wantGUID {
				t.Errorf("guid = %q, want %q", ban.GUID, tc.wantGUID)
			}
			if ban.IP != tc.wantIP {
				t.Errorf("ip = %q, want %q", ban.IP, tc.wantIP)
			}
			if ban.Minutes != tc.wantMin {
				t.Errorf("minutes = %d, want %d", ban.Minutes, tc.wantMin)
			}
			if ban.Reason != tc.wantWhy {
				t.Errorf("reason = %q, want %q", ban.Reason, tc.wantWhy)
			}
		})
	}
}

func TestUnbanCommands(t *testing.T) {
	ban := Ban{ID: 7, GUID: "0123456789abcdef0123456789abcdef"}

	got := UnbanCommands(ban)
	want := []string{
		"unban 7",
		"#unban 7",
		"removeBan 7",
		"unban 0123456789abcdef0123456789abcdef",
		"#unban 0123456789abcdef0123456789abcdef",
		"removeBan 0123456789abcdef0123456789abcdef",
	}
	if len(got) != len(want) {
		t.Fatalf("UnbanCommands = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UnbanCommands[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Without a GUID there is nothing extra to try.
	if cmds := UnbanCommands(Ban{ID: 2}); len(cmds) != 3 {
		t.Fatalf("UnbanCommands(no guid) = %v, want 3 attempts", cmds)
	}
}

func TestBanStillListed(t *testing.T) {
	target := Ban{ID: 0, GUID: "0123456789abcdef0123456789abcdef"}

	tests := []struct {
		name string
		bans []Ban
		want bool
	}{
		{
			name: "same guid",
			bans: []Ban{{ID: 5, GUID: "0123456789ABCDEF0123456789ABCDEF"}},
			want: true,
		},
		{
			name: "shifted ids but guid gone",
			bans: []Ban{{ID: 0, GUID: "76561198000000005"}},
			want: false,
		},
		{
			name: "empty list",
			bans: nil,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := BanStillListed(tc.bans, target); got != tc.want {
				t.Fatalf("BanStillListed = %v, want %v", got, tc.want)
			}
		})
	}

	// Id-only comparison is used when the target carries no GUID or IP.
	if !BanStillListed([]Ban{{ID: 3}}, Ban{ID: 3}) {
		t.Fatal("BanStillListed should match on id when there is no GUID/IP")
	}
	if BanStillListed([]Ban{{ID: 4}}, Ban{ID: 3}) {
		t.Fatal("BanStillListed matched the wrong id")
	}
	// ... but a GUID target must not match a different ban that happens to
	// reuse its old id.
	if BanStillListed([]Ban{{ID: 0, GUID: "ffffffffffffffffffffffffffffffff"}}, target) {
		t.Fatal("BanStillListed matched on id although the target has a GUID")
	}
}
