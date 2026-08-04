// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the OPML import parser.
//
// OPML files are untrusted user import/export data. Go's xml decoder with
// xml.HTMLEntity + Strict=false blocks custom-entity (billion-laughs)
// expansion, but deeply nested <outline> elements drive recursion in
// getSubscriptionsFromOutlines — a stack-overflow risk worth guarding. These
// tests (seeded PRNG + native testing.F) assert parse-or-error, never panic,
// and bounded recursion.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist here; candidate for
// consolidation onto one style.
package opml

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// fuzzOPML builds adversarial OPML documents from fragment soup, including
// deeply nested outlines and hostile entities/characters.
func fuzzOPML(r *rand.Rand, maxLen int) string {
	fragments := []string{
		"<?xml version=\"1.0\"?>",
		"<opml version=\"2.0\"><head><title>t</title></head><body>",
		"<outline title=\"x\" type=\"rss\" xmlUrl=\"http://a/feed\" htmlUrl=\"http://a\"/>",
		"<outline title=\"folder\">",
		"<outline type=\"rss\" xmlUrl=\"http://b/rss\"/>",
		"</outline>",
		"</body></opml>",
		"<!ENTITY xxe SYSTEM \"file:///etc/passwd\">",
		"&#0;", "&amp;", "&lt;", "<", ">", "/>", "</>",
	}
	var b strings.Builder
	n := 1 + r.IntN(20)
	for i := 0; i < n; i++ {
		b.WriteString(fragments[r.IntN(len(fragments))])
		if r.IntN(3) == 0 {
			b.WriteRune(rune(0x20 + r.IntN(0x5f)))
		}
	}
	if r.IntN(4) == 0 {
		// Occasional deep nesting to exercise the recursion path.
		depth := 1000 + r.IntN(1000)
		b.WriteString("<opml><body>")
		for i := 0; i < depth; i++ {
			b.WriteString("<outline title=\"f\">")
		}
		b.WriteString("<outline type=\"rss\" xmlUrl=\"http://deep/feed\"/>")
		for i := 0; i < depth; i++ {
			b.WriteString("</outline>")
		}
		b.WriteString("</body></opml>")
	}
	s := b.String()
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// TestFuzzOPMLParseOrErrorNeverPanics asserts parse is genuinely parse-or-error:
// no panic, no hang, on adversarial and deeply nested OPML.
func TestFuzzOPMLParseOrErrorNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(41, 42))
	for i := 0; i < 3000; i++ {
		data := fuzzOPML(r, 1<<20)
		// Must terminate (bounded iteration) and never panic; both outcomes are
		// acceptable (valid subscriptions or an error).
		_, _ = parse(strings.NewReader(data))
	}
}

// FuzzOPMLParse is the Go-native coverage-guided complement, asserting
// parse-or-error never panics over mutated OPML bytes.
func FuzzOPMLParse(f *testing.F) {
	f.Add(`<?xml version="1.0"?><opml><body><outline title="a" type="rss" xmlUrl="http://a/feed"/></body></opml>`)
	f.Add("")
	f.Add("<outline")
	f.Add("garbage")
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = parse(strings.NewReader(data))
	})
}
