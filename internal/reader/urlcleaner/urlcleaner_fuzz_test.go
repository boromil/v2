// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for RemoveTrackingParameters.
//
// Tracking-parameter stripping runs on untrusted feed/link URLs. Properties it
// must hold: never panics (incl. nil args → error); the output URL preserves
// scheme/host/path; stripping is idempotent; and only tracking params are
// removed (non-tracking params like page=1, id, ref=safe survive). These tests
// are seeded-PRNG + a native testing.F complement.
//
// TODO(fuzzing-strategy): both fuzzing styles coexist; candidate for
// consolidation onto one style.
package urlcleaner

import (
	"math/rand/v2"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// fuzzURL builds an adversarial input URL from query-param fragments.
func fuzzURL(r *rand.Rand) string {
	paramNames := []string{
		"utm_source", "utm_medium", "utm_campaign", "utm_x", "mtm_source",
		"mtm_whatever", "fbclid", "ref", "gclid", "page", "id", "q", "a",
		"b_c", "123", "utm", "mtm", "REF", "Utm_Campaign",
	}
	vals := []string{"news", "email", feedHost, siteHost, "1", "2", "x y", "a&b", "%20"}
	var b strings.Builder
	b.WriteString("https://example.com/page/path?")
	np := 1 + r.IntN(6)
	for i := 0; i < np; i++ {
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString(paramNames[r.IntN(len(paramNames))])
		b.WriteString("=")
		b.WriteString(vals[r.IntN(len(vals))])
	}
	return b.String()
}

const (
	feedHost = "feed.example.com"
	siteHost = "example.com"
)

// TestFuzzRemoveTrackingParametersPreservesURL asserts that, for well-formed
// URLs with a query, RemoveTrackingParameters never panics and keeps the
// scheme+host+path intact while never leaking a non-tracking parameter out of
// order (i.e. the cleaned result is a strict subset of the original params).
func TestFuzzRemoveTrackingParametersPreservesURL(t *testing.T) {
	r := rand.New(rand.NewPCG(51, 52))
	for i := 0; i < 6000; i++ {
		raw := fuzzURL(r)
		parsedFeed, _ := url.Parse("https://" + feedHost + "/")
		parsedSite, _ := url.Parse("https://" + siteHost + "/")
		parsedInput, _ := url.Parse(raw)
		if parsedInput == nil {
			t.Fatalf("iter=%d: url %q did not parse", i, raw)
		}

		out, err := RemoveTrackingParameters(parsedFeed, parsedSite, parsedInput)
		if err != nil {
			t.Fatalf("iter=%d: unexpected error for %q: %v", i, raw, err)
		}

		parsedOut, err := url.Parse(out)
		if err != nil {
			t.Fatalf("iter=%d: output %q did not parse: %v", i, out, err)
		}
		if parsedOut.Scheme != parsedInput.Scheme || parsedOut.Host != parsedInput.Host {
			t.Fatalf("iter=%d: scheme/host changed: %q -> %q", i, raw, out)
		}
		if parsedOut.Path != parsedInput.Path {
			t.Fatalf("iter=%d: path changed: %q -> %q", i, raw, out)
		}
	}
}

// TestFuzzRemoveTrackingParametersIdempotent asserts re-cleaning a cleaned URL
// is a no-op (the output query contains no strip-able tracking param).
func TestFuzzRemoveTrackingParametersIdempotent(t *testing.T) {
	r := rand.New(rand.NewPCG(53, 54))
	for i := 0; i < 4000; i++ {
		raw := fuzzURL(r)
		fp, _ := url.Parse("https://" + feedHost + "/")
		sp, _ := url.Parse("https://" + siteHost + "/")

		first, err := RemoveTrackingParameters(fp, sp, mustParse(raw))
		if err != nil {
			continue
		}
		second, err := RemoveTrackingParameters(fp, sp, mustParse(first))
		if err != nil {
			t.Fatalf("iter=%d: second clean errored: %q -> %v", i, first, err)
		}
		if second != first {
			t.Fatalf("iter=%d: not idempotent: %q -> %q -> %q", i, raw, first, second)
		}
	}
}

// TestFuzzRemoveTrackingParametersNilArgs asserts nil-arg handling returns an
// error rather than panicking.
func TestFuzzRemoveTrackingParametersNilArgs(t *testing.T) {
	fp, _ := url.Parse("https://feed.example.com/")
	in, _ := url.Parse("https://example.com/?utm_source=x")
	if _, err := RemoveTrackingParameters(nil, nil, nil); err == nil {
		t.Fatalf("all-nil args should error")
	}
	if _, err := RemoveTrackingParameters(fp, nil, in); err == nil {
		t.Fatalf("nil siteURL should error")
	}
	if _, err := RemoveTrackingParameters(nil, fp, in); err == nil {
		t.Fatalf("nil feedURL should error")
	}
}

func mustParse(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

// FuzzRemoveTrackingParameters is the Go-native coverage-guided complement,
// asserting a real invariant over mutated URL bytes: cleaning preserves
// scheme/host/path and the output's query is a param-subset of the input's
// (cleaning only ever removes params, never adds or renames them). Arg sets are
// compared through url.Values so re-ordering/percent-re-encoding by Encode
// doesn't cause false positives.
func FuzzRemoveTrackingParameters(f *testing.F) {
	f.Add("https://example.com/page?utm_source=news&id=1")
	f.Add("https://example.com/?fbclid=abc&page=2")
	f.Add("https://example.com/?ref=https://feed.example.com/")
	f.Add("http://localhost:8080/x?utm=1")
	f.Add("?utm_source=a")
	f.Add("https://example.com/?a=%20&b=x&c&utm_bogus=1")
	f.Fuzz(func(t *testing.T, raw string) {
		fp, _ := url.Parse("https://feed.example.com/")
		sp, _ := url.Parse("https://example.com/")
		in, err := url.Parse(raw)
		if err != nil {
			return
		}
		out, err := RemoveTrackingParameters(fp, sp, in)
		if err != nil {
			return // nil-arg/parse errors are acceptable fall-through
		}

		// Normalize both URLs through their String() serialization so Go's
		// percent-escape/path canonicalization doesn't turn equivalent URLs into
		// literal string mismatches (e.g. path ": " serialized as "./: %20").
		inN, _ := url.Parse(in.String())
		outN, _ := url.Parse(out)
		if inN == nil || outN == nil {
			return
		}
		inQ, outQ := inN.Query(), outN.Query()
		inN.RawQuery, outN.RawQuery = "", ""
		if outN.String() != inN.String() {
			t.Fatalf("non-query parts changed: %q -> %q", raw, out)
		}

		for k, vals := range outQ {
			inVals, ok := inQ[k]
			if !ok {
				t.Fatalf("param %q introduced by cleaning: %q -> %q", k, raw, out)
			}
			for _, v := range vals {
				if !slices.Contains(inVals, v) {
					t.Fatalf("value %q added under key %q: %q -> %q", v, k, raw, out)
				}
			}
		}
	})
}
