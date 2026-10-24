package generate

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/dayzctl/dayzctl/internal/config"
)

func TestBuildServerDataUsesEffectivePasswords(t *testing.T) {
	cfg := &config.ServerConfig{
		Server: config.Server{Password: "serverjoin", PasswordAdmin: "serveradmin"},
		Instances: []config.Instance{
			{Name: "plain", Port: 2302, Enabled: true},
			{Name: "override", Port: 2303, Enabled: true, Password: "instjoin", PasswordAdmin: "instadmin"},
		},
	}

	plain := buildServerData(cfg, cfg.Instances[0])
	if plain.Password != "serverjoin" || plain.PasswordAdmin != "serveradmin" {
		t.Fatalf("plain pass/login = %q/%q, want serverjoin/serveradmin", plain.Password, plain.PasswordAdmin)
	}

	override := buildServerData(cfg, cfg.Instances[1])
	if override.Password != "instjoin" || override.PasswordAdmin != "instadmin" {
		t.Fatalf("override pass/login = %q/%q, want instjoin/instadmin", override.Password, override.PasswordAdmin)
	}
}

func TestServerTemplateRendersPasswordAdmin(t *testing.T) {
	cfg := &config.ServerConfig{
		Server: config.Server{PasswordAdmin: "serveradmin"},
		Instances: []config.Instance{
			{Name: "plain", Port: 2302, Enabled: true},
			{Name: "override", Port: 2303, Enabled: true, PasswordAdmin: "instadmin"},
		},
	}

	tmpl, err := template.New("serverDZ").Parse(serverDZTemplate)
	if err != nil {
		t.Fatalf("parse template: %v", err)
	}

	for _, tc := range []struct {
		inst config.Instance
		want string
	}{
		{cfg.Instances[0], `passwordAdmin = "serveradmin";`},
		{cfg.Instances[1], `passwordAdmin = "instadmin";`},
	} {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, buildServerData(cfg, tc.inst)); err != nil {
			t.Fatalf("execute template for %s: %v", tc.inst.Name, err)
		}
		if !strings.Contains(buf.String(), tc.want) {
			t.Fatalf("rendered config for %s missing %q:\n%s", tc.inst.Name, tc.want, buf.String())
		}
	}
}
