package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

const (
	AccessTokenHeader = "X-Agent-Context-Probe-Token"
	authCookieName    = "agent_context_probe_session"
)

func AccessControl(accessToken string, required bool) func(http.Handler) http.Handler {
	cookieValue := accessTokenDigest(accessToken)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !required {
				r.Header.Del(AccessTokenHeader)
				next.ServeHTTP(w, r)
				return
			}

			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/auth" {
				handleAuthentication(w, r, accessToken, cookieValue)
				return
			}

			providedToken := r.Header.Get(AccessTokenHeader)
			r.Header.Del(AccessTokenHeader)
			if secureEqual(providedToken, accessToken) || validAuthCookie(r, cookieValue) {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("Cache-Control", "no-store")
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/auth", http.StatusSeeOther)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "access token required"})
		})
	}
}

func handleAuthentication(w http.ResponseWriter, r *http.Request, accessToken, cookieValue string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'")
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginPage("")))
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
		if err := r.ParseForm(); err != nil || !secureEqual(r.FormValue("token"), accessToken) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(loginPage("Access token is invalid.")))
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     authCookieName,
			Value:    cookieValue,
			Path:     "/",
			HttpOnly: true,
			Secure:   r.TLS != nil,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int((8 * time.Hour).Seconds()),
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func loginPage(errorMessage string) string {
	message := ""
	if errorMessage != "" {
		message = `<p role="alert" style="color:#b91c1c">` + errorMessage + `</p>`
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Agent Context Probe</title></head><body style="margin:0;background:#f9fafb;font-family:system-ui,sans-serif;color:#111827"><main style="max-width:28rem;margin:12vh auto;padding:2rem;background:white;border:1px solid #e5e7eb;border-radius:.75rem"><h1 style="font-size:1.25rem">Agent Context Probe</h1><p>Enter the access token configured for remote access.</p>` + message + `<form method="post" action="/auth"><label for="token">Access token</label><input id="token" name="token" type="password" autocomplete="current-password" required style="box-sizing:border-box;width:100%;margin:.5rem 0 1rem;padding:.65rem;border:1px solid #d1d5db;border-radius:.4rem"><button type="submit" style="padding:.65rem 1rem;border:0;border-radius:.4rem;background:#111827;color:white">Continue</button></form></main></body></html>`
}

func validAuthCookie(r *http.Request, expected string) bool {
	cookie, err := r.Cookie(authCookieName)
	return err == nil && secureEqual(cookie.Value, expected)
}

func accessTokenDigest(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) || len(left) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
