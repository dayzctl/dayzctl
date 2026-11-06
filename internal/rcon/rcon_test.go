package rcon

import (
	"encoding/binary"
	"testing"
)

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
