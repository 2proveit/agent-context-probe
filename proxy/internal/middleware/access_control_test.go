package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAccessControlRequiresTokenAndStripsItBeforeProxying(t *testing.T) {
	const token = "0123456789abcdef"
	seenControlHeader := false
	handler := AccessControl(token, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenControlHeader = r.Header.Get(AccessTokenHeader) != ""
		w.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/requests", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	authorizedRequest.Header.Set(AccessTokenHeader, token)
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d", authorized.Code)
	}
	if seenControlHeader {
		t.Fatal("control access token would have been forwarded upstream")
	}
}

func TestAccessControlBrowserLoginSetsDigestCookie(t *testing.T) {
	const token = "0123456789abcdef"
	handler := AccessControl(token, true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	form := url.Values{"token": {token}}.Encode()
	loginRequest := httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(form))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status = %d", login.Code)
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == token || !cookies[0].HttpOnly {
		t.Fatalf("unexpected auth cookie: %#v", cookies)
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	dashboardRequest.Header.Set("Accept", "text/html")
	dashboardRequest.AddCookie(cookies[0])
	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, dashboardRequest)
	if dashboard.Code != http.StatusNoContent {
		t.Fatalf("cookie-authenticated status = %d", dashboard.Code)
	}
}

func TestAccessControlLeavesHealthPublic(t *testing.T) {
	handler := AccessControl("0123456789abcdef", true)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d", recorder.Code)
	}
}
