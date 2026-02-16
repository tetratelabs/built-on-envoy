// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactSensitive(t *testing.T) {
	type simple struct {
		Name     string `sensitive:"true"`
		Location string
	}

	type nested struct {
		Outer string
		Inner simple
	}

	type multipleSensitive struct {
		Token    string `sensitive:"true"`
		Password string `sensitive:"true"`
		Username string
	}

	type noSensitive struct {
		Name  string
		Value string
	}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{
			name:     "redacts sensitive field",
			input:    simple{Name: "secret", Location: "public"},
			expected: simple{Name: "*****", Location: "public"},
		},
		{
			name:     "handles nested structs",
			input:    nested{Outer: "visible", Inner: simple{Name: "secret", Location: "public"}},
			expected: nested{Outer: "visible", Inner: simple{Name: "*****", Location: "public"}},
		},
		{
			name:     "redacts multiple sensitive fields",
			input:    multipleSensitive{Token: "tok-123", Password: "pass", Username: "user"},
			expected: multipleSensitive{Token: "*****", Password: "*****", Username: "user"},
		},
		{
			name:     "no sensitive fields leaves struct unchanged",
			input:    noSensitive{Name: "hello", Value: "world"},
			expected: noSensitive{Name: "hello", Value: "world"},
		},
		{
			name:     "accepts pointer input",
			input:    &simple{Name: "secret", Location: "public"},
			expected: simple{Name: "*****", Location: "public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSensitive(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRedactSensitiveDoesNotMutateOriginal(t *testing.T) {
	type config struct {
		APIKey string `sensitive:"true"`
		Host   string
	}

	original := config{APIKey: "my-secret-key", Host: "localhost"}
	_ = RedactSensitive(original)

	assert.Equal(t, "my-secret-key", original.APIKey)
	assert.Equal(t, "localhost", original.Host)
}
