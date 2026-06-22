// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package integration

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestWAFSmoke(t *testing.T) {
	const config = `{
	"mode": "FULL",
	"directives": [
		"SecRuleEngine On",
		"SecRequestBodyAccess On",
		"SecResponseBodyAccess On",
		"SecResponseBodyMimeType application/json text/plain",
		"SecRule &REQUEST_HEADERS:coraza-e2e \"@eq 0\" \"id:100,phase:1,deny,status:424,log,msg:'Coraza E2E - Missing header'\"",
		"SecRule REQUEST_URI \"@contains /admin\" \"id:101,phase:1,t:lowercase,log,deny,status:403\"",
		"SecRule ARGS:payload \"@streq maliciouspayload\" \"id:102,phase:2,t:lowercase,log,deny,status:403\"",
		"SecRule RESPONSE_HEADERS:pass \"@rx leak\" \"id:103,phase:3,t:lowercase,log,deny,status:403\"",
		"SecRule RESPONSE_BODY \"@contains responsebodycode\" \"id:104,phase:4,t:lowercase,log,deny,status:403\"",
		"SecRule ARGS_NAMES|ARGS \"@detectXSS\" \"id:9411,phase:2,t:none,t:utf8toUnicode,t:urlDecodeUni,t:htmlEntityDecode,t:jsDecode,t:cssDecode,t:removeNulls,log,deny,status:403\"",
		"SecRule ARGS_NAMES|ARGS \"@detectSQLi\" \"id:9421,phase:2,t:none,t:utf8toUnicode,t:urlDecodeUni,t:removeNulls,multiMatch,log,deny,status:403\"",
		"SecRule REQUEST_HEADERS:User-Agent \"@pm grabber masscan\" \"id:9131,phase:1,t:none,log,deny,status:403\""
	]
}`

	ports := internaltesting.FreePorts(t, 2)
	proxyPort := ports[0]

	internaltesting.RunEnvoy(t, cliBin, proxyPort, ports[1],
		"--log-level", "dynamic_modules:debug",
		"--local", "../../composer/waf",
		"--config", config)

	t.Run("allow_request", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("coraza-e2e", "ok")

		internaltesting.RequireEventuallyRequest(t, req, internaltesting.EqualStatus(200))
	})

	t.Run("block_request_headers", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/status/200", proxyPort)
		internaltesting.RequireEventuallyGet(t, url, internaltesting.EqualStatus(424))

		url = fmt.Sprintf("http://localhost:%d/admin", proxyPort)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("coraza-e2e", "ok")
		internaltesting.RequireEventuallyRequest(t, req, internaltesting.EqualStatus(403))
	})

	t.Run("block_request_body", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/anything", proxyPort)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader("payload=maliciouspayload"))
		require.NoError(t, err)
		req.Header.Set("content-type", "application/x-www-form-urlencoded")
		req.Header.Set("coraza-e2e", "ok")

		internaltesting.RequireEventuallyRequest(t, req, internaltesting.EqualStatus(403))
	})

	t.Run("block_response_headers", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/response-headers?pass=leak", proxyPort)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("coraza-e2e", "ok")
		internaltesting.RequireEventuallyRequest(t, req, internaltesting.EqualStatus(403))
	})

	t.Run("block_response_body", func(t *testing.T) {
		url := fmt.Sprintf("http://localhost:%d/headers", proxyPort)
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		require.NoError(t, err)
		req.Header.Set("coraza-e2e", "ok")
		req.Header.Set("forbidden-body-content", "responsebodycode")
		internaltesting.RequireEventuallyRequest(t, req, internaltesting.EqualStatus(403))
	})
}

const (
	envoyLogs  = "testdata/waf/envoy.log"
	albedoLogs = "testdata/waf/albedo.log"
	ftwPort    = 1064
)

func TestFTW(t *testing.T) {
	crsDir := requireCoreRuleSet(t)
	albedoPort := startAlbedo(t)
	ports := internaltesting.FreePorts(t, 1)

	// This env var is used in the tests RunEnvoy to automatically configure "albedo" the test upstream cluster.
	t.Setenv("TEST_BOE_UPSTREAM_CLUSTER_INSECURE", "localhost:"+strconv.Itoa(albedoPort))
	t.Setenv("TEST_BOE_CLI_OUTPUT_FILE", envoyLogs) // Write CLI output to this file in addition to the in-mem buffers.

	const config = `{
	"mode": "FULL",
	"directives": [
		"Include @coraza.conf",
		"Include @ftw.conf",
		"Include @crs-setup.conf",
		"Include @owasp_crs/*.conf"
	]
}`

	internaltesting.RunEnvoy(t, cliBin, ftwPort, ports[0],
		"--log-level", "dynamic_modules:debug",
		"--local", "../../composer/waf",
		"--config", config)

	cmdArgs := []string{
		"tool", "-modfile=../../../tools/go.mod",
		"go-ftw",
		"run",
		"-d", crsDir,
		"-l", envoyLogs,
		"--config", "testdata/waf/ftw.yaml",
		"--show-failures-only",
		"--read-timeout=30s",
		"--wait-for-timeout=0",
		"--max-marker-log-lines=2000",
		"--max-marker-retries=500",
	}
	if runtime.GOOS == "darwin" {
		cmdArgs = append(cmdArgs, "--rate-limit=5ms")
	}
	if debug := os.Getenv("FTW_DEBUG"); debug == "true" {
		cmdArgs = append(cmdArgs, "--debug")
	}
	if include := os.Getenv("FTW_INCLUDE"); include != "" {
		cmdArgs = append(cmdArgs, "-i", include)
	}

	// #nosec G204
	cmd := exec.Command("go", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run())
}

// TODO: Do not use go tool but just start the server using the right packages.
func startAlbedo(t *testing.T) int {
	t.Helper()

	buffers := internaltesting.TeeOutput(t, albedoLogs, "Albedo stdout", "Albedo stderr")
	port := internaltesting.FreePorts(t, 1)[0]

	t.Logf("Starting albedo on port %d", port)

	// #nosec G204
	cmd := exec.Command("go", "tool", "-modfile=../../../tools/go.mod", "albedo", "--port", strconv.Itoa(port))
	cmd.Stdout = buffers[0]
	cmd.Stderr = buffers[1]
	// Create a new process group so we can kill boe and all its children
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())

	// `go tool albedo` may take time on first run (toolchain/module setup). Make
	// sure it is listening before Envoy readiness checks depend on it.
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode < http.StatusInternalServerError
	}, 180*time.Second, time.Second, "waiting for albedo listener %d", port)

	t.Cleanup(func() {
		pgid := -cmd.Process.Pid
		_ = syscall.Kill(pgid, syscall.SIGTERM)
	})

	return port
}

// TODO: Do not use go tool but just run FTW using hte right packages.
func requireCoreRuleSet(t *testing.T) string {
	t.Helper()

	if crsDir := os.Getenv("CRS_DIR"); crsDir != "" {
		require.DirExists(t, crsDir)
		return crsDir
	}

	confPath := filepath.Join("..", "..", "composer", "waf", "coraza", "directives", "@crs-setup.conf")
	confData, err := os.ReadFile(filepath.Clean(confPath))
	require.NoError(t, err, "failed to read CRS config file")

	var crsVersion string
	for _, line := range strings.Split(string(confData), "\n") {
		if v, ok := strings.CutPrefix(line, "# OWASP CRS ver."); ok {
			crsVersion = strings.TrimSpace(v)
			break
		}
	}
	require.NotEmpty(t, crsVersion, "could not determine WAF CRS version from %s", confPath)

	buffers := internaltesting.DumpLogsOnFail(t, t.TempDir(), "CRS stdout", "CRS stderr")

	crsRef := "v" + crsVersion
	const crsRepo = "https://github.com/coreruleset/coreruleset.git"
	const crsCloneDir = "testdata/waf/coreruleset"
	crsDir := filepath.Join(crsCloneDir, "tests", "regression", "tests")

	if _, err := os.Stat(filepath.Join(crsCloneDir, ".git")); errors.Is(err, os.ErrNotExist) {
		t.Logf("Cloning Core Rule Set %s into coreruleset", crsRef)
		// #nosec G204
		cmd := exec.Command("git", "clone", "--depth", "1", "--branch", crsRef, "--single-branch", crsRepo, crsCloneDir)
		cmd.Stdout = buffers[0]
		cmd.Stderr = buffers[1]
		require.NoError(t, cmd.Run(), "failed to clone Core Rule Set")
	} else {
		t.Logf("Syncing coreruleset to %s", crsRef)
		// #nosec G204
		fetch := exec.Command("git", "-C", crsCloneDir, "fetch", "--depth", "1", "origin",
			fmt.Sprintf("refs/tags/%s:refs/tags/%s", crsRef, crsRef))
		fetch.Stdout = buffers[0]
		fetch.Stderr = buffers[1]
		require.NoError(t, fetch.Run(), "failed to fetch Core Rule Set tag %s", crsRef)

		// #nosec G204
		checkout := exec.Command("git", "-C", crsCloneDir, "checkout", "--detach", "tags/"+crsRef)
		checkout.Stdout = buffers[0]
		checkout.Stderr = buffers[1]
		require.NoError(t, checkout.Run(), "failed to checkout Core Rule Set tag %s", crsRef)
	}

	require.DirExists(t, crsDir, "Core Rule Set checkout exists but %s is missing at %s", crsDir, crsRef)
	return crsDir
}
