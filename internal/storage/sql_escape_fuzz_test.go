// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for SQL string-escaping helpers.
//
// escapeFTS5Query and parsePostgresArray transform user-controlled strings on
// their way into SQL. They are pure and must never panic. escapeFTS5Query also
// has a precise, modelable contract (wrap dotted-numeric tokens in quotes, leave
// everything else alone). Seeded-PRNG tests plus a native testing.F complement.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist; candidate for
// consolidation onto one style.
package storage

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// fuzzSearchQuery builds an adversarial FTS5 search query from token soup.
func fuzzSearchQuery(r *rand.Rand) string {
	tokens := []string{
		"2.0.8", "1.5", "abc", "a.b", "1.2.3.4", "version", ".", "..", "1.",
		".1", "a1.", "1a.2", "3.14", "x.y.z", `"quoted"`, "0.0", "007", "2.0",
		"pkg/v1.2.3", "japanese 日本語", "a_b.c", "tab\t", "dot.space .x",
		"leading.digit 9.9", "…unicode…", "",
	}
	var b strings.Builder
	n := 1 + r.IntN(8)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[r.IntN(len(tokens))])
		if r.IntN(3) == 0 {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// TestFuzzEscapeFTS5NeverPanics asserts escapeFTS5Query never panics and that
// its output has as many tokens as the input (quoting never merges/splits).
func TestFuzzEscapeFTS5NeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(91, 92))
	for i := 0; i < 8000; i++ {
		q := fuzzSearchQuery(r)
		out := escapeFTS5Query(q)
		if len(strings.Fields(out)) != len(strings.Fields(q)) {
			t.Fatalf("iter=%d input=%q: token count changed -> %q", i, q, out)
		}
	}
}

// TestFuzzEscapeFTS5Quoting asserts the precise spec: a token with a numeric dot
// is wrapped in double quotes; other tokens pass through unchanged.
func TestFuzzEscapeFTS5Quoting(t *testing.T) {
	r := rand.New(rand.NewPCG(93, 94))
	for i := 0; i < 8000; i++ {
		q := fuzzSearchQuery(r)
		inTokens := strings.Fields(q)
		outTokens := strings.Fields(escapeFTS5Query(q))

		for j := range inTokens {
			if containsNumericDot(inTokens[j]) {
				if outTokens[j] != `"`+inTokens[j]+`"` {
					t.Fatalf("iter=%d token=%q: expected quoted, got %q", i, inTokens[j], outTokens[j])
				}
			} else if outTokens[j] != inTokens[j] {
				t.Fatalf("iter=%d token=%q: expected unchanged, got %q", i, inTokens[j], outTokens[j])
			}
		}
	}
}

// TestFuzzParsePostgresArray asserts the naive-comma-split parser never panics
// and is deterministic (same input -> same output). NOTE: it does NOT correctly
// handle a comma inside a quoted element (e.g. a tag "hello, world"), which the
// naive split mis-parses into multiple fields; that is a pre-existing, noted
// limitation of the helper (it reads Postgres-produced array literals), not a
// panic on any input.
func TestFuzzParsePostgresArray(t *testing.T) {
	r := rand.New(rand.NewPCG(95, 96))
	fields := []string{"a", "b c", `"x,y"`, `""`, "1", " ", "a\"b", "日本語", `{`, "}"}
	for i := 0; i < 8000; i++ {
		n := r.IntN(6)
		parts := make([]string, n)
		for j := 0; j < n; j++ {
			parts[j] = fields[r.IntN(len(fields))]
		}
		arr := "{" + strings.Join(parts, ",") + "}"
		// Must never panic. Determinism: reparse yields the same result.
		first := parsePostgresArray(arr)
		second := parsePostgresArray(arr)
		if len(first) != len(second) {
			t.Fatalf("iter=%d input=%q: non-deterministic result", i, arr)
		}
		for j := range first {
			if first[j] != second[j] {
				t.Fatalf("iter=%d input=%q: non-deterministic element %d", i, arr, j)
			}
		}
	}
}

// FuzzEscapeFTS5Query is the native coverage-guided complement, asserting the
// no-panic and token-preservation invariant.
func FuzzEscapeFTS5Query(f *testing.F) {
	f.Add("2.0.8")
	f.Add("version 1.2 abc")
	f.Add("a.b")
	f.Add("1. 2. x")
	f.Fuzz(func(t *testing.T, q string) {
		out := escapeFTS5Query(q)
		if len(strings.Fields(out)) != len(strings.Fields(q)) {
			t.Fatalf("input=%q: token count changed -> %q", q, out)
		}
	})
}

// FuzzParsePostgresArray is the native coverage-guided complement.
func FuzzParsePostgresArray(f *testing.F) {
	f.Add("{}")
	f.Add("{a,b,c}")
	f.Add(`{"x,y","", 1}`)
	f.Add("{日本語}")
	f.Fuzz(func(t *testing.T, s string) {
		_ = parsePostgresArray(s) // never panics; parse-or-through
	})
}
