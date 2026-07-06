// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package webterminal

import _ "embed"

//go:embed www/index.html
var indexHTML []byte

func (f *terminalFilter) serveFrontend() {
	f.handle.SendResponseHeaders([][2]string{
		{":status", "200"},
		{"content-type", "text/html; charset=utf-8"},
	}, false)
	f.handle.SendResponseData(indexHTML, true)
}
