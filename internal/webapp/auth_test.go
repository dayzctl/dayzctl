package webapp

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dayzctl/dayzctl/internal/config"
)

func authServer(t *testing.T, auth config.WebAuth) *Server {
	t.Helper()
	cfg := &config.ServerConfig{}
	cfg.Paths.Base = t.TempDir()
	cfg.Paths.InstallDir = t.TempDir()
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
	s := authServer(t, config.WebAuth{Enabled: true, Username: "admin", Password: "s3cret"})

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
	s := authServer(t, config.WebAuth{Enabled: true, Username: "admin", Password: "s3cret"})

	// The form itself is reachable without a session.
	if rec := doAuth(s.Handler(), http.MethodGet, "/login", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("login form = %d, want 200", rec.Code)
	}

	// Wrong password: 401 and no cookie.
	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"username": {"admin"}, "password": {"nope"}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatalf("bad login set a cookie")
	}

	// Right password: redirect plus a session cookie that unlocks the API.
	rec = doAuth(s.Handler(), http.MethodPost, "/login", url.Values{
		"username": {"admin"},
		"password": {"s3cret"},
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

func TestAuthPasswordHash(t *testing.T) {
	sum := sha256.Sum256([]byte("hunter2"))
	hash := "sha256:" + hex.EncodeToString(sum[:])

	s := authServer(t, config.WebAuth{Enabled: true, Username: "ops", PasswordHash: hash})

	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"username": {"ops"}, "password": {"hunter2"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("hashed login = %d, want 303", rec.Code)
	}
	rec = doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"username": {"ops"}, "password": {"hunter3"}}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", rec.Code)
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
	s := authServer(t, config.WebAuth{Enabled: true, Username: "admin", Password: "s3cret"})
	rec := doAuth(s.Handler(), http.MethodPost, "/login", url.Values{"username": {"admin"}, "password": {"s3cret"}}, nil)
	cookies := rec.Result().Cookies()

	rec = doAuth(s.Handler(), http.MethodGet, "/api/health", nil, cookies)
	if !strings.Contains(rec.Body.String(), `"auth":true`) {
		t.Fatalf("health payload = %s, want auth:true", rec.Body.String())
	}
}
