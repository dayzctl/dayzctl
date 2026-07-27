package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Integration tests run by default.
func TestBuildAndHelp(t *testing.T) {

	outPath := filepath.Join(os.TempDir(), "dayzctl-integ")
	// Build binary from module import path to avoid relative cwd issues
	cmd := exec.Command("go", "build", "-o", outPath, "github.com/kabroxiko/dayzctl/cmd/dayzctl")
	var bout, berr bytes.Buffer
	cmd.Stdout = &bout
	cmd.Stderr = &berr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build failed: %v\nstdout: %s\nstderr: %s", err, bout.String(), berr.String())
	}
	defer func() { _ = os.Remove(outPath) }()

	// Run --help and ensure it returns 0 and shows usage
	help := exec.Command(outPath, "--help")
	help.Env = append(os.Environ(), "DAYZCTL_SKIP_ROOT=1")
	var hout, herr bytes.Buffer
	help.Stdout = &hout
	help.Stderr = &herr
	if err := help.Run(); err != nil {
		t.Fatalf("help failed: %v\nstdout: %s\nstderr: %s", err, hout.String(), herr.String())
	}
	if !bytes.Contains(hout.Bytes(), []byte("DayZ server management tool")) && !bytes.Contains(hout.Bytes(), []byte("Usage")) {
		t.Fatalf("help output didn't contain expected text; got:\n%s", hout.String())
	}
}

func TestBuildAndListWithConfig(t *testing.T) {

	outPath := filepath.Join(os.TempDir(), "dayzctl-integ")
	// Build binary from module import path to avoid relative cwd issues
	cmd := exec.Command("go", "build", "-o", outPath, "github.com/kabroxiko/dayzctl/cmd/dayzctl")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, string(out))
	}
	defer func() { _ = os.Remove(outPath) }()

	// Write minimal config
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
		t.Fatalf("failed to write config: %v", err)
	}

	// Run list with DAYZCTL_CONFIG pointing to our file
	listCmd := exec.Command(outPath, "list")
	listCmd.Env = append(os.Environ(), "DAYZCTL_CONFIG="+cfgPath, "DAYZCTL_SKIP_ROOT=1")
	var lout, lerr bytes.Buffer
	listCmd.Stdout = &lout
	listCmd.Stderr = &lerr
	if err := listCmd.Run(); err != nil {
		t.Fatalf("list failed: %v\nstdout: %s\nstderr: %s", err, lout.String(), lerr.String())
	}
	if !bytes.Contains(lout.Bytes(), []byte("testinst")) {
		t.Fatalf("list output didn't contain instance name; got:\n%s", lout.String())
	}
}
