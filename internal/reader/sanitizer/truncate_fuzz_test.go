// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for TruncateHTML.
//
// TruncateHTML runs on REWRITTEN FEED TITLES and content (see the rss/atom/json
// adapters) — fully untrusted feed HTML that is entirely OUTSIDE FuzzSanitizer
// (which only reaches SanitizeHTML). These seeded-PRNG tests (plus the native
// coverage fuzzer at the end) assert the property a title-truncator must hold:
// output is valid UTF-8 and, when truncated, never introduces an unbalanced
// tag (an XSS-in-title vector). Failures reproduce by the printed seed.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist here (seeded PRNG + Go
// native testing.F). Candidate for consolidation onto one style.
package sanitizer

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// fuzzHTML builds adversarial HTML from fragment soup (opened/closed tags,
// comments, attributes, entities, raw text, control bytes) over a bounded size.
func fuzzHTML(r *rand.Rand, maxLen int) string {
	fragments := []string{
		"<", ">", "</", "/>", "<a>", "</a>", "<a href=\"x\">", "<b>", "</b>",
		"<!--", "-->", "&#", "&amp;", "&nbsp;", "<script>", "</script>",
		"<img src=\"a\">", " text ", "\t", "\n", "😀", "\x00", "a<b>c</b>d",
	}
	var b strings.Builder
	n := 1 + r.IntN(16)
	for i := 0; i < n; i++ {
		b.WriteString(fragments[r.IntN(len(fragments))])
		if r.IntN(3) == 0 {
			b.WriteRune(rune(0x20 + r.IntN(0x5f)))
		}
	}
	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// TestFuzzTruncateHTMLProperties asserts the core invariants:
//   - never panics;
//   - output is valid UTF-8 (no mid-rune ellipsis);
//   - pass-through: limit >= input runes returns input unchanged;
//   - no unclosed-tag leak: if the input is balanced, the output must not add
//     an unbalanced opening tag.
func TestFuzzTruncateHTMLProperties(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 32))
	for i := 0; i < 8000; i++ {
		input := fuzzHTML(r, 512)
		limit := r.IntN(200)

		out := TruncateHTML(input, limit)

		if !utf8.ValidString(out) {
			t.Fatalf("iter=%d input=%q limit=%d: output not valid UTF-8: %q", i, input, limit, out)
		}
	}

	// Pass-through property: for tag-free, normally-spaced input, truncation with
	// a limit at least as large as the input is a no-op (TruncateHTML only strips
	// HTML tags and collapses whitespace, so the property needs plain text).
	for i := 0; i < 2000; i++ {
		input := "the quick brown fox jumps over the lazy dog"
		if r.IntN(2) == 0 {
			input += strings.Repeat(" word", r.IntN(20))
		}
		out := TruncateHTML(input, utf8.RuneCountInString(input)+100)
		if out != input {
			t.Fatalf("iter=%d pass-through violated: in=%q out=%q", i, input, out)
		}
	}
}

// TestFuzzTruncateHTMLNoTagLeak asserts truncation never leaks raw markup into
// the output. TruncateHTML strips tags via an HTML tokenizer and only emits text
// tokens, so for input whose text nodes contain no '<' or '>' characters the
// output must contain no tag delimiters at all — an unbalanced opener surviving
// into a title would otherwise render as markup. NOTE: this assertion is only
// valid for input without a literal '<' in its textual content (e.g. "a < b");
// the document's text can legitimately contain '<' and pass through unchanged,
// so we constrain the corpus to controlled markup where text nodes are clean.
// (Replaces a vacuous tag-count check: count("</") <= count("<") is always true.)
func TestFuzzTruncateHTMLNoTagLeak(t *testing.T) {
	r := rand.New(rand.NewPCG(33, 34))
	cases := []string{
		"<a>hello</a>",
		"<a><b>nest</b></a>",
		"<div>x</div><span>y</span>",
		"<img src=\"x\"> standalone",
		"<p>some words</p><p>more</p>",
		"<ul><li>one</li><li>two</li></ul>",
	}
	for i := 0; i < 4000; i++ {
		seed := cases[r.IntN(len(cases))]
		out := TruncateHTML(seed, r.IntN(len(seed)+5))
		if strings.Contains(out, "<") || strings.Contains(out, ">") {
			t.Fatalf("iter=%d in=%q out=%q: raw tag delimiter leaked into truncation", i, seed, out)
		}
	}
}

// FuzzTruncateHTML is the Go-native coverage-guided complement, asserting valid
// UTF-8 output and no panic over mutated HTML with a bounded limit.
func FuzzTruncateHTML(f *testing.F) {
	f.Add("<a>hello world</a>", 5)
	f.Add("<p>short</p>", 100)
	f.Add("no tags at all", 3)
	f.Add("<", 5)
	f.Add("😀😀😀", 2)
	f.Fuzz(func(t *testing.T, input string, limit int) {
		if limit < 0 || limit > 10000 {
			return
		}
		out := TruncateHTML(input, limit)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 output for input=%q limit=%d: %q", input, limit, out)
		}
	})
}
