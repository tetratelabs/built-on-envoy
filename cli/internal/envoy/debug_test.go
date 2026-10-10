// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaltesting "github.com/tetratelabs/built-on-envoy/internal/testing"
)

func TestFindExecutable(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "dlv")
	require.NoError(t, os.WriteFile(bin, nil, 0o600))
	got, err := findExecutable(bin)
	require.NoError(t, err)
	require.Equal(t, bin, got)

	_, err = findExecutable(filepath.Join(t.TempDir(), "dlv"))
	require.ErrorContains(t, err, "not found")
}

func TestFindEnvoyPid(t *testing.T) {
	// Start a child process that looks like Envoy (it has the --use-dynamic-base-id flag) and one that does not.
	other := exec.Command("sleep", "30")
	require.NoError(t, other.Start())
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })

	envoy := exec.Command("sh", "-c", "sleep 30; exit 0", "--use-dynamic-base-id")
	require.NoError(t, envoy.Start())
	t.Cleanup(func() { _ = envoy.Process.Kill(); _ = envoy.Wait() })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pid, err := findEnvoyPid(ctx, os.Getpid())
	require.NoError(t, err)
	require.Equal(t, envoy.Process.Pid, pid)
}

func TestFindEnvoyPidTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	_, err := findEnvoyPid(ctx, os.Getpid())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDockerRunArgsDebug(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"boe", "debug", "--debug-server-path", "/usr/bin/dlv", "--debug-port=40000"}
	t.Cleanup(func() { os.Args = originalArgs })

	r := &RunnerDocker{
		Logger:     internaltesting.NewTLogger(t),
		ListenPort: 10000,
		AdminPort:  9901,
		Arch:       "arm64",
		Pull:       "missing",
		Debug:      &DebugOptions{Port: 40000},
	}
	want := []string{
		"run", "--rm",
		"--pull", "missing",
		"--platform", "linux/arm64",
		"-p", "10000:10000/tcp",
		"-p", "10000:10000/udp",
		"-p", "9901:9901",
		"-v", "boe-cache:" + containerVolumeDir,
		"-e", "BOE_ADMIN_ADDRESS=0.0.0.0:9901",
		"-e", "BOE_CONFIG_HOME=" + containerConfigHome,
		"-e", "BOE_DATA_HOME=" + containerDataHome,
		"-e", "BOE_STATE_HOME=" + containerStateHome,
		"-e", "BOE_RUNTIME_DIR=" + containerRuntimeDir,
		"--cap-add=SYS_PTRACE",
		"--security-opt", "seccomp=unconfined",
		"-p", "127.0.0.1:40000:40000",
		"-e", DebugListenHostEnv + "=0.0.0.0",
		"-e", "GOFLAGS=-buildvcs=false",
		"--entrypoint", "/boe", "ghcr.io/test/boe:latest",
		"debug", "--debug-port=40000",
	}
	require.Equal(t, want, r.dockerRunArgs("ghcr.io/test/boe:latest", nil))
}

func TestProcessLocalExtensionsDebug(t *testing.T) {
	// Mount the composer bundle root (once) for composer sub-extensions, and the extension itself otherwise.
	composer, err := filepath.Abs("../../../extensions/composer")
	require.NoError(t, err)
	lua, err := filepath.Abs("../../../extensions/example-lua")
	require.NoError(t, err)

	r := &RunnerDocker{Logger: internaltesting.NewTLogger(t), Debug: &DebugOptions{}}
	got, err := r.processLocalExtensionsDebug([]string{
		"../../../extensions/composer/opa",
		"../../../extensions/composer/example",
		"../../../extensions/example-lua",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"-v", composer + ":" + composer, "-v", lua + ":" + lua}, got)

	_, err = r.processLocalExtensionsDebug([]string{t.TempDir()})
	require.ErrorContains(t, err, "failed to load manifest")
}

func TestProcessCommandArgsDebug(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)

	r := &RunnerDocker{Logger: internaltesting.NewTLogger(t), Debug: &DebugOptions{}}
	got := r.processCommandArgs([]string{
		"boe", "debug",
		"--local", "./ext1",
		"--local=/abs/ext2",
		"--debug-server-path", "/usr/bin/dlv",
		"--debug-server-path=/usr/bin/dlv",
		"--docker-image-version", "dev",
		"--debug-port", "40000",
	})
	require.Equal(t, []string{
		"debug",
		"--local", filepath.Join(cwd, "ext1"),
		"--local=/abs/ext2",
		"--debug-port", "40000",
	}, got)
}

func TestDebugNotes(t *testing.T) {
	r := &RunnerDocker{Debug: &DebugOptions{Port: 2345, Notes: []string{"note 1\nline 2", "note 2"}}}
	args := r.debugArgs()
	env := args[len(args)-1]
	require.Equal(t, "-e", args[len(args)-2])

	// The notes are passed to the container and read back unchanged.
	name, value, _ := strings.Cut(env, "=")
	t.Setenv(name, value)
	require.Equal(t, []string{"note 1\nline 2", "note 2"}, DebugNotesFromEnv())

	// No notes, no env var.
	r.Debug.Notes = nil
	require.NotContains(t, strings.Join(r.debugArgs(), " "), debugNotesEnv)
}
