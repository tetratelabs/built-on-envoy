// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package fault

import "testing"

// mockHeaderGetter is a simple mock for HeaderGetter used in tests.
type mockHeaderGetter struct {
	headers map[string]string
}

func (m *mockHeaderGetter) GetOne(name string) string {
	return m.headers[name]
}

func TestMatchRoute_PrefixMatch(t *testing.T) {
	match := MatchConfig{Prefix: "/api/"}
	headers := &mockHeaderGetter{headers: map[string]string{}}

	if !MatchRoute(match, "/api/users", headers) {
		t.Error("expected /api/users to match prefix /api/")
	}
	if !MatchRoute(match, "/api/", headers) {
		t.Error("expected /api/ to match prefix /api/")
	}
	if MatchRoute(match, "/health", headers) {
		t.Error("expected /health not to match prefix /api/")
	}
}

func TestMatchRoute_ExactMatch(t *testing.T) {
	match := MatchConfig{Exact: "/health"}
	headers := &mockHeaderGetter{headers: map[string]string{}}

	if !MatchRoute(match, "/health", headers) {
		t.Error("expected /health to match exact /health")
	}
	if MatchRoute(match, "/health/check", headers) {
		t.Error("expected /health/check not to match exact /health")
	}
}

func TestMatchRoute_ExactMatchStripsQueryString(t *testing.T) {
	match := MatchConfig{Exact: "/health"}
	headers := &mockHeaderGetter{headers: map[string]string{}}

	if !MatchRoute(match, "/health?foo=bar", headers) {
		t.Error("expected /health?foo=bar to match exact /health (query string stripped)")
	}
}

func TestMatchRoute_HeaderExactMatch(t *testing.T) {
	match := MatchConfig{
		Headers: []HeaderMatchConfig{
			{Name: "x-env", ExactMatch: "staging"},
		},
	}

	headers := &mockHeaderGetter{headers: map[string]string{"x-env": "staging"}}
	if !MatchRoute(match, "/anything", headers) {
		t.Error("expected match when header x-env=staging")
	}

	headers = &mockHeaderGetter{headers: map[string]string{"x-env": "production"}}
	if MatchRoute(match, "/anything", headers) {
		t.Error("expected no match when header x-env=production")
	}

	headers = &mockHeaderGetter{headers: map[string]string{}}
	if MatchRoute(match, "/anything", headers) {
		t.Error("expected no match when header x-env is absent")
	}
}

func TestMatchRoute_HeaderPresentMatch(t *testing.T) {
	match := MatchConfig{
		Headers: []HeaderMatchConfig{
			{Name: "x-debug", PresentMatch: true},
		},
	}

	headers := &mockHeaderGetter{headers: map[string]string{"x-debug": "1"}}
	if !MatchRoute(match, "/anything", headers) {
		t.Error("expected match when header x-debug is present")
	}

	headers = &mockHeaderGetter{headers: map[string]string{}}
	if MatchRoute(match, "/anything", headers) {
		t.Error("expected no match when header x-debug is absent")
	}
}

func TestMatchRoute_CombinedPrefixAndHeaders(t *testing.T) {
	match := MatchConfig{
		Prefix: "/api/",
		Headers: []HeaderMatchConfig{
			{Name: "x-version", ExactMatch: "v2"},
		},
	}

	headers := &mockHeaderGetter{headers: map[string]string{"x-version": "v2"}}
	if !MatchRoute(match, "/api/users", headers) {
		t.Error("expected match with correct prefix and header")
	}

	headers = &mockHeaderGetter{headers: map[string]string{"x-version": "v1"}}
	if MatchRoute(match, "/api/users", headers) {
		t.Error("expected no match with correct prefix but wrong header")
	}

	headers = &mockHeaderGetter{headers: map[string]string{"x-version": "v2"}}
	if MatchRoute(match, "/other", headers) {
		t.Error("expected no match with wrong prefix but correct header")
	}
}

func TestMatchRoute_EmptyMatch(t *testing.T) {
	match := MatchConfig{}
	headers := &mockHeaderGetter{headers: map[string]string{}}

	if !MatchRoute(match, "/anything", headers) {
		t.Error("expected empty match config to match everything")
	}
}

func TestShouldApply_Always(t *testing.T) {
	if !ShouldApply(100) {
		t.Error("expected ShouldApply(100) to return true")
	}
	if !ShouldApply(150) {
		t.Error("expected ShouldApply(150) to return true")
	}
}

func TestShouldApply_Never(t *testing.T) {
	if ShouldApply(0) {
		t.Error("expected ShouldApply(0) to return false")
	}
	if ShouldApply(-10) {
		t.Error("expected ShouldApply(-10) to return false")
	}
}
