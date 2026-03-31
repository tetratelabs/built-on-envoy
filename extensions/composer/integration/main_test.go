// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

//go:build integration

package integration

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mccutchen/go-httpbin/v2/httpbin"
	"github.com/stretchr/testify/require"
)

const (
	envoyLogFile  = "envoy.log"
	albedoLogFile = "albedo.log"

	envoyAdminPort = 9901
	smokePort      = 1063
	ftwPort        = 1064
	httpbinPort    = 1234
	albedoPort     = 1235

	defaultCRSDir = "./coreruleset/tests/regression/tests"
)

func TestIntegration(t *testing.T) {
	composerLibPath := requireComposerLib(t)
	integrationDir := requireIntegrationDir(t)

	startSmokeUpstream(t)
	startAlbedo(t, integrationDir)
	requireRunEnvoy(t, integrationDir, composerLibPath)
	requireEnvoyReady(t)

	t.Run("smoke", func(t *testing.T) {
		t.Log("waf smoke test")
		t.Run("allow_request", func(t *testing.T) {
			t.Log("test allow request")
			requireStatus(t, http.MethodGet, "http://localhost:1063/status/200", "", map[string]string{
				"coraza-e2e": "ok",
			}, http.StatusOK)
		})

		t.Run("block_request_headers", func(t *testing.T) {
			t.Log("test block request headers")
			requireStatus(t, http.MethodGet, "http://localhost:1063/status/200", "", nil, 424)
			requireStatus(t, http.MethodGet, "http://localhost:1063/admin", "", map[string]string{
				"coraza-e2e": "ok",
			}, http.StatusForbidden)
		})

		t.Run("block_request_body", func(t *testing.T) {
			t.Log("test block request body")
			requireStatus(t, http.MethodPost, "http://localhost:1063/anything", "payload=maliciouspayload", map[string]string{
				"content-type": "application/x-www-form-urlencoded",
				"coraza-e2e":   "ok",
			}, http.StatusForbidden)
		})

		t.Run("block_response_headers", func(t *testing.T) {
			t.Log("test block response headers")
			requireStatus(t, http.MethodGet, "http://localhost:1063/response-header-leak", "", map[string]string{
				"coraza-e2e": "ok",
			}, http.StatusForbidden)
		})

		t.Run("block_response_body", func(t *testing.T) {
			t.Log("test block response body")
			requireStatus(t, http.MethodGet, "http://localhost:1063/response-body-leak", "", map[string]string{
				"coraza-e2e": "ok",
			}, http.StatusForbidden)
		})
	})

	t.Run("ftw", func(t *testing.T) {
		t.Log("test waf aginst the OWASP CRS using ftw")
		crsDir := os.Getenv("CRS_DIR")
		if crsDir == "" {
			crsDir = defaultCRSDir
		}

		if _, err := os.Stat(crsDir); errors.Is(err, os.ErrNotExist) {
			t.Skipf("Core Rule Set path %s does not exist, skipping FTW tests. Run `make integration-setup` first.", crsDir)
		}

		cmdArgs := []string{
			"tool",
			"go-ftw",
			"run",
			"-d",
			crsDir,
			"--config",
			"ftw.yml",
			"--show-failures-only",
			"--read-timeout=30s",
			"--wait-for-timeout=0",
			"--max-marker-log-lines=2000",
			"--max-marker-retries=500",
		}
		if runtime.GOOS == "darwin" {
			cmdArgs = append(cmdArgs, "--rate-limit=5ms")
		}
		if include := os.Getenv("FTW_INCLUDE"); include != "" {
			cmdArgs = append(cmdArgs, "-i", include)
		}

		// #nosec G204
		cmd := exec.Command("go", cmdArgs...)
		cmd.Dir = integrationDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		require.NoError(t, cmd.Run())
	})
}

func requireComposerLib(t *testing.T) string {
	t.Helper()

	libPath := os.Getenv("COMPOSER_LIB_PATH")
	if libPath == "" {
		libPath = filepath.Join("..", "libcomposer.so")
	}
	absLibPath, err := filepath.Abs(libPath)
	require.NoError(t, err)
	require.FileExists(t, absLibPath, "composer shared library not found, run `make build` first or set COMPOSER_LIB_PATH")

	return absLibPath
}

func requireIntegrationDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)
	return dir
}

func startSmokeUpstream(t *testing.T) {
	t.Helper()

	httpbinHandler := httpbin.New()
	mux := http.NewServeMux()
	mux.HandleFunc("/response-header-leak", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("pass", "leak")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/response-body-leak", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("responsebodycode"))
	})
	mux.Handle("/", httpbinHandler)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", httpbinPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) && err != nil {
			t.Logf("http upstream server error: %v", err)
		}
	}()

	t.Cleanup(func() {
		_ = server.Close()
	})
}

func startAlbedo(t *testing.T, integrationDir string) {
	t.Helper()

	logPath := filepath.Join(integrationDir, albedoLogFile)
	_ = os.Remove(logPath)

	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logFile.Close() })

	// #nosec G204
	cmd := exec.Command("go", "tool", "albedo", "--port", strconv.Itoa(albedoPort))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start())

	// `go tool albedo` may take time on first run (toolchain/module setup). Make
	// sure it is listening before Envoy readiness checks depend on it.
	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", albedoPort))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode < http.StatusInternalServerError
	}, 180*time.Second, time.Second, "waiting for albedo listener %d", albedoPort)

	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
}

func requireRunEnvoy(t *testing.T, integrationDir, composerLibPath string) {
	t.Helper()

	logPath := filepath.Join(integrationDir, envoyLogFile)
	_ = os.Remove(logPath)

	logFile, err := os.Create(logPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = logFile.Close() })

	composerDir := filepath.Dir(composerLibPath)
	if envoyImage := os.Getenv("ENVOY_IMAGE"); envoyImage != "" {
		dockerArgs := []string{
			"run",
			"--network", "host",
			"-v", integrationDir + ":/integration",
			"-v", composerDir + ":/composer:ro",
			"-w", "/integration",
			"-e", "ENVOY_DYNAMIC_MODULES_SEARCH_PATH=/composer",
			"-e", "GODEBUG=cgocheck=0",
			"--rm",
		}
		if dockerPlatform := os.Getenv("DOCKER_PLATFORM"); dockerPlatform != "" {
			dockerArgs = append(dockerArgs, "--platform", dockerPlatform)
		}
		dockerArgs = append(dockerArgs,
			envoyImage,
			"-c", "/integration/envoy.yaml",
			"--log-level", "warn",
			"--concurrency", strconv.Itoa(max(runtime.NumCPU(), 2)),
			"--base-id", strconv.Itoa(time.Now().Nanosecond()),
		)

		// #nosec G204
		cmd := exec.Command("docker", dockerArgs...)
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = cmd.Process.Signal(os.Interrupt)
			_, _ = cmd.Process.Wait()
		})
		return
	}

	// #nosec G204
	versionCmd := exec.Command("envoy", "--version")
	out, err := versionCmd.CombinedOutput()
	require.NoError(t, err, "failed to get Envoy version")

	if expectedVersion := os.Getenv("ENVOY_VERSION"); expectedVersion != "" &&
		!strings.Contains(string(out), expectedVersion) {
		t.Skipf("local envoy version %q does not match ENVOY_VERSION=%s", strings.TrimSpace(string(out)), expectedVersion)
	}

	// #nosec G204
	cmd := exec.CommandContext(t.Context(), "envoy",
		"-c", filepath.Join(integrationDir, "envoy.yaml"),
		"--log-level", "warn",
		"--concurrency", strconv.Itoa(max(runtime.NumCPU(), 2)),
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(), "ENVOY_DYNAMIC_MODULES_SEARCH_PATH="+composerDir)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		_, _ = cmd.Process.Wait()
	})
}

func requireEnvoyReady(t *testing.T) {
	t.Helper()

	require.Eventually(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/ready", envoyAdminPort))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 60*time.Second, time.Second, "waiting for Envoy admin ready endpoint")

	for _, port := range []int{smokePort, ftwPort} {
		require.Eventually(t, func() bool {
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d/status/200", port), nil)
			if err != nil {
				return false
			}
			if port == smokePort {
				req.Header.Set("coraza-e2e", "ok")
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return false
			}
			defer resp.Body.Close()
			if port == smokePort {
				return resp.StatusCode == http.StatusOK
			}

			// FTW listener readiness should verify listener/upstream liveness only.
			// FTW/CRS rules may intentionally alter specific response codes during startup checks.
			return resp.StatusCode < http.StatusInternalServerError
		}, 60*time.Second, time.Second, "waiting for Envoy listener %d", port)
	}
}

func requireStatus(t *testing.T, method, url, body string, headers map[string]string, wantStatus int) {
	t.Helper()

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, url, bodyReader)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, wantStatus, resp.StatusCode)
}
