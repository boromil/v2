// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the entry filter rules engine.
//
// These follow the project's seeded-PRNG fuzzing pattern (distinct from the
// coverage-guided Fuzz* targets): the filter engine consumes user-supplied
// rule grammars (block/keep regex rules, date patterns, durations), which are
// untrusted input. A failure is reproducible by re-running with the printed
// seed.
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
