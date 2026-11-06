package webapp

import (
	"path/filepath"
	"testing"
)

func TestBanHintsRememberAndPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".dayzctl", "ban-hints.json")

	h := newBanHints(path)
	h.remember("93AC90BA57A8BA55D46E4F0903C4B2D8", "Mira")

	if got := h.player("93ac90ba57a8ba55d46e4f0903c4b2d8"); got != "Mira" {
		t.Fatalf("player = %q, want Mira", got)
	}

	// A restart reads the file back, so a ban issued before a deploy still
	// lands on the right row afterwards.
	reloaded := newBanHints(path)
	if got := reloaded.player("93ac90ba57a8ba55d46e4f0903c4b2d8"); got != "Mira" {
		t.Fatalf("reloaded player = %q, want Mira", got)
	}

	reloaded.forget("93ac90ba57a8ba55d46e4f0903c4b2d8")
	if got := reloaded.player("93ac90ba57a8ba55d46e4f0903c4b2d8"); got != "" {
		t.Fatalf("after forget player = %q, want empty", got)
	}
	if got := newBanHints(path).player("93ac90ba57a8ba55d46e4f0903c4b2d8"); got != "" {
		t.Fatalf("forget was not persisted: %q", got)
	}
}
