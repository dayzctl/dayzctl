package webapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

// testAdminPassword stands in for server.password_admin in these tests.
const testAdminPassword = "s3cret"

func authServer(t *testing.T, auth config.WebAuth) *Server {
	t.Helper()
	cfg := &config.ServerConfig{}
	cfg.Paths.Base = t.TempDir()
	cfg.Paths.InstallDir = t.TempDir()
	cfg.Server.PasswordAdmin = testAdminPassword
	cfg.Web = config.Web{Enabled: true, Address: "127.0.0.1", Port: 8081, Auth: auth}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func doAuth(h http.Handler, method, target string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("Accept", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthDisabledServesEverything(t *testing.T) {
	s := authServer(t, config.WebAuth{})

	if rec := doAuth(s.Handler(), http.MethodGet, "/api/health", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("health = %d, want 200", rec.Code)
	}
	if rec := doAuth(s.Handler(), http.MethodGet, "/", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("index = %d, want 200", rec.Code)
	}
}

func TestAuthRedirectsAndBlocks(t *testing.T) {
	s := authServer(t, config.WebAuth{Enabled: true})

	// The API answers 401 so the UI can react instead of parsing HTML.
	rec := doAuth(s.Handler(), http.MethodGet, "/api/health", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("api health = %d, want 401", rec.Code)
	}

	// Pages redirect to the sign-in form.
	rec = doAuth(s.Handler(), http.MethodGet, "/", nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("index = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("redirect = %q, want /login", loc)
	}

	// A deeper page in the app keeps its target.
	rec = doAuth(s.Handler(), http.MethodGet, "/?tab=map", nil, nil)
	if loc := rec.Header().Get("Location"); loc != "/login?next=%2F%3Ftab%3Dmap" {
		t.Fatalf("redirect = %q, want the encoded next target", loc)
	}
}

func TestAuthLoginFlow(t *testing.T) {
	s := authServer(t, config.WebAuth{Enabled: true})

	// The form itself is reachable without a session.
	if rec := doAuth(s.Handler(), http.MethodGet, "/login", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("login form = %d, want 200", rec.Code)
	}

	// Wrong password: 401 and no cookie.
	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"password": {"nope"}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("bad login set a cookie")
	}

	// Right password: redirect plus a session cookie that unlocks the API.
	rec = doAuth(s.Handler(), http.MethodPost, "/login", url.Values{
		"password": {testAdminPassword},
		"next":     {"/?tab=map"},
	}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?tab=map" {
		t.Fatalf("redirect = %q, want the next target", loc)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly {
		t.Fatalf("login cookie = %+v", cookies)
	}

	if rec := doAuth(s.Handler(), http.MethodGet, "/api/health", nil, cookies); rec.Code != http.StatusOK {
		t.Fatalf("health with session = %d, want 200", rec.Code)
	}

	// Signing out invalidates the cookie immediately.
	if rec := doAuth(s.Handler(), http.MethodPost, "/logout", nil, cookies); rec.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", rec.Code)
	}
	if rec := doAuth(s.Handler(), http.MethodGet, "/api/health", nil, cookies); rec.Code != http.StatusUnauthorized {
		t.Fatalf("health after logout = %d, want 401", rec.Code)
	}
}

func TestAuthSafeNextStaysLocal(t *testing.T) {
	for _, next := range []string{"", "/", "/?tab=map", "https://evil.example/x", "//evil.example", "javascript:alert(1)"} {
		got := safeNext(next)
		if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			t.Errorf("safeNext(%q) = %q, want a rooted local path", next, got)
		}
	}
}

func TestAuthHealthAdvertisesLogin(t *testing.T) {
	s := authServer(t, config.WebAuth{Enabled: true})
	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"password": {testAdminPassword}}, nil)
	cookies := rec.Result().Cookies()

	rec = doAuth(s.Handler(), http.MethodGet, "/api/health", nil, cookies)
	if !strings.Contains(rec.Body.String(), `"auth":true`) {
		t.Fatalf("health payload = %s, want auth:true", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"user":"admin"`) {
		t.Fatalf("health payload = %s, want user:admin", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Steam SSO
// ---------------------------------------------------------------------------

const testSteamID = "76561198000000001"

func steamAuth() config.WebAuth {
	return config.WebAuth{Enabled: true, Steam: config.WebSteam{
		Enabled: true,
		Admins:  map[string]string{testSteamID: "Alice"},
	}}
}

// steamServer builds a server whose Steam round-trip is stubbed and whose
// password form is off (Steam-only setup).
func steamServer(t *testing.T, auth config.WebAuth, verify steamVerifier) *Server {
	t.Helper()
	s := authServer(t, auth)
	s.adminPassword = ""
	s.steamEndpoint = "https://steam.example/openid/login"
	if verify != nil {
		s.verifySteam = verify
	}
	return s
}

// steamStart hits /auth/steam and returns the state, its cookie and the raw
// return_to URL.
func steamStart(t *testing.T, s *Server, next string) (string, *http.Cookie, string) {
	t.Helper()
	rec := doAuth(s.Handler(), http.MethodGet, "/auth/steam?next="+url.QueryEscape(next), nil, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("/auth/steam = %d, want 303", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("redirect URL: %v", err)
	}
	returnTo := loc.Query().Get("openid.return_to")
	if returnTo == "" {
		t.Fatalf("redirect %q has no openid.return_to", loc)
	}
	parsed, err := url.Parse(returnTo)
	if err != nil {
		t.Fatalf("return_to: %v", err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatalf("return_to %q has no state", returnTo)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == steamStateCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("/auth/steam did not set the state cookie")
	}
	return state, cookie, returnTo
}

func steamCallback(s *Server, state string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	target := "/auth/steam/callback?state=" + url.QueryEscape(state) +
		"&next=%2F%3Ftab%3Dmap&openid.mode=id_res" +
		"&openid.claimed_id=" + url.QueryEscape("https://steamcommunity.com/openid/id/"+testSteamID)
	return doAuth(s.Handler(), http.MethodGet, target, nil, cookies)
}

func TestSteamLoginRedirectsToSteam(t *testing.T) {
	s := steamServer(t, steamAuth(), nil)
	_, _, returnTo := steamStart(t, s, "/?tab=map")

	rec := doAuth(s.Handler(), http.MethodGet, "/auth/steam?next=%2F%3Ftab%3Dmap", nil, nil)
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("redirect URL: %v", err)
	}
	if loc.Scheme != "https" || loc.Host != "steam.example" || loc.Path != "/openid/login" {
		t.Fatalf("redirect = %q, want the configured Steam endpoint", loc)
	}
	q := loc.Query()
	if got := q.Get("openid.mode"); got != "checkid_setup" {
		t.Errorf("openid.mode = %q, want checkid_setup", got)
	}
	if got := q.Get("openid.realm"); got != "http://example.com/" {
		t.Errorf("openid.realm = %q, want http://example.com/", got)
	}
	if got := q.Get("openid.identity"); got != openIDIdentifierSelect {
		t.Errorf("openid.identity = %q, want the identifier_select value", got)
	}
	if !strings.Contains(returnTo, "next=%2F%3Ftab%3Dmap") {
		t.Errorf("return_to = %q, want the next target inside it", returnTo)
	}
}

func TestSteamCallbackSignsInAllowedAccount(t *testing.T) {
	s := steamServer(t, steamAuth(), func(_ context.Context, q url.Values) (string, error) {
		if got := q.Get("openid.mode"); got != "id_res" {
			t.Errorf("verifier saw openid.mode = %q", got)
		}
		return testSteamID, nil
	})

	state, stateCookie, _ := steamStart(t, s, "/?tab=map")
	rec := steamCallback(s, state, stateCookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("callback = %d, want 303 (body %s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/?tab=map" {
		t.Fatalf("redirect = %q, want the next target", loc)
	}

	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil || session.Value == "" || !session.HttpOnly {
		t.Fatalf("callback cookie = %+v", rec.Result().Cookies())
	}

	health := doAuth(s.Handler(), http.MethodGet, "/api/health", nil, []*http.Cookie{session})
	for _, want := range []string{`"user":"Alice"`, `"via":"steam"`} {
		if !strings.Contains(health.Body.String(), want) {
			t.Errorf("health payload = %s, want %s", health.Body.String(), want)
		}
	}

	// The state cookie is single-use: the same state cannot sign in again.
	if rec := steamCallback(s, state, stateCookie); rec.Code != http.StatusBadRequest {
		t.Fatalf("replayed callback = %d, want 400", rec.Code)
	}
}

func TestSteamCallbackDeniesUnknownAccount(t *testing.T) {
	s := steamServer(t, steamAuth(), func(context.Context, url.Values) (string, error) {
		return "76561198000000002", nil
	})

	state, stateCookie, _ := steamStart(t, s, "/")
	rec := steamCallback(s, state, stateCookie)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("callback = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not a dayzctl admin") {
		t.Errorf("body = %s, want a not-authorized message", rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			t.Fatalf("denied Steam account received a session cookie")
		}
	}
}

func TestSteamCallbackRequiresMatchingState(t *testing.T) {
	s := steamServer(t, steamAuth(), func(context.Context, url.Values) (string, error) {
		return testSteamID, nil
	})

	state, stateCookie, _ := steamStart(t, s, "/")
	// No state cookie at all.
	if rec := steamCallback(s, state); rec.Code != http.StatusBadRequest {
		t.Fatalf("callback without a state cookie = %d, want 400", rec.Code)
	}
	// A cookie from a different browser.
	other := &http.Cookie{Name: steamStateCookie, Value: "someone-else"}
	if rec := steamCallback(s, state, other); rec.Code != http.StatusBadRequest {
		t.Fatalf("callback with a foreign state = %d, want 400", rec.Code)
	}
	// The real pair still works.
	if rec := steamCallback(s, state, stateCookie); rec.Code != http.StatusSeeOther {
		t.Fatalf("valid callback = %d, want 303", rec.Code)
	}
}

func TestSteamCallbackVerifierFailure(t *testing.T) {
	s := steamServer(t, steamAuth(), func(context.Context, url.Values) (string, error) {
		return "", errors.New("Steam did not confirm the sign-in")
	})

	state, stateCookie, _ := steamStart(t, s, "/")
	rec := steamCallback(s, state, stateCookie)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("callback = %d, want 502", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.Value != "" {
			t.Fatalf("failed Steam sign-in received a session cookie")
		}
	}
}

func TestLoginPageOffersEnabledMethods(t *testing.T) {
	// Steam-only: no password form, but the button is there.
	steamOnly := steamServer(t, steamAuth(), nil)
	rec := doAuth(steamOnly.Handler(), http.MethodGet, "/login", nil, nil)
	body := rec.Body.String()
	if !strings.Contains(body, "/auth/steam") {
		t.Error("steam-only login page has no Steam button")
	}
	if strings.Contains(body, `name="password"`) {
		t.Error("steam-only login page still shows the password form")
	}

	// Local-only: no Steam button, and /auth/steam bounces back to /login.
	localOnly := authServer(t, config.WebAuth{Enabled: true})
	rec = doAuth(localOnly.Handler(), http.MethodGet, "/login", nil, nil)
	body = rec.Body.String()
	if strings.Contains(body, "/auth/steam") {
		t.Error("local-only login page offers Steam")
	}
	if !strings.Contains(body, `name="password"`) {
		t.Error("local-only login page has no password form")
	}
	if rec := doAuth(localOnly.Handler(), http.MethodGet, "/auth/steam", nil, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("/auth/steam with Steam off = %d, want 303", rec.Code)
	}

	// Both: the password form, an "or" divider and the Steam button.
	both := authServer(t, config.WebAuth{
		Enabled: true,
		Steam:   config.WebSteam{Enabled: true, Admins: map[string]string{testSteamID: ""}},
	})
	body = doAuth(both.Handler(), http.MethodGet, "/login", nil, nil).Body.String()
	for _, want := range []string{`name="password"`, "/auth/steam", `class="or"`} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %s", want)
		}
	}
}
