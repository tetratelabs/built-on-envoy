// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package saml

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/envoyproxy/envoy/source/extensions/dynamic_modules/sdk/go/shared"
)

// SessionData holds the data stored in a session cookie.
type SessionData struct {
	// NameID is the authenticated user's NameID from the SAML assertion.
	NameID string `json:"sub"`
	// Attributes maps SAML attribute names to their values.
	Attributes map[string][]string `json:"attrs,omitempty"`
	// ExpiresAt is when the session expires.
	ExpiresAt time.Time `json:"exp"`
}

// createSessionToken creates a signed session token containing the session data.
// Format: base64(json_payload) + "." + base64(hmac_sha256_signature)
func createSessionToken(signingKey []byte, data *SessionData) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal session data: %w", err)
	}

	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	sig := computeHMAC(signingKey, []byte(encodedPayload))
	encodedSig := base64.RawURLEncoding.EncodeToString(sig)

	return encodedPayload + "." + encodedSig, nil
}

// validateSessionToken validates a signed session token and returns the session data.
func validateSessionToken(signingKey []byte, token string) (*SessionData, error) {
	// Split into payload and signature.
	dotIdx := -1
	for i := len(token) - 1; i >= 0; i-- {
		if token[i] == '.' {
			dotIdx = i
			break
		}
	}
	if dotIdx < 0 {
		return nil, errors.New("invalid session token format")
	}

	encodedPayload := token[:dotIdx]
	encodedSig := token[dotIdx+1:]

	// Verify HMAC signature.
	expectedSig := computeHMAC(signingKey, []byte(encodedPayload))
	actualSig, err := base64.RawURLEncoding.DecodeString(encodedSig)
	if err != nil {
		return nil, errors.New("invalid session token signature encoding")
	}
	if !hmac.Equal(actualSig, expectedSig) {
		return nil, errors.New("invalid session token signature")
	}

	// Decode payload.
	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		return nil, errors.New("invalid session token payload encoding")
	}

	var data SessionData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	// Check expiration.
	if time.Now().After(data.ExpiresAt) {
		return nil, errors.New("session token expired")
	}

	return &data, nil
}

// computeHMAC computes an HMAC-SHA256 signature.
func computeHMAC(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

// buildSessionCookie builds a Set-Cookie header value for the session.
func buildSessionCookie(l logger, cfg *Config, token string) string {
	cookie := fmt.Sprintf("%s=%s; Path=/; HttpOnly; SameSite=Lax",
		cfg.CookieName, token)

	if cfg.CookieSecure {
		cookie += "; Secure"
	}
	if cfg.CookieDomain != "" {
		cookie += "; Domain=" + cfg.CookieDomain
	}

	maxAge := int(cfg.SessionDuration.Seconds())
	cookie += fmt.Sprintf("; Max-Age=%d", maxAge)

	l.Log(shared.LogLevelDebug, "saml: session cookie: %s", cookie)

	return cookie
}

// clearSessionCookie builds a Set-Cookie header that clears the session cookie.
func clearSessionCookie(cfg *Config) string {
	cookie := fmt.Sprintf("%s=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0",
		cfg.CookieName)

	if cfg.CookieSecure {
		cookie += "; Secure"
	}
	if cfg.CookieDomain != "" {
		cookie += "; Domain=" + cfg.CookieDomain
	}

	return cookie
}

// extractSessionCookie extracts the session token from the Cookie header.
func extractSessionCookie(cookieHeader string, cookieName string) string {
	// Parse cookies manually (simple implementation).
	// Cookie header format: name1=value1; name2=value2; ...
	for _, part := range splitCookies(cookieHeader) {
		name, value := parseCookiePair(part)
		if name == cookieName {
			return value
		}
	}
	return ""
}

// splitCookies splits a Cookie header value into individual cookie strings.
func splitCookies(header string) []string {
	var cookies []string
	start := 0
	for i := 0; i < len(header); i++ {
		if header[i] == ';' {
			cookies = append(cookies, header[start:i])
			start = i + 1
		}
	}
	cookies = append(cookies, header[start:])
	return cookies
}

// parseCookiePair parses a single cookie "name=value" pair, trimming whitespace.
func parseCookiePair(pair string) (name, value string) {
	// Trim leading whitespace.
	for len(pair) > 0 && pair[0] == ' ' {
		pair = pair[1:]
	}
	eqIdx := -1
	for i := 0; i < len(pair); i++ {
		if pair[i] == '=' {
			eqIdx = i
			break
		}
	}
	if eqIdx < 0 {
		return pair, ""
	}
	return pair[:eqIdx], pair[eqIdx+1:]
}
