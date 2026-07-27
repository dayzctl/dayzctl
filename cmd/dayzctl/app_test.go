package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	//_"github.com/kabroxiko/dayz/dayzctl/internal/config"
)

func captureStdout(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	f()
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

func TestModsHelpAndInstanceFirst(t *testing.T) {
	// prepare minimal config
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	cfg := `steam:
  username: testuser
paths:
  base: ` + tmp + `
instances:
  - name: testinst
    port: 2302
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	// ensure tests use test-only override
	t.Setenv("DAYZCTL_SKIP_ROOT", "1")
	t.Setenv("DAYZCTL_CONFIG", cfgPath)

	app := NewApp()

	out := captureStdout(func() {
		if err := app.Run([]string{"dayzctl", "mods"}); err != nil {
			t.Fatalf("mods help run failed: %v", err)
		}
	})
	if out == "" {
		t.Fatalf("expected help output for mods, got empty")
	}

	// run instance-first command
	if err := app.Run([]string{"dayzctl", "mods", "testinst", "list"}); err != nil {
		t.Fatalf("mods instance-first list failed: %v", err)
	}
}
