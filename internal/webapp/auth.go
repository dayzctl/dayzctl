package webapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/dayzctl/dayzctl/internal/config"
	"github.com/dayzctl/dayzctl/internal/logger"
)

const (
	// sessionCookie is the name of the login cookie.
	sessionCookie = "dayzctl_session"
	// steamStateCookie binds a Steam redirect to the browser that started it,
	// so a callback from another browser cannot be replayed here.
	steamStateCookie = "dayzctl_steam_state"
	// steamStateTTL is how long a visitor may spend inside Steam.
	steamStateTTL = 10 * time.Minute
)

// session is one signed-in browser.
type session struct {
	// User is the display name shown in the UI.
	User string
	// Via is "password" or "steam".
	Via string
	// SteamID is set for a Steam sign-in and empty otherwise.
	SteamID string
	Expiry  time.Time
}

// sessions keeps issued login tokens in memory. A restart invalidates them,
// which is the right behaviour for a single-host tool: nothing is persisted
// where a copied file could leak it.
type sessions struct {
	mu     sync.Mutex
	tokens map[string]session
	ttl    time.Duration
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{tokens: make(map[string]session), ttl: ttl}
}

// issue mints a token for ident and returns its expiry.
func (s *sessions) issue(ident session) (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(buf)
	expiry := time.Now().Add(s.ttl)
	ident.Expiry = expiry

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.tokens[token] = ident
	return token, expiry, nil
}

// get returns the session a request carries, if it is still valid.
func (s *sessions) get(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ident, ok := s.tokens[c.Value]
	if !ok {
		return session{}, false
	}
	if time.Now().After(ident.Expiry) {
		delete(s.tokens, c.Value)
		return session{}, false
	}
	return ident, true
}

// valid reports whether the request carries a live session.
func (s *sessions) valid(r *http.Request) bool {
	_, ok := s.get(r)
	return ok
}

// drop forgets a token so a sign-out takes effect immediately.
func (s *sessions) drop(r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, c.Value)
}

func (s *sessions) pruneLocked() {
	now := time.Now()
	for token, ident := range s.tokens {
		if now.After(ident.Expiry) {
			delete(s.tokens, token)
		}
	}
}

// steamVerifier checks an OpenID 2.0 assertion with Steam and returns the
// SteamID64 it names. The server holds one so tests can stand in for Steam.
type steamVerifier func(ctx context.Context, q url.Values) (string, error)

// pendingStates remembers the state tokens handed to Steam. A token can be
// used once, so a captured callback cannot sign in a second time.
type pendingStates struct {
	mu     sync.Mutex
	tokens map[string]time.Time
}

func newPendingStates() *pendingStates {
	return &pendingStates{tokens: make(map[string]time.Time)}
}

func (p *pendingStates) add(token string, ttl time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for t, expiry := range p.tokens {
		if now.After(expiry) {
			delete(p.tokens, t)
		}
	}
	p.tokens[token] = now.Add(ttl)
}

// consume reports whether token is live and forgets it in the same step.
func (p *pendingStates) consume(token string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	expiry, ok := p.tokens[token]
	if !ok {
		return false
	}
	delete(p.tokens, token)
	return time.Now().Before(expiry)
}

// verifySteamOpenID posts the assertion back to Steam. Steam answers
// "is_valid:true" only when it really issued it, which is what stops a forged
// callback from signing anyone in.
func verifySteamOpenID(ctx context.Context, q url.Values) (string, error) {
	claimed := q.Get("openid.claimed_id")
	const prefix = "https://steamcommunity.com/openid/id/"
	if !strings.HasPrefix(claimed, prefix) {
		return "", errors.New("callback does not identify a Steam account")
	}
	id := strings.TrimPrefix(claimed, prefix)
	if len(id) != 17 || !strings.HasPrefix(id, "7656") {
		return "", errors.New("callback carries a malformed SteamID64")
	}

	form := url.Values{}
	for key, values := range q {
		if strings.HasPrefix(key, "openid.") && len(values) > 0 {
			form.Set(key, values[0])
		}
	}
	form.Set("openid.mode", "check_authentication")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://steamcommunity.com/openid/login", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach Steam: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("could not read Steam's answer: %w", err)
	}
	valid := false
	for _, line := range strings.Split(string(body), "\n") {
		if strings.EqualFold(strings.TrimSpace(line), "is_valid:true") {
			valid = true
			break
		}
	}
	if !valid {
		return "", errors.New("Steam did not confirm the sign-in")
	}
	return id, nil
}

// requireAuth puts the login switch in front of the whole handler tree.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Active() {
			next.ServeHTTP(w, r)
			return
		}
		if s.isAuthPath(r.URL.Path) || s.sessions.valid(r) {
			next.ServeHTTP(w, r)
			return
		}
		// The UI asks for JSON, so answer with a status it can show instead of
		// redirecting an XHR into an HTML page.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		target := "/login"
		if next := r.URL.RequestURI(); next != "/" {
			target += "?next=" + url.QueryEscape(next)
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	})
}

// isAuthPath lists the endpoints that stay reachable without a session: the
// sign-in page and the Steam round-trip.
func (s *Server) isAuthPath(path string) bool {
	switch path {
	case "/login", "/auth/steam", "/auth/steam/callback":
		return true
	}
	return false
}

// handleLoginForm serves the sign-in page.
func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Active() || s.sessions.valid(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, http.StatusOK, safeNext(r.URL.Query().Get("next")), "")
}

// handleLogin checks the server's admin password and issues a session cookie.
// There is no username: server.password_admin is the only local credential.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Active() || s.adminPassword == "" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "malformed form")
		return
	}
	pass := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if !config.CheckAdminPassword(s.adminPassword, pass) {
		// Slow failed attempts down a little; the form has no other backoff.
		logger.Warn("Web sign-in failed", "via", "password", "remote", r.RemoteAddr)
		time.Sleep(400 * time.Millisecond)
		s.renderLogin(w, http.StatusUnauthorized, next, "Wrong admin password.")
		return
	}
	logger.Info("Web sign-in", "via", "password", "remote", r.RemoteAddr)
	s.beginSession(w, r, session{User: "admin", Via: "password"}, next)
}

// beginSession mints a token and sends the browser on to its destination.
func (s *Server) beginSession(w http.ResponseWriter, r *http.Request, ident session, next string) {
	token, expiry, err := s.sessions.issue(ident)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start a session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	http.Redirect(w, r, safeNext(next), http.StatusSeeOther)
}

// renderLogin writes the sign-in page, offering whichever methods are enabled.
func (s *Server) renderLogin(w http.ResponseWriter, status int, next, errMsg string) {
	block := ""
	if errMsg != "" {
		block = `<p class="error">` + htmlEscape(errMsg) + `</p>`
	}
	local := ""
	if s.adminPassword != "" {
		local = strings.NewReplacer(
			"{{NEXT}}", htmlEscape(next),
		).Replace(loginLocalForm)
	}
	steam := ""
	if s.auth.SteamEnabled() {
		steam = `<a class="steam" href="/auth/steam?next=` + url.QueryEscape(next) + `">Sign in with Steam</a>`
		if local != "" {
			steam += `
    <p class="or">or</p>`
		}
	}
	page := strings.NewReplacer(
		"{{ERROR}}", block,
		"{{LOCAL}}", local,
		"{{STEAM}}", steam,
	).Replace(loginPage)

	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	w.Header().Set(headerCacheControl, "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
}

// handleLogout drops the session and returns to the sign-in page.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.drop(r)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Steam SSO (OpenID 2.0)
// ---------------------------------------------------------------------------

const (
	openIDNamespace        = "http://specs.openid.net/auth/2.0"
	openIDIdentifierSelect = "http://specs.openid.net/auth/2.0/identifier_select"
	openIDSteamIDPrefix    = "https://steamcommunity.com/openid/id/"
)

// handleSteamLogin sends the browser to Steam to prove which account it owns.
func (s *Server) handleSteamLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.SteamEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	state, err := randomToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start the Steam sign-in")
		return
	}
	s.steamStates.add(state, steamStateTTL)
	next := safeNext(r.URL.Query().Get("next"))
	http.SetCookie(w, &http.Cookie{
		Name:     steamStateCookie,
		Value:    state,
		Path:     "/auth/steam",
		MaxAge:   int(steamStateTTL / time.Second),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})

	origin := s.steamOrigin(r)
	returnTo := origin + "/auth/steam/callback?state=" + url.QueryEscape(state) + "&next=" + url.QueryEscape(next)
	params := url.Values{
		"openid.ns":         {openIDNamespace},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {origin + "/"},
		"openid.identity":   {openIDIdentifierSelect},
		"openid.claimed_id": {openIDIdentifierSelect},
	}
	http.Redirect(w, r, s.steamEndpoint+"?"+params.Encode(), http.StatusSeeOther)
}

// handleSteamCallback verifies Steam's answer and only then signs the visitor
// in, provided the Steam account is on the allowlist.
func (s *Server) handleSteamCallback(w http.ResponseWriter, r *http.Request) {
	if !s.auth.SteamEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	q := r.URL.Query()
	next := safeNext(q.Get("next"))

	state := ""
	if c, err := r.Cookie(steamStateCookie); err == nil {
		state = c.Value
	}
	// The state is single-use whether or not it matched.
	http.SetCookie(w, &http.Cookie{
		Name:     steamStateCookie,
		Value:    "",
		Path:     "/auth/steam",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
	if state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(q.Get("state"))) != 1 {
		s.renderAuthError(w, http.StatusBadRequest, "Sign-in expired",
			"This sign-in link is no longer valid. Start again from the sign-in page.")
		return
	}
	// Only a callback that came back to the browser that started it may spend
	// the token, so a stale tab cannot cancel a fresh sign-in attempt.
	if !s.steamStates.consume(state) {
		s.renderAuthError(w, http.StatusBadRequest, "Sign-in expired",
			"This sign-in link has already been used. Start again from the sign-in page.")
		return
	}
	if q.Get("openid.mode") != "id_res" {
		s.renderAuthError(w, http.StatusBadRequest, "Sign-in cancelled",
			"Steam did not complete the sign-in.")
		return
	}

	id, err := s.verifySteam(r.Context(), q)
	if err != nil {
		logger.Warn("Steam sign-in verification failed", "error", err)
		s.renderAuthError(w, http.StatusBadGateway, "Steam sign-in failed", err.Error())
		return
	}
	name, ok := s.auth.SteamAllowed(id)
	if !ok {
		logger.Warn("Steam sign-in refused: account is not in web.auth.steam.admins", "steamID", id)
		s.renderAuthError(w, http.StatusForbidden, "Not authorized",
			"The Steam account "+id+" is not a dayzctl admin on this server. Ask the operator to add it to web.auth.steam.admins.")
		return
	}
	logger.Info("Web sign-in", "via", "steam", "steamID", id, "user", name)
	s.beginSession(w, r, session{User: name, Via: "steam", SteamID: id}, next)
}

// steamOrigin is the external URL the browser reached us on, used for the
// OpenID return_to and realm.
func (s *Server) steamOrigin(r *http.Request) string {
	if base := strings.TrimSpace(s.auth.Steam.PublicURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); proto == "http" || proto == "https" {
		scheme = proto
	}
	host := r.Host
	if fwd := firstHeaderValue(r.Header.Get("X-Forwarded-Host")); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

func firstHeaderValue(v string) string {
	if v == "" {
		return ""
	}
	first, _, _ := strings.Cut(v, ",")
	return strings.TrimSpace(first)
}

// renderAuthError writes a small page for a sign-in that did not go through.
func (s *Server) renderAuthError(w http.ResponseWriter, status int, title, message string) {
	page := strings.NewReplacer(
		"{{TITLE}}", htmlEscape(title),
		"{{MESSAGE}}", htmlEscape(message),
	).Replace(authErrorPage)

	w.Header().Set(headerContentType, "text/html; charset=utf-8")
	w.Header().Set(headerCacheControl, "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(page))
}

// randomToken returns a hex string with 128 bits of randomness.
func randomToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// safeNext keeps redirects on this host: only a rooted path is accepted.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func htmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

// authPageCSS is shared by the sign-in and sign-in-error pages, which keep
// their own styles so they render even when the main stylesheet is missing.
const authPageCSS = `
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body {
    margin: 0; min-height: 100vh; display: grid; place-items: center;
    background: #0b0e11; color: #e6e9ee;
    font: 14px/1.5 system-ui, -apple-system, Segoe UI, Roboto, sans-serif;
  }
  .card {
    width: min(360px, calc(100vw - 32px));
    background: #12161b; border: 1px solid #232a32; border-radius: 12px;
    padding: 24px;
  }
  h1 { margin: 0 0 4px; font-size: 17px; }
  p.sub { margin: 0 0 18px; color: #9aa4b2; font-size: 12.5px; }
  label { display: block; font-size: 11.5px; letter-spacing: .5px;
          text-transform: uppercase; color: #6f7a88; margin-bottom: 4px; }
  input {
    width: 100%; margin-bottom: 14px; padding: 8px 10px; font: inherit;
    color: #e6e9ee; background: #0d1116; border: 1px solid #232a32;
    border-radius: 8px;
  }
  input:focus { outline: 2px solid #d43f4a; outline-offset: 1px; }
  button {
    width: 100%; padding: 9px 12px; font: inherit; font-weight: 600;
    color: #fff; background: #d43f4a; border: 0; border-radius: 8px;
    cursor: pointer;
  }
  button:hover { background: #b83742; }
  .error {
    margin: 0 0 14px; padding: 8px 10px; border-radius: 8px; font-size: 13px;
    color: #ffd7d9; background: #3a161a; border: 1px solid #6b2a2f;
  }
  p.or { margin: 14px 0 12px; text-align: center; color: #6f7a88;
         font-size: 12px; text-transform: uppercase; letter-spacing: .5px; }
  a.steam {
    display: block; padding: 9px 12px; text-align: center;
    font-weight: 600; text-decoration: none; color: #cfe0f0;
    background: #1b2a38; border: 1px solid #2c4257; border-radius: 8px;
  }
  a.steam:hover { background: #213547; }
  p.back { margin: 18px 0 0; text-align: center; }
  p.back a { color: #9aa4b2; }
`

// loginPage is the sign-in screen. {{LOCAL}} holds the password form and
// {{STEAM}} the Steam button; renderLogin fills in whichever are enabled.
const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Sign in — DayZ Log Analyzer</title>
<style>` + authPageCSS + `</style>
</head>
<body>
  <div class="card">
    <h1>DayZ Log Analyzer</h1>
    <p class="sub">Sign in to read this server's logs.</p>
    {{ERROR}}
    {{STEAM}}
    {{LOCAL}}
  </div>
</body>
</html>
`

// loginLocalForm is the admin-password half of the sign-in page.
const loginLocalForm = `<form method="post" action="/login">
  <label for="password">Admin password</label>
  <input id="password" name="password" type="password" autocomplete="current-password" autofocus required />
  <input type="hidden" name="next" value="{{NEXT}}" />
  <button type="submit">Sign in</button>
</form>`

// authErrorPage explains why a sign-in did not go through (denied Steam
// account, expired state, Steam unreachable).
const authErrorPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>{{TITLE}} — DayZ Log Analyzer</title>
<style>` + authPageCSS + `</style>
</head>
<body>
  <div class="card">
    <h1>{{TITLE}}</h1>
    <p class="sub">{{MESSAGE}}</p>
    <p class="back"><a href="/login">Back to sign in</a></p>
  </div>
</body>
</html>
`
