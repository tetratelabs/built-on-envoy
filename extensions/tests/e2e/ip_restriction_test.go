// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"fmt"
	"testing"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestIPRestrictionDeny(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../ip-restriction",
		"--config", `{"deny_addresses": ["127.0.0.0/8"]}`)

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	internaltesting.RequireEventuallyGet(t, url, internaltesting.EqualStatus(403))
}

func TestIPRestrictionAllow(t *testing.T) {
	ports := internaltesting.FreePorts(t, 2)
	proxyPort, adminPort := ports[0], ports[1]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, adminPort,
		"--log-level", "dynamic_modules:debug",
		"--local", "../../ip-restriction",
		"--config", `{"allow_addresses": ["127.0.0.0/8"]}`)

	url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
	internaltesting.RequireEventuallyGet(t, url, internaltesting.EqualStatus(200))
}
