package webapp

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// sessionCookie is the name of the login cookie.
const sessionCookie = "dayzctl_session"

// sessions keeps issued login tokens in memory. A restart invalidates them,
// which is the right behaviour for a single-host tool: nothing is persisted
// where a copied file could leak it.
type sessions struct {
	mu     sync.Mutex
	tokens map[string]time.Time
	ttl    time.Duration
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{tokens: make(map[string]time.Time), ttl: ttl}
}

// issue mints a token and returns its expiry.
func (s *sessions) issue() (string, time.Time, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(buf)
	expiry := time.Now().Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.tokens[token] = expiry
	return token, expiry, nil
}

// valid reports whether the request carries a live session.
func (s *sessions) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.tokens[c.Value]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.tokens, c.Value)
		return false
	}
	return true
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
	for token, expiry := range s.tokens {
		if now.After(expiry) {
			delete(s.tokens, token)
		}
	}
}

// requireAuth puts the login switch in front of the whole handler tree.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.auth.Active() {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/login") || s.sessions.valid(r) {
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

// handleLoginForm serves the sign-in page.
func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Active() || s.sessions.valid(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderLogin(w, http.StatusOK, safeNext(r.URL.Query().Get("next")), "", "")
}

// handleLogin checks the credentials and issues a session cookie.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Active() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "malformed form")
		return
	}
	user := r.PostFormValue("username")
	pass := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))

	if !s.auth.CheckCredentials(user, pass) {
		// Slow failed attempts down a little; the form has no other backoff.
		time.Sleep(400 * time.Millisecond)
		s.renderLogin(w, http.StatusUnauthorized, next, user, "Wrong username or password.")
		return
	}

	token, expiry, err := s.sessions.issue()
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
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// renderLogin writes the sign-in page, with an optional error block.
func (s *Server) renderLogin(w http.ResponseWriter, status int, next, user, errMsg string) {
	block := ""
	if errMsg != "" {
		block = `<p class="error">` + htmlEscape(errMsg) + `</p>`
	}
	page := strings.NewReplacer(
		"{{NEXT}}", htmlEscape(next),
		"{{USER}}", htmlEscape(user),
		"{{ERROR}}", block,
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

// loginPage is the sign-in screen. It keeps its own styles so it renders even
// when the main stylesheet cannot be loaded.
const loginPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Sign in — DayZ Log Analyzer</title>
<style>
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
</style>
</head>
<body>
  <form class="card" method="post" action="/login">
    <h1>DayZ Log Analyzer</h1>
    <p class="sub">Sign in to read this server's logs.</p>
    {{ERROR}}
    <label for="username">Username</label>
    <input id="username" name="username" value="{{USER}}" autocomplete="username" autofocus required />
    <label for="password">Password</label>
    <input id="password" name="password" type="password" autocomplete="current-password" required />
    <input type="hidden" name="next" value="{{NEXT}}" />
    <button type="submit">Sign in</button>
  </form>
</body>
</html>
`
