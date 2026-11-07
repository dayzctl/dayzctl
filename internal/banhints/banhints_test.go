package banhints

import (
	"path/filepath"
	"testing"
)

func TestRememberPersistAndForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ban-hints.json")

	s := New(path)
	s.Remember("0123456789ABCDEF0123456789ABCDEF", "Mira")

	if got := s.Player("0123456789abcdef0123456789abcdef"); got != "Mira" {
		t.Fatalf("player = %q, want Mira", got)
	}

	// A restart reads the file back, so a ban issued before a deploy still
	// lands on the right row afterwards.
	reloaded := New(path)
	if got := reloaded.Player("0123456789abcdef0123456789abcdef"); got != "Mira" {
		t.Fatalf("reloaded player = %q, want Mira", got)
	}

	reloaded.Forget("0123456789abcdef0123456789abcdef")
	if got := reloaded.Player("0123456789abcdef0123456789abcdef"); got != "" {
		t.Fatalf("after forget player = %q, want empty", got)
	}
	if got := New(path).Player("0123456789abcdef0123456789abcdef"); got != "" {
		t.Fatalf("forget was not persisted: %q", got)
	}
}

func TestGUIDSByName(t *testing.T) {
	s := New("")
	s.Remember("0123456789abcdef0123456789abcdef", "Mira")
	s.Remember("76561198000000005", `"MIRA"`)

	guids := s.GUIDSByName(" mira ")
	if len(guids) != 2 {
		t.Fatalf("guids = %v, want 2 entries", guids)
	}
	if got := s.GUIDSByName("Nobody"); len(got) != 0 {
		t.Fatalf("guids for unknown name = %v, want none", got)
	}
}

func TestPruneDropsVanishedBans(t *testing.T) {
	s := New("")
	s.Remember("0123456789abcdef0123456789abcdef", "Mira")
	s.Remember("76561198000000005", "Ox")

	s.Prune(map[string]struct{}{"0123456789abcdef0123456789abcdef": {}})

	if got := s.Player("76561198000000005"); got != "" {
		t.Fatalf("pruned hint still present: %q", got)
	}
	if got := s.Player("0123456789abcdef0123456789abcdef"); got != "Mira" {
		t.Fatalf("live hint lost: %q", got)
	}
}
