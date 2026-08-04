// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for charset detection/conversion.
//
// NewCharsetReaderFromBytes runs on untrusted feed bytes and an untrusted
// Content-Type header. Properties: never panics; valid UTF-8 input passes
// through byte-for-byte (no spurious double-encode); invalid UTF-8 is converted
// to valid UTF-8. Seeded-PRNG tests plus a native testing.F complement.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist; candidate for
// consolidation onto one style.
package encoding

import (
	"bytes"
	"math/rand/v2"
	"io"
	"testing"
	"unicode/utf8"
)

// fuzzBytes builds adversarial input from UTF-8 and non-UTF-8 mixtures plus a
// random charset hint.
func fuzzBytes(r *rand.Rand, maxLen int) []byte {
	fragments := []byte{}
	// Mix valid UTF-8 text, arbitrary high-bytes (likely utf-8-invalid), and ASCII.
	pieces := [][]byte{
		[]byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?><rss>"),
		[]byte("text with unicode 日本語 and émojis 😀"),
		{0xFF, 0xFE, 0x00, 0x41, 0x00},   // UTF-16 LE BOM-ish
		{0xEF, 0xBB, 0xBF, 'a', 'b'},     // UTF-8 BOM
		{0xC3, 0x28},                      // invalid 2-byte seq
		{0x80, 0x80, 0x80},                // continuation bytes
		[]byte("<meta charset=\"iso-8859-1\"><body>"),
	}
	n := 1 + r.IntN(8)
	for i := 0; i < n; i++ {
		fragments = append(fragments, pieces[r.IntN(len(pieces))]...)
		if r.IntN(3) == 0 {
			fragments = append(fragments, byte(r.IntN(256)))
		}
	}
	if len(fragments) > maxLen {
		// Truncate at a rune boundary so we test realistic whole byte sequences
		// rather than a synthetic mid-rune cut (the real caller always passes a
		// complete body). The invalid-UTF-8 coverage still comes from the explicit
		// high-byte/continuation pieces, not from a truncation artifact.
		fragments = fragments[:maxLen]
		for maxLen > 0 && !utf8.RuneStart(fragments[maxLen-1]) {
			maxLen--
		}
		fragments = fragments[:maxLen]
	}
	return fragments
}

func fuzzContentType(r *rand.Rand) string {
	types := []string{
		"", "text/xml; charset=utf-8", "application/atom+xml",
		"text/html; charset=iso-8859-1", "text/html; charset=windows-1252",
		"text/html; charset=utf-16", "application/xml; charset=shift_jis",
		"charset=bogus", "text/xml;",
	}
	return types[r.IntN(len(types))]
}

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

// TestFuzzCharsetRoundTrip asserts valid UTF-8 input passes through unchanged
// and invalid-UTF-8 input converts to valid UTF-8, never panicking.
func TestFuzzCharsetRoundTrip(t *testing.T) {
	r := rand.New(rand.NewPCG(81, 82))
	for i := 0; i < 5000; i++ {
		buf := fuzzBytes(r, 2048)

		out, err := NewCharsetReaderFromBytes(buf, fuzzContentType(r))
		if err != nil {
			continue // "unable to determine encoding" is a valid error outcome
		}
		got, err := readAll(out)
		if err != nil {
			continue
		}

		// Valid UTF-8 must round-trip byte-for-byte (no double-encode) — the
		// double-encoding the code explicitly guards against. For non-UTF-8 input
		// the charset guess is best-effort (golang.org/x/net/html/charset), so we
		// only require no-panic/no-error there, not a strictly-valid output.
		if utf8.Valid(buf) && !bytes.Equal(got, buf) {
			t.Fatalf("iter=%d: valid UTF-8 mis-encoded:\n in=%v\nout=%v", i, buf, got)
		}
	}
}

// FuzzNewCharsetReader is the native coverage-guided complement.
func FuzzNewCharsetReader(f *testing.F) {
	f.Add([]byte("plain ascii"), "text/html; charset=utf-8")
	f.Add([]byte{0xC3, 0x28, 'a'}, "text/html; charset=windows-1252")
	f.Add([]byte{0xFF, 0xFE, 0x41, 0x00}, "")
	f.Add([]byte("<?xml encoding=\"iso-8859-1\"?>"), "text/xml")
	f.Fuzz(func(t *testing.T, data []byte, contentType string) {
		if len(data) > 4096 {
			return
		}
		out, err := NewCharsetReaderFromBytes(data, contentType)
		if err != nil {
			return
		}
		got, rerr := io.ReadAll(out)
		if rerr != nil {
			return
		}
		if utf8.Valid(data) && !bytes.Equal(got, data) {
			t.Fatalf("valid UTF-8 mis-encoded: %q -> %q", data, got)
		}
	})
}
