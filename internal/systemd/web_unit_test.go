package systemd

import (
	"strings"
	"testing"
)

func TestRenderWebUnit(t *testing.T) {
	content, err := renderWebUnit(WebData{
		BinaryPath: "/usr/local/bin/dayzctl",
		Address:    "127.0.0.1",
		Port:       8081,
		WorkingDir: "/srv/dayz",
	})
	if err != nil {
		t.Fatalf("renderWebUnit: %v", err)
	}

	unit := string(content)

	wantLines := []string{
		"ExecStart=/usr/local/bin/dayzctl web --addr 127.0.0.1 --port 8081",
		"WorkingDirectory=/srv/dayz",
		"Restart=on-failure",
		"ProtectSystem=full",
		"NoNewPrivileges=yes",
		"WantedBy=multi-user.target",
	}

	for _, want := range wantLines {
		if !strings.Contains(unit, want) {
			t.Errorf("unit is missing %q", want)
		}
	}

	if strings.Contains(unit, "{{") {
		t.Error("template placeholders were not substituted")
	}
	if !strings.HasPrefix(unit, "[Unit]") {
		t.Error("unit should start with the [Unit] section")
	}
}

func TestRenderWebUnitHonoursPort(t *testing.T) {
	content, err := renderWebUnit(WebData{BinaryPath: "dayzctl", Address: "0.0.0.0", Port: 9000, WorkingDir: "/srv/dayz"})
	if err != nil {
		t.Fatalf("renderWebUnit: %v", err)
	}
	if !strings.Contains(string(content), "--addr 0.0.0.0 --port 9000") {
		t.Errorf("expected the configured address and port, got:\n%s", content)
	}
}
