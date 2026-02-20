// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreateAndValidateSessionToken(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:gosec,errcheck

	data := &SessionData{
		NameID:     "user@example.com",
		Attributes: map[string][]string{"email": {"user@example.com"}, "groups": {"admin", "dev"}},
		ExpiresAt:  time.Now().Add(1 * time.Hour),
	}

	token, err := createSessionToken(key, data)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.Contains(t, token, ".")

	result, err := validateSessionToken(key, token)
	require.NoError(t, err)
	require.Equal(t, "user@example.com", result.NameID)
	require.Equal(t, []string{"user@example.com"}, result.Attributes["email"])
	require.Equal(t, []string{"admin", "dev"}, result.Attributes["groups"])
}

func TestValidateSessionToken_Expired(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:gosec,errcheck

	data := &SessionData{
		NameID:    "user@example.com",
		ExpiresAt: time.Now().Add(-1 * time.Hour), // expired
	}

	token, err := createSessionToken(key, data)
	require.NoError(t, err)

	_, err = validateSessionToken(key, token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}

func TestValidateSessionToken_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	rand.Read(key1) //nolint:gosec,errcheck
	rand.Read(key2) //nolint:gosec,errcheck

	data := &SessionData{
		NameID:    "user@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	token, err := createSessionToken(key1, data)
	require.NoError(t, err)

	_, err = validateSessionToken(key2, token)
	require.Error(t, err)
	require.Contains(t, err.Error(), "signature")
}

func TestValidateSessionToken_InvalidFormat(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:gosec,errcheck

	_, err := validateSessionToken(key, "no-dot-separator")
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid session token format")
}

func TestValidateSessionToken_TamperedPayload(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key) //nolint:gosec,errcheck

	data := &SessionData{
		NameID:    "user@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	token, err := createSessionToken(key, data)
	require.NoError(t, err)

	// Tamper with the payload part (before the dot).
	tampered := "dGFtcGVyZWQ" + token[10:]
	_, err = validateSessionToken(key, tampered)
	require.Error(t, err)
}

func TestBuildSessionCookie(t *testing.T) {
	cfg := &Config{
		CookieName:      "_saml_session",
		CookieSecure:    true,
		CookieDomain:    ".example.com",
		SessionDuration: 8 * time.Hour,
	}

	cookie := buildSessionCookie(noopLog, cfg, "test-token-value")
	require.Contains(t, cookie, "_saml_session=test-token-value")
	require.Contains(t, cookie, "Path=/")
	require.Contains(t, cookie, "HttpOnly")
	require.Contains(t, cookie, "SameSite=Lax")
	require.Contains(t, cookie, "Secure")
	require.Contains(t, cookie, "Domain=.example.com")
	require.Contains(t, cookie, "Max-Age=28800")
}

func TestBuildSessionCookie_NoSecure(t *testing.T) {
	cfg := &Config{
		CookieName:      "_saml_session",
		CookieSecure:    false,
		SessionDuration: 1 * time.Hour,
	}

	cookie := buildSessionCookie(noopLog, cfg, "token")
	require.NotContains(t, cookie, "Secure")
	require.NotContains(t, cookie, "Domain=")
}

func TestClearSessionCookie(t *testing.T) {
	cfg := &Config{
		CookieName:   "_saml_session",
		CookieSecure: true,
		CookieDomain: ".example.com",
	}

	cookie := clearSessionCookie(cfg)
	require.Contains(t, cookie, "_saml_session=")
	require.Contains(t, cookie, "Max-Age=0")
	require.Contains(t, cookie, "Secure")
	require.Contains(t, cookie, "Domain=.example.com")
}

func TestExtractSessionCookie(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		cookieName string
		want       string
	}{
		{"single cookie", "_saml_session=abc123", "_saml_session", "abc123"},
		{"multiple cookies", "other=val; _saml_session=abc123; more=stuff", "_saml_session", "abc123"},
		{"not found", "other=val; another=val2", "_saml_session", ""},
		{"empty header", "", "_saml_session", ""},
		{"cookie with equals in value", "_saml_session=abc=123", "_saml_session", "abc=123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionCookie(tt.header, tt.cookieName)
			require.Equal(t, tt.want, got)
		})
	}
}
