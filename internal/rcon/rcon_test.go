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
