package mods

import (
	"testing"

	"github.com/kabroxiko/dayzctl/internal/config"
)

func TestGetSymlinkNameFormats(t *testing.T) {
	m := New("/install", "/workshop")
	mod := config.ModRef{ID: "12345", Name: "My Fancy Mod"}
	got := m.getSymlinkName(mod)
	want := "@my_fancy_mod"
	if got != want {
		t.Fatalf("getSymlinkName: got=%q want=%q", got, want)
	}
}
