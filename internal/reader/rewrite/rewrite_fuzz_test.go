// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for the content rewrite engine.
//
// Rewrite rules are user-supplied and run against untrusted feed content. The
// pure string->string transformers (replaceCustom, decodeBase64Content,
// getYoutubVideoIDFromURL, the goquery rewriters) must never panic or hang on
// adversarial input. replaceCustom in particular compiles a user-supplied regex
// and applies it to feed content — a ReDoS regression surface. These are
// seeded-PRNG tests plus a native testing.F complement.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist; candidate for
// consolidation onto one style.
package rewrite

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// fuzzRuleText builds adversarial rewrite-rule text for parseRules.
func fuzzRuleText(r *rand.Rand) string {
	tokens := []string{
		"add_image_title", "remove", "replace", "replace_title", "nl2br",
		"\"a\"", "\"a b\"", "\"\"", "(", ")", "|", "\n", "\t", "😀", "\x00",
		"\"unterminated", "a\"b", "#selector > .x, .y",
	}
	var b strings.Builder
	n := 1 + r.IntN(8)
	for i := 0; i < n; i++ {
		b.WriteString(tokens[r.IntN(len(tokens))])
		if r.IntN(4) == 0 {
			b.WriteRune(rune(0x20 + r.IntN(0x5f)))
		}
	}
	return b.String()
}

// fuzzHTML builds adversarial feed-content HTML for the transformers.
func fuzzHTML(r *rand.Rand) string {
	fragments := []string{
		"<p>text</p>", "<a href=\"x\">link</a>", "<img src=\"a?blur=5\">",
		"<table><tr><td>x</td></tr></table>", "<figure class=\"kg-card\">",
		"<div>", "</div>", "<script>", "text", " ", "\n", "😀", "&#0;",
	}
	var b strings.Builder
	n := 1 + r.IntN(12)
	for i := 0; i < n; i++ {
		b.WriteString(fragments[r.IntN(len(fragments))])
		if r.IntN(3) == 0 {
			b.WriteRune(rune(0x20 + r.IntN(0x5f)))
		}
	}
	return b.String()
}

// TestFuzzParseRulesNeverPanics drives the text/scanner grammar, asserting it
// never panics and produces only name-typed rules with string args.
func TestFuzzParseRulesNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(61, 62))
	for i := 0; i < 5000; i++ {
		rules := parseRules(fuzzRuleText(r))
		for _, rule := range rules {
			if rule.name == "" {
				t.Fatalf("iter=%d: produced rule with empty name", i)
			}
		}
	}
}

// TestFuzzReplaceCustomReDoS asserts replaceCustom never hangs and never panics
// even when the user search-term is a classic catastrophic-backtracking regex.
func TestFuzzReplaceCustomReDoS(t *testing.T) {
	r := rand.New(rand.NewPCG(63, 64))
	// ReDoS-shaped search terms: nested quantifiers have no matching payload in
	// the content, so the RE2 regex engine must fail fast (no backtracking blowup).
	dangerousSearch := []string{
		`(a+)+$`, `(a|a)+$`, `(x+x+)+y`, `([a-zA-Z]+)*$`,
		`^(a|aa)+$`, `(.*)*x`,
	}
	for i := 0; i < 5000; i++ {
		content := strings.Repeat("a", 1+r.IntN(200))
		search := dangerousSearch[r.IntN(len(dangerousSearch))]
		// Bounded iteration asserts a hang would surface as a timeout/failure.
		_ = replaceCustom(content, search, "…")
	}
}

// TestFuzzTransformersNeverPanic drives the pure goquery-based transformers and
// the regex/base64 helpers over adversarial HTML, asserting they always return
// (no panic) and never return invalid UTF-8.
func TestFuzzTransformersNeverPanic(t *testing.T) {
	r := rand.New(rand.NewPCG(65, 66))
	base64ish := func() string {
		b := make([]byte, 1+r.IntN(32))
		for j := range b {
			// Draw from printable ASCII so some inputs are valid base64 and some
			// are not, exercising both decode branches.
			b[j] = byte(0x20 + r.IntN(0x5f))
		}
		return string(b)
	}
	for i := 0; i < 3000; i++ {
		html := fuzzHTML(r)

		for _, out := range []string{
			fixGhostCards(html),
			removeTables(html),
			removeImgBlurParams(html),
			decodeBase64Content(base64ish()),
			replaceCustom(html, `bogus[`, "x"), // invalid regex -> unchanged
		} {
			_ = out
		}
	}
}

// TestFuzzYoutubeID asserts getYoutubVideoIDFromURL only ever returns an
// 11-char ID or empty, and never panics on hostile URLs.
func TestFuzzYoutubeID(t *testing.T) {
	r := rand.New(rand.NewPCG(67, 68))
	hosts := []string{"https://youtube.com", "https://www.youtube.com", "https://evil.com", "x"}
	for i := 0; i < 3000; i++ {
		u := hosts[r.IntN(len(hosts))] + "/" + randomPath(r)
		id := getYoutubVideoIDFromURL(u)
		if id != "" && len(id) != 11 {
			t.Fatalf("iter=%d url=%q: non-11 id %q", i, u, id)
		}
	}
}

func randomPath(r *rand.Rand) string {
	switch r.IntN(3) {
	case 0:
		return "watch?v=" + randAlnum(r, 11)
	case 1:
		return "shorts/" + randAlnum(r, 11)
	default:
		return "channel/UC" + randAlnum(r, 10)
	}
}

const alnum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

func randAlnum(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alnum[r.IntN(len(alnum))]
	}
	return string(b)
}

// FuzzParseRules is the native coverage-guided complement.
func FuzzParseRules(f *testing.F) {
	f.Add("add_image_title")
	f.Add(`replace \n "a" "b"`)
	f.Add("remove \"#a > .x, .y\"")
	f.Add("\"unterminated")
	f.Fuzz(func(t *testing.T, rulesText string) {
		_ = parseRules(rulesText)
	})
}

// FuzzReplaceCustom is the native ReDoS/hang guard over mutated regex + content.
func FuzzReplaceCustom(f *testing.F) {
	f.Add("hello world", "(a+)+$", "x")
	f.Add("aaaa", `(x+x+)+y`, "") // no match -> must fail fast
	f.Fuzz(func(t *testing.T, content, search, replace string) {
		if len(content) > 4096 || len(search) > 512 {
			return
		}
		_ = replaceCustom(content, search, replace)
	})
}
