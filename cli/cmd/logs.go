// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tetratelabs/built-on-envoy/cli/internal/xdg"
)

// defaultFollowTail is the number of lines shown when --follow is set without an explicit --tail.
var defaultFollowTail = 20

// Logs is a command to print the CLI logs.
type Logs struct {
	Follow bool `short:"f" help:"Follow the log output (like tail -f)."`
	Tail   int  `short:"t" name:"tail" help:"Number of recent log lines to show. Defaults to 20 when --follow is set, 0 (all lines) otherwise."`

	output io.Writer `kong:"-"` // Internal field for testing
}

//go:embed logs_help.md
var logsHelp string

// Help provides detailed help for the logs command.
func (l *Logs) Help() string { return logsHelp }

// AfterApply initializes the logs command, ensuring the log file exists.
func (l *Logs) AfterApply() error {
	if l.Tail == 0 && l.Follow {
		l.Tail = defaultFollowTail
	}
	return nil
}

// Run executes the logs command.
func (l *Logs) Run(ctx context.Context, dirs *xdg.Directories, logger *slog.Logger) error {
	logger.Debug("handling logs command", "follow", l.Follow, "tail", l.Tail)

	if l.output == nil {
		l.output = os.Stdout
	}

	logFile := filepath.Clean(filepath.Join(dirs.StateHome, "boe.log"))
	if err := os.MkdirAll(dirs.StateHome, 0o750); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Eagerly create the file if it does not exist instead of failing
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if l.Tail > 0 {
		if err = printLastLines(f, l.output, l.Tail); err != nil {
			return err
		}
		if !l.Follow {
			return nil
		}
		// Seek to end of file to follow from here.
		if _, err = f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("failed to seek log file: %w", err)
		}
	} else {
		// Print all current content.
		if _, err = io.Copy(l.output, f); err != nil {
			return fmt.Errorf("failed to read log file: %w", err)
		}
		if !l.Follow {
			return nil
		}
		// File position is already at the end after io.Copy, ready to follow.
	}

	return followFile(ctx, f, l.output)
}

// tailChunkSize is the read-chunk size used when scanning backwards for line boundaries.
const tailChunkSize = 4096

// printLastLines writes the last n lines of f to out by scanning backwards from EOF.
// Memory usage is O(tailChunkSize), independent of file size.
func printLastLines(f *os.File, out io.Writer, n int) error {
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("failed to seek log file: %w", err)
	}
	if size == 0 {
		return nil
	}

	// Exclude a trailing newline so it doesn't count as an extra empty line.
	scanEnd := size
	lastByte := make([]byte, 1)
	if _, err := f.ReadAt(lastByte, size-1); err != nil {
		return fmt.Errorf("failed to read log file: %w", err)
	}
	if lastByte[0] == '\n' {
		scanEnd--
	}

	remaining := n
	startPos := int64(0)
	buf := make([]byte, tailChunkSize)

	for pos := scanEnd; pos > 0 && remaining > 0; {
		chunkStart := pos - int64(tailChunkSize)
		if chunkStart < 0 {
			chunkStart = 0
		}
		chunk := buf[:pos-chunkStart]
		if _, err := f.ReadAt(chunk, chunkStart); err != nil && err != io.EOF {
			return fmt.Errorf("failed to read log file: %w", err)
		}
		for i := len(chunk) - 1; i >= 0 && remaining > 0; i-- {
			if chunk[i] == '\n' {
				remaining--
				if remaining == 0 {
					startPos = chunkStart + int64(i) + 1
				}
			}
		}
		pos = chunkStart
	}

	if _, err := f.Seek(startPos, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek log file: %w", err)
	}
	if _, err := io.Copy(out, f); err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}
	return nil
}

// followFile polls f for new content and writes it to out until ctx is done.
func followFile(ctx context.Context, f *os.File, out io.Writer) error {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to read log file: %w", err)
		}
	}
}
