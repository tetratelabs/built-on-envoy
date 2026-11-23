// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

package waf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ctypes "github.com/corazawaf/coraza/v3/types"
)

// metadataConfig holds the dynamic metadata settings of a filter configuration. Its zero value is the default.
//
// Like the metrics settings, these are read from the filter configuration only: per-route configurations do not
// override them. It is parsed by parseConfig.
type metadataConfig struct {
	// matchedRules enables the matched_rules key.
	matchedRules bool
}

// parseMetadata parses the metadata object of the filter configuration, if there is one. Unknown keys in it are
// rejected, so that a misspelled setting is not silently ignored.
func parseMetadata(raw json.RawMessage) (metadataConfig, error) {
	var cfg metadataConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	var m struct {
		MatchedRules *bool `json:"matched_rules"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return metadataConfig{}, fmt.Errorf("invalid metadata: %w", err)
	}
	if m.MatchedRules != nil {
		cfg.matchedRules = *m.MatchedRules
	}
	return cfg, nil
}

// matchedRuleIDs returns the IDs of the rules a transaction matched, comma-separated in the order they matched, each
// once. Rules without the log action are skipped, as Coraza does for its error log callback. It returns "" when no
// rule is left.
func matchedRuleIDs(matched []ctypes.MatchedRule) string {
	var b strings.Builder
	for i, mr := range matched {
		if !isLogged(mr) || matchedBefore(matched[:i], mr.Rule().ID()) {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(mr.Rule().ID()))
	}
	return b.String()
}

// isLogged reports whether a matched rule has the log action, set on the rule or by SecDefaultAction.
// ctypes.MatchedRule does not expose it, but Coraza's implementation does; a matched rule whose
// implementation does not is kept.
func isLogged(mr ctypes.MatchedRule) bool {
	l, ok := mr.(interface{ Log() bool })
	return !ok || l.Log()
}

// matchedBefore reports whether the rule with the given ID is among the previous matches, so that a
// rule is kept once per transaction.
func matchedBefore(previous []ctypes.MatchedRule, id int) bool {
	for _, mr := range previous {
		if mr.Rule().ID() == id {
			return true
		}
	}
	return false
}
