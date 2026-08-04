// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for feed format detection and parsing.
//
// These follow the project's seeded-PRNG fuzzing pattern (distinct from the
// coverage-guided FuzzParse target): feeds are untrusted network XML/JSON, so
// byte-soup feeding DetectFeedFormat/ParseFeed must always be "parse-or-error,
// never panic, never hang". A failure is reproducible by re-running with the
// printed seed.
//
// TODO(fuzzing-strategy): this file ALSO gains Go-native testing.F fuzzers
// below. Both styles coexist intentionally (breadth + determinism) and are a
// candidate for consolidation.
package parser

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// fuzzFeedBytes builds adversarial feed bytes from grammar fragments and raw
// noise, covering truncated XML, entity/DOCTYPE attempts, mismatched tags,
// JSON-ish shapes, and arbitrary binary. maxLen bounds allocation.
func fuzzFeedBytes(r *rand.Rand, maxLen int) []byte {
	fragments := []string{
		"<?xml version=\"1.0\"?>",
		"<!DOCTYPE rss [<!ENTITY xxe SYSTEM \"file:///etc/passwd\">]>",
		"<rss version=\"2.0\"><channel><item><title>",
		"<feed xmlns=\"http://www.w3.org/2005/Atom\"><entry><title>",
		"<rdf:RDF xmlns:rdf=\"http://www.w3.org/1999/02/22-rdf-syntax-ns#\">",
		"{\"version\":\"https://jsonfeed.org/version/1.1\",\"items\":[",
		"\"title\":\"x\",\"content_html\":",
		"</channel></rss>", "</feed>", "</rdf:RDF>", "]}",
		"<", ">", "/>", "</>", "&amp;", "&#0;", "<a><b><c>",
	}

	var b strings.Builder
	n := 1 + r.IntN(20)
	for i := 0; i < n; i++ {
		b.WriteString(fragments[r.IntN(len(fragments))])
		if r.IntN(3) == 0 {
			b.WriteRune(rune(1 + r.IntN(0x7f))) // arbitrary ASCII/control bytes
		}
	}

	// Randomly inject pure noise to add the non-fragment byte space.
	if r.IntN(3) == 0 {
		b.WriteString(randomNoise(r, r.IntN(64)))
	}

	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return []byte(s)
}

func randomNoise(r *rand.Rand, n int) string {
	const charset = "<>/\"'{}[]:;,=&!\n\t abcXYZ0129"
	b := make([]byte, n)
	for i := range b {
		b[i] = charset[r.IntN(len(charset))]
	}
	return string(b)
}

// TestFuzzDetectFeedFormatNeverPanics drives DetectFeedFormat over adversarial
// bytes and asserts it always returns (never panics/hangs) and yields a known
// format constant.
func TestFuzzDetectFeedFormatNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	for i := 0; i < 3000; i++ {
		data := fuzzFeedBytes(r, 4096)
		format, _ := DetectFeedFormat(strings.NewReader(string(data)))
		switch format {
		case FormatRDF, FormatRSS, FormatAtom, FormatJSON, FormatUnknown:
		default:
			t.Fatalf("iter=%d: DetectFeedFormat returned unknown format %q", i, format)
		}
	}
}

// TestFuzzParseFeedNeverPanics drives the full ParseFeed pipeline (detect +
// format-specific parse) over adversarial bytes and asserts parse-or-error
// (no panic, no hang). This exercises the rss/atom/rdf/json parsers against
// hostile XML/JSON.
func TestFuzzParseFeedNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(13, 14))
	for i := 0; i < 2000; i++ {
		data := fuzzFeedBytes(r, 8096)
		_, _ = ParseFeed("https://example.com", strings.NewReader(string(data)))
	}
}

// FuzzParseFeed_Native is a Go-native coverage-guided fuzzer over the full
// feed-format pipeline (detect + parse). It complements the seeded-PRNG
// ParseFeed test above with coverage-guided byte mutation. Run with:
//   go test -fuzz=FuzzParseFeed_Native -run=X ./internal/reader/parser
func FuzzParseFeed_Native(f *testing.F) {
	f.Add("https://example.com", []byte("<?xml version=\"1.0\"?><rss version=\"2.0\"><channel><item><title>t</title></item></channel></rss>"))
	f.Add("https://example.com", []byte("<feed xmlns=\"http://www.w3.org/2005/Atom\"><entry><title>x</title></entry></feed>"))
	f.Add("https://example.com", []byte("{\"version\":\"https://jsonfeed.org/version/1.1\",\"items\":[{}]}"))
	f.Fuzz(func(t *testing.T, baseURL string, data []byte) {
		_, _ = ParseFeed(baseURL, strings.NewReader(string(data)))
	})
}
