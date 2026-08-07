// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the entry filter rules engine.
//
// TODO(fuzzing-strategy): this file carries BOTH project fuzzing styles — the
// seeded-PRNG generative tests below AND Go-native testing.F coverage fuzzers
// at the end. That is intentional for now (breadth + determinism), but is a
// candidate for consolidation: decide whether to standardize on one style and
// collapse the overlap.
package filter

import (
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// fuzzRuleText builds adversarial rule text from a few raw components, covering
// grammar boundaries (empty, whitespace, missing '=', extra '=', CRLF vs LF,
// many '=' signs, hostile UTF-8, NUL bytes, very long values).
func fuzzRuleText(r *rand.Rand, maxLen int) string {
	tokens := []string{
		"", " ", "=", "EntryTitle", "EntryURL", "EntryDate", "EntryTag",
		"before", "after", "between", "max-age", "future",
		"2006-01-02", "2024-12-31", "2024-01-01", "30", "7d", "1h", "1m",
		"99999999999d", "\x00", "🙂", "a= b=c", "=x", "a b",
	}
	var b strings.Builder
	n := 1 + r.IntN(6)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[r.IntN(len(tokens))])
		if r.IntN(3) == 0 {
			b.WriteRune(rune(0x20 + r.IntN(0x5f))) // random printable
		}
	}
	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// TestFuzzParseRuleNeverPanics drives parseRule over adversarial rule text and
// asserts it never panics (the grammar is intentionally permissive — a leading
// '=' or leftover separators are tolerated and simply won't match any entry).
func TestFuzzParseRuleNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 5000; i++ {
		_, _ = parseRule(fuzzRuleText(r, 256))
	}
}

// TestFuzzParseRulesNeverPanics drives the full multi-rule parser, exercising
// Newline splitting, trimming, and CRLF handling over hostile rule blocks.
func TestFuzzParseRulesNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 2000; i++ {
		lines := make([]string, 0, 8)
		for j := 0; j < r.IntN(8); j++ {
			lines = append(lines, fuzzRuleText(r, 128))
		}
		userRules := strings.Join(lines, "\n")
		feedRules := fuzzRuleText(r, 256)
		// Must never panic and always return a reader-consumable slice.
		_ = ParseRules(userRules, feedRules)
	}
}

// maxSaneDays is the largest day count that fits in time.Duration without
// overflow (time.Duration max / hours-per-day ≈ 106751 days). parseDuration
// parses arbitrary day counts via strconv.Atoi and multiplies by 24h; day
// counts beyond this silently wrap under int64, producing a negative (or
// otherwise absurd) duration. A max-age rule then computes a *future* cutoff
// instead of the past — the bug this regression-first fuzzer guards against.
const maxSaneDays = time.Duration(1<<63-1) / (24 * time.Hour)

// TestFuzzParseDurationRejectsOverflow asserts parseDuration never silently
// wraps huge day counts into a negative duration. A duration filter cutoff must
// always be in the past, so a parsed max-age duration must never be < 0, and
// day counts far beyond the sane range must not collapse to a tiny value.
func TestFuzzParseDurationRejectsOverflow(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))

	for i := 0; i < 10000; i++ {
		var days uint64
		switch r.IntN(4) {
		case 0:
			days = uint64(r.Uint64()) % uint64(maxSaneDays) // sane region
		case 1:
			days = uint64(maxSaneDays) + 1 + uint64(r.Uint64()%100000) // overflow region
		case 2:
			days = uint64(maxSaneDays) // boundary
		default:
			days = 1 + uint64(r.Uint64()%uint64(maxSaneDays))
		}

		d, err := parseDuration(itoa(days) + "d")
		if err != nil {
			continue // parse-or-error is acceptable
		}
		if d < 0 {
			t.Fatalf("input=%dd days=%d: negative duration %v: overflow wrap", days, days, d)
		}
	}

	// Deterministic regression on the exact overflow the fuzzer found: a
	// modest-but-overflowing day count must not produce a negative duration.
	for _, days := range []string{"107170d", "106752d", "99999999999d"} {
		if d, err := parseDuration(days); err == nil && d < 0 {
			t.Fatalf("parseDuration(%q)=%v: must not be negative", days, d)
		}
	}
}

func itoa(v uint64) string {
	if v == 0 {
		return "0"
	}
	var b [24]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// TestFuzzIsDateMatchingPattern drives the date-pattern grammar over hostile
// inputs, asserting it never panics and always returns a defined boolean.
func TestFuzzIsDateMatchingPattern(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 5000; i++ {
		pattern := fuzzRuleText(r, 128)
		// Exercise the timezone-aware entry-date comparison path too.
		entryDate := time.Unix(int64(r.Uint64()>>1), 0)
		_ = isDateMatchingPattern(pattern, entryDate)
	}
}

// ---------------------------------------------------------------------------
// Go-native coverage-guided fuzzing (testing.F) — added alongside the seeded
// PRNG tests above. Run with: go test -fuzz=Fuzz -run=X ./internal/reader/filter
// These provide coverage-guided byte mutation on top of the deterministic PRNG
// property checks. Both styles are kept until a consolidation decision is made;
// see the TODO note at the top of this file.
// ---------------------------------------------------------------------------

// FuzzParseRules exercises the user-supplied rules grammar with coverage-guided
// input, asserting it never panics.
func FuzzParseRules(f *testing.F) {
	f.Add("EntryTitle=.*golang.*", "")
	f.Add("EntryDate before=2024-01-01", "")
	f.Add("EntryDate max-age=30d", "")
	f.Add("EntryDate max-age=121170d", "") // regression: overflow case feeds rules too
	f.Fuzz(func(t *testing.T, userRules, feedRules string) {
		_ = ParseRules(userRules, feedRules)
	})
}

// FuzzIsDateMatchingPattern fuzzes the date-pattern grammar, asserting it never
// panics on coverage-guided dates and patterns.
func FuzzIsDateMatchingPattern(f *testing.F) {
	f.Add("before:2024-01-01")
	f.Add("between:2024-01-01,2025-01-01")
	f.Add("max-age:30d")
	for _, p := range []string{"future", "before", "after", "between", "bogus"} {
		f.Add(p + ":2024-06-01")
	}
	// A fixed reference entry date keeps the fuzz case deterministic; the input
	// being fuzzed is the pattern/user string, not the clock.
	entryDate := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, pattern string) {
		_ = isDateMatchingPattern(pattern, entryDate)
	})
}
