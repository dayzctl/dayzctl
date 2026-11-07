package webapp

import (
	"path/filepath"
	"testing"
)

func TestBanHintsRememberAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dayzctl", "ban-hints.json")

	h := newBanHints(path)
	h.remember("0123456789ABCDEF0123456789ABCDEF", "Mira")

	if got := h.player("0123456789abcdef0123456789abcdef"); got != "Mira" {
		t.Fatalf("player = %q, want Mira", got)
	}

	// A restart reads the file back, so a ban issued before a deploy still
	// lands on the right row afterwards.
	reloaded := newBanHints(path)
	if got := reloaded.player("0123456789abcdef0123456789abcdef"); got != "Mira" {
		t.Fatalf("reloaded player = %q, want Mira", got)
	}

	reloaded.forget("0123456789abcdef0123456789abcdef")
	if got := reloaded.player("0123456789abcdef0123456789abcdef"); got != "" {
		t.Fatalf("after forget player = %q, want empty", got)
	}
	if got := newBanHints(path).player("0123456789abcdef0123456789abcdef"); got != "" {
		t.Fatalf("forget was not persisted: %q", got)
	}
}
