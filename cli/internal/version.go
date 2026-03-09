// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package internal

import (
	"fmt"
	"strconv"
	"strings"
)

// version is the current version of build. This is populated by the Go linker.
var version string

// CurrentVersion version with the Git information.
func CurrentVersion() Version {
	return parseGit(version)
}

// ParseVersion returns the parsed service's version information. (from raw git label)
func ParseVersion() string {
	return CurrentVersion().String()
}

// Version contains the version information extracted from a Version SHA.
type Version struct {
	ClosestTag   string
	CommitsAhead int
	Sha          string
}

func (v Version) String() string {
	switch {
	case v == Version{}:
		// unofficial version built without using the make tooling
		return "dev"
	case v.CommitsAhead != 0:
		// built from a non release commit point
		// In the version string, the commit tag is prefixed with "-g" (which stands for "git").
		// When printing the version string, remove that prefix to just show the real commit hash.
		return fmt.Sprintf("%s (%s, +%d)", v.Sha, v.ClosestTag, v.CommitsAhead)
	default:
		return v.ClosestTag
	}
}

// parseGit the given version string into a version object. The input version string
// is in the format:
//
//	<release tag>-<commits since release tag>-g<commit hash>
func parseGit(v string) Version {
	// ensure that at least we should be able to parse release tag, commits, hash
	if len(strings.Split(v, "-")) < 3 {
		return Version{}
	}

	// The git tag could contain '-' characters, so we start parting the version string
	// from the last parts, and concatenate the remaining ones at the beginning to reconstruct
	// the original tag if it had '-' characters.
	parts := strings.Split(v, "-")
	l := len(parts)
	commits, err := strconv.Atoi(parts[l-2])
	if err != nil { // extra safety but should never happen
		return Version{}
	}

	return Version{
		ClosestTag:   strings.Join(parts[:l-2], "-"),
		CommitsAhead: commits,
		Sha:          parts[l-1][1:], // remove the 'g' prefix
	}
}
