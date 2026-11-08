package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

const adminConfigFixture = `# dayzctl test config
steam:
  username: "bot"
paths:
  base: "/srv/dayz"
instances:
  - name: test
    port: 2302
    template: dayzOffline.chernarusplus
server:
  password_admin: "s3cret"

web:
  enabled: true
  address: 127.0.0.1
  port: 8081
  auth:
    enabled: true
    # Steam accounts allowed into the UI
    steam:
      enabled: true
      admins:
        "76561198000000001": "Alice"
`

func writeAdminConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(adminConfigFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func adminServer(t *testing.T) (*Server, string) {
	t.Helper()
	path := writeAdminConfig(t)
	s := authServer(t, config.WebAuth{
		Enabled: true,
		Steam: config.WebSteam{
			Enabled: true,
			Admins:  map[string]string{"76561198000000001": "Alice"},
		},
	})
	s.SetConfigPath(path)
	return s, path
}

func loginAdmin(t *testing.T, s *Server) []*http.Cookie {
	t.Helper()
	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"password": {testAdminPassword}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303", rec.Code)
	}
	return rec.Result().Cookies()
}

func postAdminJSON(t *testing.T, s *Server, cookies []*http.Cookie, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admins", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestWriteSteamAdminsKeepsTheRestOfTheFile(t *testing.T) {
	path := writeAdminConfig(t)
	err := writeSteamAdmins(path, map[string]string{
		"76561198000000002": "Bob",
		"76561198000000003": "",
	})
	if err != nil {
		t.Fatalf("writeSteamAdmins: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{
		"# dayzctl test config",
		"# Steam accounts allowed into the UI",
		"76561198000000002",
		"Bob",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("config missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Alice") {
		t.Errorf("old admin survived the rewrite:\n%s", body)
	}
	if !strings.Contains(body, "\n  enabled: true\n") {
		t.Errorf("indentation changed away from two spaces:\n%s", body)
	}
	if !strings.Contains(body, "\n\nweb:\n") {
		t.Errorf("blank line before web: was lost:\n%s", body)
	}
	// The result must still parse as a config with the new list.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("rewritten config does not load: %v", err)
	}
	if got := cfg.Web.Auth.Steam.Admins; len(got) != 2 || got["76561198000000002"] != "Bob" {
		t.Errorf("admins = %v", got)
	}
}

func TestAdminEndpoints(t *testing.T) {
	s, path := adminServer(t)
	cookies := loginAdmin(t, s)

	// The list starts from the config.
	rec := doAuth(s.Handler(), http.MethodGet, "/api/admins", nil, cookies)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/admins = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`"writable":true`, `"steam":true`, `"steamID":"76561198000000001"`} {
		if !strings.Contains(body, want) {
			t.Errorf("admins payload = %s, want %s", body, want)
		}
	}

	// Grant a second admin; the config file follows.
	code, out := postAdminJSON(t, s, cookies, `{"steamID":"76561198000000002","name":"Bob","action":"add"}`)
	if code != http.StatusOK {
		t.Fatalf("add = %d (%s), want 200", code, out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "76561198000000002") {
		t.Errorf("config not updated:\n%s", data)
	}
	if _, ok := s.admins.allowed("76561198000000002"); !ok {
		t.Error("new admin is not live in the running server")
	}

	// Revoke it again.
	code, out = postAdminJSON(t, s, cookies, `{"steamID":"76561198000000002","action":"remove"}`)
	if code != http.StatusOK {
		t.Fatalf("remove = %d (%s), want 200", code, out)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "76561198000000002") {
		t.Errorf("admin not removed from config:\n%s", data)
	}
	if _, ok := s.admins.allowed("76561198000000002"); ok {
		t.Error("removed admin still allowed")
	}

	// Bad input is rejected.
	code, _ = postAdminJSON(t, s, cookies, `{"steamID":"12345","action":"add"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("bad id = %d, want 400", code)
	}
	code, _ = postAdminJSON(t, s, cookies, `{"steamID":"76561198000000002","action":"grant"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("bad action = %d, want 400", code)
	}

	// Anonymous callers cannot change anything.
	if rec := doAuth(s.Handler(), http.MethodPost, "/api/admins", nil, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST = %d, want 401", rec.Code)
	}
}

func TestAdminEndpointsWithoutConfigPath(t *testing.T) {
	s := authServer(t, config.WebAuth{
		Enabled: true,
		Steam:   config.WebSteam{Enabled: true, Admins: map[string]string{"76561198000000001": "Alice"}},
	})
	cookies := loginAdmin(t, s)

	code, out := postAdminJSON(t, s, cookies, `{"steamID":"76561198000000002","action":"add"}`)
	if code != http.StatusNotImplemented {
		t.Fatalf("add without config path = %d (%s), want 501", code, out)
	}
}

func TestAdminEndpointsRequireSteam(t *testing.T) {
	s := authServer(t, config.WebAuth{Enabled: true})
	s.SetConfigPath(writeAdminConfig(t))
	cookies := loginAdmin(t, s)

	code, out := postAdminJSON(t, s, cookies, `{"steamID":"76561198000000002","action":"add"}`)
	if code != http.StatusConflict {
		t.Fatalf("add with steam off = %d (%s), want 409", code, out)
	}
}

func TestDeniedSteamSignInBecomesPending(t *testing.T) {
	s := authServer(t, steamAuth())
	s.steamEndpoint = "https://steam.example/openid/login"
	s.verifySteam = func(context.Context, url.Values) (string, error) {
		return "76561198000000002", nil
	}
	state, stateCookie, _ := steamStart(t, s, "/")
	if rec := steamCallback(s, state, stateCookie); rec.Code != http.StatusForbidden {
		t.Fatalf("callback = %d, want 403", rec.Code)
	}

	// The refused account is queued so the operator can approve it.
	cookies := loginAdmin(t, s)
	rec := doAuth(s.Handler(), http.MethodGet, "/api/admins", nil, cookies)
	body := rec.Body.String()
	if !strings.Contains(body, `"steamID":"76561198000000002"`) || !strings.Contains(body, `"attempts":1`) {
		t.Fatalf("pending payload = %s", body)
	}
}
