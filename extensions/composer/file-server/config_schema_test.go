// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package impl

import (
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/extensions/composer/internal/testing"
)

func TestConfigSchema(t *testing.T) {
	t.Run("valid full config", func(t *testing.T) {
		internaltesting.AssertSchemaValid(t, "config.schema.json", `{
			"path_mappings": [
				{"request_path_prefix": "/static/", "file_path_prefix": "/var/www/"}
			],
			"content_types": {"html": "text/html", "css": "text/css"},
			"default_content_type": "application/octet-stream",
			"directory_index_files": ["index.html"]
		}`)
	})
	t.Run("empty config", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{}`)
	})
	t.Run("invalid empty path mappings", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"path_mappings": []
		}`)
	})
	t.Run("invalid directory index file with slash", func(t *testing.T) {
		internaltesting.AssertSchemaInvalid(t, "config.schema.json", `{
			"path_mappings": [
				{"request_path_prefix": "/static/", "file_path_prefix": "/var/www/"}
			],
			"directory_index_files": ["path/index.html"]
		}`)
	})
}
