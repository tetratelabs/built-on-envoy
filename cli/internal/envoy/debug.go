// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package envoy

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/tetratelabs/built-on-envoy/cli/internal"
)

const (
	// DefaultDelvePort is the default port for the headless Delve server.
	DefaultDelvePort = 2345
	// DelveVersion is the version of Delve built by boe.
	DelveVersion = "v1.27.1"
	// delveModule is the Go module of Delve.
	delveModule = "github.com/go-delve/delve"
	// delvePatchRevision identifies the patches boe applies to Delve (see patchDelve). Bump it when
	// changing them, so previously built binaries are not reused.
	delvePatchRevision = "boe3"
	// DelveListenHostEnv overrides the address the headless Delve server listens on. Set by RunnerDocker
	// so Delve listens on all interfaces inside the container.
	DelveListenHostEnv = "BOE_DLV_LISTEN_HOST"
	// DelveInstallDirEnv overrides the directory where boe builds Delve. Set by RunnerDocker so Delve
	// is built in the container cache volume.
	DelveInstallDirEnv = "BOE_DLV_INSTALL_DIR"
	// debugNotesEnv passes the debug notes computed in the host to the boe process running in the container.
	debugNotesEnv = "BOE_DEBUG_NOTES"
	// debugNotesSeparator separates the debug notes in debugNotesEnv.
	debugNotesSeparator = "\x1e"

	// envoyProcessTimeout is how long to wait for the Envoy process to be found.
	envoyProcessTimeout = 30 * time.Second
	// delveReadyTimeout is how long to wait for the Delve server to accept connections.
	delveReadyTimeout = 60 * time.Second
)

// DebugOptions configures attaching a debugger to the Envoy process.
type DebugOptions struct {
	// DelvePath is the path to the dlv binary. If empty, boe builds a patched Delve in InstallDir.
	DelvePath string
	// InstallDir is the directory where boe builds Delve.
	InstallDir string
	// Rebuild forces rebuilding Delve even if it is already in InstallDir.
	Rebuild bool
	// DelvePort is the port the headless Delve server listens on.
	DelvePort uint32
	// Notes are additional messages printed after the instructions to connect to Delve.
	Notes []string

	dlvPath string // resolved path to the dlv binary
}

// resolveDelve returns the dlv binary to use. Unless a dlv binary is explicitly configured, boe builds
// its own patched Delve (see patchDelve), as upstream Delve cannot list the goroutines of a Go runtime that
// lives in a shared library loaded by a non-Go process (such as Envoy).
func (d *DebugOptions) resolveDelve(ctx context.Context, logger *slog.Logger) error {
	var (
		dlvPath string
		err     error
	)
	if d.DelvePath != "" {
		_, _ = fmt.Fprintf(os.Stderr, "%s⚠ Using %s: unpatched Delve versions may not be able to list goroutines.%s\n",
			internal.ANSIBold, d.DelvePath, internal.ANSIReset)
		dlvPath, err = findDelve(d.DelvePath)
	} else {
		dlvPath, err = installDelve(ctx, logger, cmp.Or(os.Getenv(DelveInstallDirEnv), d.InstallDir), d.Rebuild)
	}
	if err != nil {
		return err
	}
	logger.Debug("using delve", "path", dlvPath)
	d.dlvPath = dlvPath
	return nil
}

// listenAddress returns the address the headless Delve server listens on.
func (d *DebugOptions) listenAddress() string {
	host := cmp.Or(os.Getenv(DelveListenHostEnv), "127.0.0.1")
	return net.JoinHostPort(host, strconv.FormatUint(uint64(d.DelvePort), 10))
}

// delveArgs returns the arguments to start a headless Delve server attached to the given process.
func (d *DebugOptions) delveArgs(pid int) []string {
	return []string{
		"attach", strconv.Itoa(pid),
		"--headless",
		"--listen=" + d.listenAddress(),
		"--api-version=2",
		"--accept-multiclient",
		"--continue", // Do not stop Envoy when attaching; it keeps serving until a breakpoint is hit.
	}
}

// attach finds the Envoy process started by this process and attaches a headless Delve server to it.
// The Delve process is bound to runCtx so it is stopped when the run finishes.
func (d *DebugOptions) attach(runCtx, ctx context.Context, logger *slog.Logger) (*exec.Cmd, error) {
	findCtx, cancel := context.WithTimeout(ctx, envoyProcessTimeout)
	defer cancel()
	pid, err := findEnvoyPid(findCtx, os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("failed to find the Envoy process to attach the debugger to: %w", err)
	}

	_, _ = fmt.Fprintf(os.Stderr, "→ %sAttaching Delve to Envoy (PID %d)...%s\n", internal.ANSIBold, pid, internal.ANSIReset)

	// #nosec G204
	cmd := exec.CommandContext(runCtx, d.dlvPath, d.delveArgs(pid)...)
	cmd.Stdout = &prefixedWriter{prefix: "[dlv] ", w: os.Stderr}
	cmd.Stderr = &prefixedWriter{prefix: "[dlv] ", w: os.Stderr}
	// Delve detaches from Envoy (without killing it) when it receives SIGINT or SIGTERM.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	logger.Debug("starting delve", "cmd", cmd.String())
	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start Delve: %w", err)
	}

	if err = waitForTCPReady(net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(d.DelvePort), 10)), delveReadyTimeout); err != nil {
		stopDelve(logger, cmd)
		return nil, fmt.Errorf("delve did not become ready: %w", err)
	}

	return cmd, nil
}

// printInstructions prints how to connect to the Delve server attached to the given process.
func (d *DebugOptions) printInstructions(dlvPid int) {
	_, _ = fmt.Fprintf(os.Stderr, `
%[1]s🐞 Delve is attached to Envoy and listening on 127.0.0.1:%[3]d%[2]s (dlv PID %[4]d)
  → %[1]sVS Code:%[2]s run the "Attach to Envoy (boe debug)" launch configuration
  → %[1]sCLI:%[2]s     dlv connect 127.0.0.1:%[3]d

`, internal.ANSIBold, internal.ANSIReset, d.DelvePort, dlvPid)
	for _, note := range d.Notes {
		_, _ = fmt.Fprintln(os.Stderr, note)
	}
}

// DebugNotesFromEnv returns the debug notes passed in the environment by the host boe process.
func DebugNotesFromEnv() []string {
	if v := os.Getenv(debugNotesEnv); v != "" {
		return strings.Split(v, debugNotesSeparator)
	}
	return nil
}

// stopDelve stops the Delve server, which detaches it from Envoy without killing it.
func stopDelve(logger *slog.Logger, cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	logger.Debug("stopping delve", "pid", cmd.Process.Pid)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	_ = cmd.Wait()
}

// findDelve checks that the dlv binary exists at the given path.
func findDelve(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("dlv not found at %s: %w", path, err)
	}
	return path, nil
}

// installDelve builds the patched Delve in the given directory, unless it is already there and rebuild is false.
func installDelve(ctx context.Context, logger *slog.Logger, dir string, rebuild bool) (string, error) {
	dlvPath := filepath.Join(dir, fmt.Sprintf("dlv-%s-%s", DelveVersion, delvePatchRevision))
	if _, err := os.Stat(dlvPath); err == nil && !rebuild {
		return dlvPath, nil
	}
	_, _ = fmt.Fprintf(os.Stderr, "→ %sBuilding Delve %s...%s\n", internal.ANSIBold, DelveVersion, internal.ANSIReset)

	// Download the Delve sources to the module cache and copy them to a writable directory to patch them.
	// #nosec G204
	cmd := exec.CommandContext(ctx, "go", "mod", "download", "-json", delveModule+"@"+DelveVersion)
	cmd.Dir = os.TempDir() // Not in a module, so the download is not affected by any go.mod
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to download Delve sources: %w\nOutput: %s", err, out)
	}
	var mod struct{ Dir string }
	if err = json.Unmarshal(out, &mod); err != nil || mod.Dir == "" {
		return "", fmt.Errorf("failed to download Delve sources: unexpected output: %s", out)
	}
	src, err := os.MkdirTemp("", "boe-delve-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(src) }()
	if err = os.CopyFS(src, os.DirFS(mod.Dir)); err != nil {
		return "", fmt.Errorf("failed to copy Delve sources: %w", err)
	}
	if err = patchDelve(src); err != nil {
		return "", err
	}

	if err = os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	// #nosec G204
	cmd = exec.CommandContext(ctx, "go", "build", "-o", dlvPath, "./cmd/dlv")
	cmd.Dir = src
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-mod=mod")
	logger.Debug("building delve", "cmd", cmd.String(), "dir", src)
	if out, err = cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to build Delve: %w\nOutput: %s", err, out)
	}
	return dlvPath, nil
}

// delvePatch is a set of source replacements applied to a Delve source file.
type delvePatch struct {
	file         string
	replacements [][2]string
}

// delvePatches are the patches applied to Delve to debug a Go runtime that lives in a shared library
// loaded by a non-Go process (libcomposer.so in Envoy) after attaching to it:
//
//   - Delve only looks for the runtime goroutine list (runtime.allgs) in the main executable, and does
//     it when the target is created, before the shared libraries are loaded when attaching. Listing
//     goroutines then fails with "could not find goroutine array", and IDEs cannot show where the
//     program stopped. The patch looks for runtime.allgs in all the loaded images, and retries when
//     the goroutines are first requested.
//   - Delve stops the target when shared libraries are loaded until it sees a Go image being loaded.
//     When attaching, the Go image is already loaded, so the first library loaded afterwards (e.g. an
//     NSS module during a DNS or user lookup) stops Envoy inside the dynamic linker, which IDEs report
//     as a breakpoint hit in an unknown location. The patch sets up the Go image when attaching.
//   - On x86_64, Delve finds the current goroutine of a thread through the G pointer in thread local
//     storage, but only computes its location for the executable. When the Go runtime is in a shared
//     library loaded by a non-Go process, Delve reads the wrong location and reports a different
//     (usually parked) goroutine as the current one, so IDEs show the wrong location when a breakpoint
//     is hit. The patch reads the location of the G pointer from the GOT entry of the library.
var delvePatches = []delvePatch{
	{
		file: "pkg/proc/goroutine_cache.go",
		replacements: [][2]string{
			{
				`	exeimage := bi.Images[0]
	rdr := exeimage.DwarfReader()
	if rdr == nil {
		return
	}

	gcache.allglenAddr, _ = rdr.AddrFor("runtime.allglen", exeimage.StaticBase, bi.Arch.PtrSize())

	rdr.Seek(0)
	gcache.allgentryAddr, err = rdr.AddrFor("runtime.allgs", exeimage.StaticBase, bi.Arch.PtrSize())
	if err != nil {
		// try old name (pre Go 1.6)
		gcache.allgentryAddr, _ = rdr.AddrFor("runtime.allg", exeimage.StaticBase, bi.Arch.PtrSize())
	}
`,
				`	for _, image := range bi.Images {
		rdr := image.DwarfReader()
		if rdr == nil {
			continue
		}
		allglenAddr, err := rdr.AddrFor("runtime.allglen", image.StaticBase, bi.Arch.PtrSize())
		if err != nil {
			continue
		}
		rdr.Seek(0)
		allgentryAddr, err := rdr.AddrFor("runtime.allgs", image.StaticBase, bi.Arch.PtrSize())
		if err != nil {
			continue
		}
		gcache.allglenAddr, gcache.allgentryAddr = allglenAddr, allgentryAddr
		return
	}
	_ = err
`,
			},
			{
				`func (gcache *goroutineCache) getRuntimeAllg(bi *BinaryInfo, mem MemoryReadWriter) (uint64, uint64, error) {
	if gcache.allglenAddr == 0 || gcache.allgentryAddr == 0 {
		return 0, 0, ErrNoRuntimeAllG
`,
				`func (gcache *goroutineCache) getRuntimeAllg(bi *BinaryInfo, mem MemoryReadWriter) (uint64, uint64, error) {
	if gcache.allglenAddr == 0 || gcache.allgentryAddr == 0 {
		gcache.init(bi)
	}
	if gcache.allglenAddr == 0 || gcache.allgentryAddr == 0 {
		return 0, 0, ErrNoRuntimeAllG
`,
			},
		},
	},
	{
		file: "pkg/proc/target.go",
		replacements: [][2]string{
			{
				`func (t *Target) sharedLibCallback(th Thread, tgt *Target) (bool, error) {
`,
				`// InitGoImage sets up the Go-specific breakpoints if a Go image is already loaded (e.g. when
// attaching to a non-Go process that already loaded a Go shared library), so that later shared
// library loads do not stop the target.
func (t *Target) InitGoImage() {
	if !t.BinInfo().HasGoImage() {
		return
	}
	t.onInitialGoImage.Do(func() {
		t.createUnrecoveredPanicBreakpoint()
		t.createFatalThrowBreakpoint()
		t.createPluginOpenBreakpoint()
	})
}

func (t *Target) sharedLibCallback(th Thread, tgt *Target) (bool, error) {
`,
			},
		},
	},
	{
		file: "pkg/proc/native/proc_linux.go",
		replacements: [][2]string{
			{
				`	err = linutil.ElfUpdateSharedObjects(dbp)
	if err != nil {
		return nil, err
	}
	setupSharedLibBreakpoint(dbp, tgt)
	return tgt, nil
}
`,
				`	err = linutil.ElfUpdateSharedObjects(dbp)
	if err != nil {
		return nil, err
	}
	setupSharedLibBreakpoint(dbp, tgt)
	if sel := tgt.Selected; sel != nil && len(sel.BinInfo().Images) > 0 && !sel.BinInfo().Images[0].IsGo {
		sel.InitGoImage()
	}
	return tgt, nil
}
`,
			},
		},
	},
	{
		file: "pkg/proc/bininfo.go",
		replacements: [][2]string{
			{
				`		// determine g struct offset only when loading the executable file
		wg.Add(1)
		go bi.setGStructOffsetElf(image, dwarfFile, wg)
	}
`,
				`		// determine g struct offset only when loading the executable file
		wg.Add(1)
		go bi.setGStructOffsetElf(image, dwarfFile, wg)
	} else if elfFile.Machine == elf.EM_X86_64 && len(bi.Images) > 0 && !bi.Images[0].IsGo {
		bi.setGStructOffsetElfSharedLib(image, elfFile)
	}
`,
			},
			{
				`func getSymbol(image *Image, logger logflags.Logger, exe *elf.File, name string) *elf.Symbol {
`,
				`// setGStructOffsetElfSharedLib sets the offset of the G pointer in thread
// local storage when the Go runtime is in a shared library built with
// buildmode=c-shared and loaded by a non-Go executable on x86_64.
//
// The library accesses runtime.tlsg with the initial-exec TLS model: the
// offset of the G pointer from the thread pointer is stored in a GOT entry,
// filled by the dynamic linker through a R_X86_64_TPOFF64 relocation. The
// offset depends on where the library's TLS block was placed at load time, so
// it can't be computed from the file: the GOT entry is read from the target
// memory instead.
func (bi *BinaryInfo) setGStructOffsetElfSharedLib(image *Image, exe *elf.File) {
	if bi.gStructOffsetIsPtr {
		// Already set by another Go shared library.
		return
	}
	tlsg := getSymbol(image, bi.logger, exe, "runtime.tlsg")
	if tlsg == nil {
		return
	}
	rela := exe.Section(".rela.dyn")
	if rela == nil {
		return
	}
	data, err := rela.Data()
	if err != nil {
		return
	}
	var (
		dynsyms, _ = exe.DynamicSymbols()
		got        uint64
	)
	const relaSize = 24 // sizeof(Elf64_Rela)
	for i := 0; i+relaSize <= len(data); i += relaSize {
		off := exe.ByteOrder.Uint64(data[i:])
		info := exe.ByteOrder.Uint64(data[i+8:])
		addend := exe.ByteOrder.Uint64(data[i+16:])
		if elf.R_X86_64(elf.R_TYPE64(info)) != elf.R_X86_64_TPOFF64 {
			continue
		}
		// runtime.tlsg is a local symbol, so the relocation usually has no
		// symbol and its addend is the offset of runtime.tlsg in the TLS block.
		sym := elf.R_SYM64(info)
		if (sym == 0 && addend == tlsg.Value) ||
			(sym > 0 && int(sym) <= len(dynsyms) && dynsyms[sym-1].Name == "runtime.tlsg") {
			got = off
			break
		}
	}
	if got == 0 {
		return
	}
	bi.gStructOffset = image.StaticBase + got
	bi.gStructOffsetIsPtr = true
}

func getSymbol(image *Image, logger logflags.Logger, exe *elf.File, name string) *elf.Symbol {
`,
			},
		},
	},
}

// patchDelve applies delvePatches to the Delve sources in the given directory.
func patchDelve(src string) error {
	for _, p := range delvePatches {
		file := filepath.Join(src, p.file)
		content, err := os.ReadFile(file) //nolint:gosec // path built from a temp dir
		if err != nil {
			return fmt.Errorf("failed to patch Delve: %w", err)
		}
		patched := string(content)
		for _, r := range p.replacements {
			if strings.Count(patched, r[0]) != 1 {
				return fmt.Errorf("failed to patch Delve: unexpected contents in %s", p.file)
			}
			patched = strings.Replace(patched, r[0], r[1], 1)
		}
		if err = os.WriteFile(file, []byte(patched), 0o600); err != nil {
			return fmt.Errorf("failed to patch Delve: %w", err)
		}
	}
	return nil
}

// findEnvoyPid polls the children of the given process until it finds the Envoy process started by func-e.
// Envoy is identified by the --use-dynamic-base-id flag that RunnerFuncE always passes, which distinguishes
// it from other child processes such as ext_proc servers.
func findEnvoyPid(ctx context.Context, parentPid int) (int, error) {
	parent, err := process.NewProcessWithContext(ctx, int32(parentPid)) //nolint:gosec // pid never overflows int32
	if err != nil {
		return 0, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		children, _ := parent.ChildrenWithContext(ctx)
		for _, child := range children {
			cmdline, err := child.CmdlineSliceWithContext(ctx)
			if err == nil && slices.Contains(cmdline, "--use-dynamic-base-id") {
				return int(child.Pid), nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

// mergeGODEBUG merges the given settings into an existing GODEBUG value. Settings in add override
// existing settings with the same key.
func mergeGODEBUG(existing string, add ...string) string {
	keys := make(map[string]bool, len(add))
	for _, a := range add {
		k, _, _ := strings.Cut(a, "=")
		keys[k] = true
	}
	var merged []string
	for _, e := range strings.Split(existing, ",") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if k, _, _ := strings.Cut(e, "="); keys[k] {
			continue
		}
		merged = append(merged, e)
	}
	return strings.Join(append(merged, add...), ",")
}
