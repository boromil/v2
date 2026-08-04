// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the feed date parser.
//
// Feed date strings are untrusted input parsed against ~200 hand-rolled
// layouts, with a timezone-replacer and an offset-clamping step. These seeded-
// PRNG tests (distinct from the coverage-guided FuzzParse in parser_test.go)
// assert parse-or-error and that any success is a sane, well-formed time —
// never a panic or a timezone out of the RFC-blessed [-12h,+14h] offset range.
//
// TODO(fuzzing-strategy): FuzzParseTimezoneRange below adds Go-native
// coverage-guided fuzzing alongside this PRNG file; both continue until a
// consolidation decision is made.
package date

import (
	"math/rand/v2"
	"testing"
)

// fuzzDateString builds adversarial date strings from time-format fragments
// (layouts, zone abbreviations, separators, numeric timestamps) plus noise.
func fuzzDateString(r *rand.Rand, maxLen int) string {
	fragments := []string{
		"2006-01-02", "2006-01-02T15:04:05Z", "2006-01-02 15:04:05",
		"Mon, 02 Jan 2006 15:04:05 MST", "Mon, 2 Jan 2006 15:04:05 -0700",
		"03 Jan 2006", "2006/01/02", "02.01.2006", "Jan 2, 2006",
		"20060102", "151200", "-0700", "UTC", "GMT", "America/Los_Angeles",
		"PST", "PDT", "EST", "EDT", "+14:00", "-12:00", "+14:01", "-12:01",
		":", "/", "-", " ", "\t", "\n", ",", "T", "Z", "+", "Jan", "January",
	}
	var b []rune
	n := 1 + r.IntN(8)
	for i := 0; i < n; i++ {
		f := fragments[r.IntN(len(fragments))]
		for _, cr := range f {
			b = append(b, cr)
		}
		if r.IntN(4) == 0 {
			b = append(b, rune(0x20+r.IntN(0x5f)))
		}
	}
	s := string(b)
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// TestFuzzDateParseOrErrorNeverPanics drives Parse over adversarial strings and
// asserts it never panics and always yields a valid time or an error.
func TestFuzzDateParseOrErrorNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 22))
	for i := 0; i < 8000; i++ {
		input := fuzzDateString(r, 256)
		parsed, err := Parse(input)
		if err == nil && parsed.IsZero() {
			t.Fatalf("iter=%d input=%q: success returned zero time", i, input)
		}
	}
}

// TestFuzzDateTimezoneRange asserts that any successful parse stays within the
// RFC-blessed UTC offset range [-12h, +14h]; checkTimezoneRange is supposed to
// enforce this and must never panic.
func TestFuzzDateTimezoneRange(t *testing.T) {
	r := rand.New(rand.NewPCG(23, 24))
	for i := 0; i < 8000; i++ {
		// Bias toward zone-abbreviation fragments that exercise the zone paths.
		input := fuzzDateString(r, 256)
		if r.IntN(2) == 0 {
			input += []string{" EST", " PDT", " +14:00", " -12:00", " +14:01", " -12:01"}[r.IntN(6)]
		}
		parsed, err := Parse(input)
		if err != nil {
			continue
		}
		_, offset := parsed.Zone()
		if offset > 14*60*60 || offset < -12*60*60 {
			t.Fatalf("iter=%d input=%q: offset %d out of [-12h,+14h]", i, input, offset)
		}
	}
}

// TestFuzzDateNumericBoundaries bounds the date parser over raw epoch timestamps,
// including huge and boundary values, asserting parse-or-error without panic.
func TestFuzzDateNumericBoundaries(t *testing.T) {
	r := rand.New(rand.NewPCG(25, 26))
	for i := 0; i < 2000; i++ {
		// Numeric timestamps (seconds epoch) — including huge/negative values.
		v := r.Uint64()
		_, err := Parse(itoa(v))
		_ = err
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

// FuzzParseTimezoneRange is a Go-native coverage-guided fuzzer asserting the
// checkTimezoneRange invariant on any successful parse: the resulting offset
// must stay within [-12h,+14h] and Parse must never panic. complements
// TestFuzzDateTimezoneRange; run with:
//   go test -fuzz=FuzzParseTimezoneRange -run=X ./internal/reader/date
func FuzzParseTimezoneRange(f *testing.F) {
	f.Add("2017-12-22T22:09:49+14:00")
	f.Add("2017-12-22T22:09:49-12:00")
	f.Add("Fri, 31 Mar 2023 20:19:00 PST")
	f.Add("2006-01-02T15:04:05 EST")
	f.Fuzz(func(t *testing.T, s string) {
		parsed, err := Parse(s)
		if err != nil {
			return
		}
		_, offset := parsed.Zone()
		if offset > 14*60*60 || offset < -12*60*60 {
			t.Fatalf("offset %d out of [-12h,+14h] for input %q", offset, s)
		}
	})
}
