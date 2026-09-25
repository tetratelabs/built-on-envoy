// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package coraza

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"slices"
)

// recordingFS records the files and globs a WAF build reads (includes, operator data files),
// so the WAF is reused only while they are unchanged. Coraza uses fs.ReadFile and fs.Glob;
// any direct Open cannot be tracked and prevents reuse.
type recordingFS struct {
	root   fs.FS
	inputs *wafInputs
}

func newRecordingFS(root fs.FS) *recordingFS {
	return &recordingFS{root: root, inputs: &wafInputs{
		files: map[string]fileState{},
		globs: map[string][]string{},
	}}
}

func (r *recordingFS) Open(name string) (fs.File, error) {
	r.inputs.untracked = true
	return r.root.Open(name)
}

func (r *recordingFS) ReadFile(name string) ([]byte, error) {
	content, err := fs.ReadFile(r.root, name)
	r.inputs.files[name] = newFileState(content, err)
	return content, err
}

func (r *recordingFS) Glob(pattern string) ([]string, error) {
	matches, err := fs.Glob(r.root, pattern)
	if err != nil {
		r.inputs.untracked = true
		return nil, err
	}
	r.inputs.globs[pattern] = slices.Clone(matches)
	return matches, nil
}

// wafInputs are the filesystem inputs a WAF was built from.
type wafInputs struct {
	files     map[string]fileState
	globs     map[string][]string
	untracked bool
}

// fileState is a file's content hash, or why it could not be read. Missing files are recorded
// too: operators search several directories and use the first file found.
type fileState struct {
	sum      [sha256.Size]byte
	missing  bool
	readFail bool
}

func newFileState(content []byte, err error) fileState {
	switch {
	case err == nil:
		return fileState{sum: sha256.Sum256(content)}
	case errors.Is(err, fs.ErrNotExist):
		return fileState{missing: true}
	default:
		return fileState{readFail: true}
	}
}

// unchanged reports whether reading the inputs from root gives the same results as
// when the WAF was built.
func (in *wafInputs) unchanged(root fs.FS) bool {
	if in.untracked {
		return false
	}
	for pattern, matches := range in.globs {
		current, err := fs.Glob(root, pattern)
		if err != nil || !slices.Equal(current, matches) {
			return false
		}
	}
	for name, state := range in.files {
		if newFileState(fs.ReadFile(root, name)) != state {
			return false
		}
	}
	return true
}
