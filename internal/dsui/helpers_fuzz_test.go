// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"
)

// TODO(fuzzing-strategy): seeded-PRNG generative tests here are the primary
// style; a consolidation with Go-native Fuzz* targets is a future candidate.

// fuzzRand returns a deterministic PCG PRNG for reproducible generative tests.
func fuzzRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
}

// TestFuzzBuildSSEEntriesURL exercises the pure SSE-URL builder across the
// parameter space and checks the URL grammar is stable and precise.
//
// Invariants (the contract used by the SSE entry-list navigation):
//   - output always starts with /ds/sse/entries?
//   - always parseable as a URL
//   - `view` param present and equal to the input view
//   - feedId present in the query iff feedID > 0
//   - categoryId present in the query iff categoryID > 0
//   - searchQuery present iff non-empty
func TestFuzzBuildSSEEntriesURL(t *testing.T) {
	views := []string{"unread", "starred", "history", "search", "feed", "category", ""}
	for i := 0; i < 2000; i++ {
		r := fuzzRand(uint64(i))

		view := views[r.IntN(len(views))]
		feedID := r.Int64N(2000) - 500 // -500..1499, mixes zero and non-zero
		categoryID := r.Int64N(2000) - 500
		searchQuery := ""
		if r.IntN(2) == 0 {
			v := make([]byte, r.IntN(12))
			for j := range v {
				v[j] = byte(r.IntN(0x7F)) // ASCII-ish search query
			}
			searchQuery = strings.ReplaceAll(string(v), "&", "?") // keep query grammar clean
		}

		got := buildSSEEntriesURL(view, feedID, categoryID, searchQuery)

		if !strings.HasPrefix(got, "/ds/sse/entries?") {
			t.Fatalf("iter %d: unexpected prefix for view=%q feedID=%d categoryID=%d q=%q: %q",
				i, view, feedID, categoryID, searchQuery, got)
		}

		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("iter %d: url.Parse(%q) failed: %v", i, got, err)
		}

		q := u.Query()
		if view != "" {
			if got := q.Get("view"); got != view {
				t.Fatalf("iter %d: view param=%q, want %q (url %q)", i, got, view, got)
			}
		}
		if mid := q.Get("feedId"); (mid != "") != (feedID > 0) {
			t.Fatalf("iter %d: feedId param presence %q (value %v) mismatches feedID>0=%v (url %q)",
				i, mid, mid, feedID > 0, got)
		}
		if cid := q.Get("categoryId"); (cid != "") != (categoryID > 0) {
			t.Fatalf("iter %d: categoryId param presence %q mismatches categoryID>0=%v (url %q)",
				i, cid, categoryID > 0, got)
		}
		if sq := q.Get("searchQuery"); (sq != "") != (searchQuery != "") {
			t.Fatalf("iter %d: searchQuery presence %q mismatches non-empty=%v (url %q)",
				i, sq, searchQuery != "", got)
		}
	}
}

// TestFuzzMarkReadBehaviorRoundTrip checks that the mark-read-behavior mapping
// is idempotent: for the canonical behavior tokens, encode(decode(x)) == x; for
// any other token the decoder maps to the "no-auto" default.
func TestFuzzMarkReadBehaviorRoundTrip(t *testing.T) {
	canonical := []string{
		"no-auto",
		"on-view",
		"on-view-but-wait-for-player-completion",
		"on-player-completion",
	}
	// Every value the decoder accepts must be one of the canonical tokens, and
	// round-trips back to itself.
	for i := 0; i < 2000; i++ {
		r := fuzzRand(uint64(i))
		behavior := "on-view"
		if r.IntN(2) == 0 {
			b := make([]byte, r.IntN(14))
			for j := range b {
				b[j] = byte(r.IntN(0x7F))
			}
			behavior = strings.ReplaceAll(string(b), " ", "")
		}
		// Sometimes pick from canonical set explicitly.
		if r.IntN(4) == 0 {
			behavior = canonical[r.IntN(len(canonical))]
		}

		onView, onPlayer := applyMarkReadBehavior(behavior)
		encoded := markReadBehavior(onView, onPlayer)

		isCanonical := false
		for _, c := range canonical {
			if behavior == c {
				isCanonical = true
				break
			}
		}
		if isCanonical {
			if encoded != behavior {
				t.Fatalf("iter %d (seed %d): round-trip failed: apply(%q)=(%v,%v) -> mark(...)=%q",
					i, uint64(i), behavior, onView, onPlayer, encoded)
			}
		} else if encoded != "no-auto" {
			t.Fatalf("iter %d (seed %d): non-canonical %q must map to no-auto via (%v,%v), got %q",
				i, uint64(i), behavior, onView, onPlayer, encoded)
		}
	}
}
