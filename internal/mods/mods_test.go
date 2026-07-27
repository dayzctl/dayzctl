package mods

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMetaFile_NameExtraction(t *testing.T) {
	tmp := t.TempDir()

	cases := []struct {
		name     string
		content  string
		expected string
	}{
		{"double-quoted", `name = "My Mod Name";`, "My_Mod_Name"},
		{"single-quoted", `name = 'Other Mod';`, "Other_Mod"},
		{"unquoted", `name = SomeMod;`, "SomeMod"},
	}

	m := New("/install", "/workshop")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := filepath.Join(tmp, tc.name+".meta")
			if err := os.WriteFile(f, []byte(tc.content), 0644); err != nil {
				t.Fatalf("failed to write meta file: %v", err)
			}

			got := m.parseMetaFile(f)
			if got != tc.expected {
				t.Fatalf("unexpected parsed name: got=%q want=%q", got, tc.expected)
			}
		})
	}
}
