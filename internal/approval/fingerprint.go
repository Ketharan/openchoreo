// Copyright 2026 The OpenChoreo Authors
// SPDX-License-Identifier: Apache-2.0

// Package approval implements the approval gate: deciding whether an attempted
// action needs a human decision, and whether one has already been given for
// exactly that change.
package approval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Fingerprint returns a stable digest of the state an approval is granted for.
//
// The digest is what stops an approval being replayed against a different
// change: the gate only lets an action proceed when the state being applied
// hashes to the value recorded on the approved request. Round-tripping through
// a map before marshalling makes the result independent of key order and of
// insignificant formatting in the caller's JSON, so re-serialising the same
// logical state does not invalidate an approval.
func Fingerprint(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("cannot fingerprint empty state")
	}

	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("state is not valid JSON: %w", err)
	}

	// Go marshals map keys in sorted order, so this is canonical for any value
	// reachable from JSON.
	canonical, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("canonicalising state: %w", err)
	}

	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// FingerprintMatches reports whether two fingerprints refer to the same state.
// Compared as opaque strings so the digest algorithm can change without every
// caller needing to know.
func FingerprintMatches(a, b string) bool {
	return a != "" && a == b
}
