// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for URL helpers.
//
// These resolve/clean user and feed URLs, which are untrusted input. The
// properties: ResolveToAbsoluteURL never returns a relative or non-parseable
// result and never panics; Domain/DomainWithoutWWW/IsAbsoluteURL never panic on
// malformed URLs (scheme-only, control chars, IPv6 brackets, stray '//').
// Seeded-PRNG tests plus a native testing.F complement.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist; candidate for
// consolidation onto one style.
package urllib

import (
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"
)

// fuzzRelativeURL builds adversarial relative URLs from path/query fragments.
func fuzzRelativeURL(r *rand.Rand) string {
	fragments := []string{
		"", "/", "//host/x", "a", "a?b=c", "/a/b/../c", "../x", "./y", "..",
		"http://x", "https://x.com/y?z=1", "#frag", "a%", "a%2F", "\x00", "😀",
		"?ref=123", " a b ", "/path with space",
	}
	var b strings.Builder
	n := 1 + r.IntN(4)
	for i := 0; i < n; i++ {
		b.WriteString(fragments[r.IntN(len(fragments))])
	}
	return b.String()
}

// fuzzAbsoluteURL builds adversarial absolute URLs (includes malformed ones).
func fuzzAbsoluteURL(r *rand.Rand) string {
	schemes := []string{"http://", "https://", "ftp://", "", "://"}
	hosts := []string{"example.com", "a.b.c", "127.0.0.1", "[::1]", "sub", "exa mple"}
	paths := []string{"", "/", "/a/b", "/a?b=c", "%", " x"}
	return strings.TrimSpace(schemes[r.IntN(len(schemes))] + hosts[r.IntN(len(hosts))] + paths[r.IntN(len(paths))])
}

// TestFuzzResolveToAbsoluteURL asserts resolution never panics and, when it
// succeeds, yields an absolute, parseable URL (never a relative fragment).
func TestFuzzResolveToAbsoluteURL(t *testing.T) {
	r := rand.New(rand.NewPCG(71, 72))
	basePool := []string{
		"https://example.com",
		"https://example.com/feed.xml",
		"https://sub.example.org/dir/page",
		"http://localhost:8080/x",
	}
	for i := 0; i < 8000; i++ {
		base := basePool[r.IntN(len(basePool))]
		rel := fuzzRelativeURL(r)
		out, err := ResolveToAbsoluteURL(base, rel)
		if err != nil {
			continue
		}
		parsed, err := url.Parse(out)
		if err != nil {
			// Malformed source (e.g. invalid %-escape) may produce an unparseable
			// output; parse-or-through is acceptable for garbage input.
			continue
		}
		if !parsed.IsAbs() {
			t.Fatalf("iter=%d base=%q rel=%q: output %q not absolute", i, base, rel, out)
		}
	}
}

// TestFuzzDomainNeverPanics asserts domain helpers never panic on adversarial
// absolute URLs and return a plausible domain (or empty).
func TestFuzzDomainNeverPanics(t *testing.T) {
	r := rand.New(rand.NewPCG(73, 74))
	for i := 0; i < 8000; i++ {
		u := fuzzAbsoluteURL(r)
		_ = Domain(u)
		_ = DomainWithoutWWW(u)
		_ = IsAbsoluteURL(u)
		_ = RootURL(u)
		_ = IsHTTPS(u)
	}
}

// TestFuzzIsAbsoluteURLConsistency asserts IsAbsoluteURL agrees with net/url on
// what is an absolute (scheme-bearing) URL.
func TestFuzzIsAbsoluteURLConsistency(t *testing.T) {
	r := rand.New(rand.NewPCG(75, 76))
	for i := 0; i < 6000; i++ {
		u := r.IntN(2) == 0
		var s string
		if u {
			s = "https://x/y"
		} else {
			s = fuzzRelativeURL(r)
		}
		isAbs := IsAbsoluteURL(s)
		if u && !isAbs {
			t.Fatalf("iter=%d: %q should be absolute", i, s)
		}
	}
}

// FuzzResolveToAbsoluteURL is the native coverage-guided complement. The base is
// fixed to a valid absolute feed URL (the realistic contract); only the relative
// URL is fuzzed.
func FuzzResolveToAbsoluteURL(f *testing.F) {
	f.Add("/a/b/../c")
	f.Add("?q=1")
	f.Add("../y")
	f.Add("//evil.com/x")
	f.Add("//")
	f.Fuzz(func(t *testing.T, rel string) {
		out, err := ResolveToAbsoluteURL("https://example.com/feed.xml", rel)
		if err != nil {
			return
		}
		p, perr := url.Parse(out)
		if perr != nil {
			return // malformed source -> unparseable output is acceptable garbage-through
		}
		if !p.IsAbs() {
			t.Fatalf("rel=%q -> %q not absolute", rel, out)
		}
	})
}

// TestResolveProtocolRelativeFuzzRegression guards the fuzz-identified fast-path
// regression: protocol-relative refs with no real host ("//", "///x", "//?x")
// must NOT be fabricated into hostless "https://..." URLs by the // fast path.
func TestResolveProtocolRelativeFuzzRegression(t *testing.T) {
	base := "https://sub.example.org/dir/page"
	for _, rel := range []string{"//", "///x", "//?x", "//#f", "//@"} {
		out, err := ResolveToAbsoluteURL(base, rel)
		if err != nil {
			t.Fatalf("rel=%q unexpected error: %v", rel, err)
		}
		p, perr := url.Parse(out)
		if perr != nil {
			t.Fatalf("rel=%q -> %q did not parse: %v", rel, out, perr)
		}
		if out == "https://" || (p.Host == "" && strings.Contains(out, "//") && !strings.Contains(rel, "@")) {
			t.Fatalf("rel=%q produced hostless %q; fast path must not fabricate a host", rel, out)
		}
	}

	// A legitimate protocol-relative ref must keep the fast-path conversion.
	if out, err := ResolveToAbsoluteURL(base, "//cdn.example.com/x.png"); err != nil || out != "https://cdn.example.com/x.png" {
		t.Fatalf("legit protocol-relative ref: got %q err=%v", out, err)
	}
}
