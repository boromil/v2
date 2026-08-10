// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package sanitizer

import (
	"strings"
	"testing"
)

func TestStripContentBeforeFirstHeading(t *testing.T) {
	tests := []struct {
		name  string
		input string
		// want means these substrings must be present in the output.
		want []string
		// dontWant means these substrings must NOT be present in the output.
		dontWant []string
	}{
		{
			name:     "heading anchor removes preceding junk",
			input:    `<p>junk</p><h1>Title</h1><p>body</p>`,
			want:     []string{"<h1>Title</h1>", "<p>body</p>"},
			dontWant: []string{"junk"},
		},
		{
			name:     "hero class anchor keeps hero",
			input:    `<div>nav</div><div class="hero"><h2>Title</h2><p>lead</p></div>`,
			want:     []string{`class="hero"`, "<h2>Title</h2>", "<p>lead</p>"},
			dontWant: []string{"nav"},
		},
		{
			name:     "id anchor article-body",
			input:    `<nav>links</nav><div id="article-body"><p>content</p></div>`,
			want:     []string{`id="article-body"`, "<p>content</p>"},
			dontWant: []string{"links"},
		},
		{
			name:     "article tag anchor",
			input:    `<p>junk</p><article><p>story</p></article>`,
			want:     []string{"<article>", "<p>story</p>"},
			dontWant: []string{"junk"},
		},
		{
			name:     "header tag anchor",
			input:    `<div>garbage</div><header><h2>Title</h2></header>`,
			want:     []string{"<header>", "<h2>Title</h2>"},
			dontWant: []string{"garbage"},
		},
		{
			name:     "case-insensitive class and id matching",
			input:    `<p>junk</p><div class="Hero-Image"><h2>Title</h2></div><p>more</p><div id="ArticleBody"><h3>Next</h3></div>`,
			want:     []string{`class="Hero-Image"`, "<h2>Title</h2>", "<p>more</p>"},
			dontWant: []string{"junk"},
		},
		{
			name:     "precedence by document order hero before h1",
			input:    `<nav>nav</nav><div class="hero"><h2>A</h2></div><h1>B</h1>`,
			want:     []string{`class="hero"`, "<h2>A</h2>", "<h1>B</h1>"},
			dontWant: []string{"nav"},
		},
		{
			name:     "precedence by document order h1 before hero",
			input:    `<p>junk</p><h1>B</h1><div class="hero"><h2>A</h2></div>`,
			want:     []string{"<h1>B</h1>", `class="hero"`},
			dontWant: []string{"junk"},
		},
		{
			name:     "nested preamble inside leading wrapper divs",
			input:    `<div><p>nav</p></div><section><div><h1>Title</h1></div><p>body</p></section>`,
			want:     []string{"<h1>Title</h1>", "<p>body</p>", "<section>"},
			dontWant: []string{"nav"},
		},
		{
			name:     "self-contained tags inside preamble are removed",
			input:    `<img src="banner"><br><div>junk</div><h1>Title</h1><p>body</p>`,
			want:     []string{"<h1>Title</h1>", "<p>body</p>"},
			dontWant: []string{"banner", "junk"},
		},
		{
			name:     "empty input",
			input:    ``,
			want:     []string{},
			dontWant: []string{},
		},
		{
			name:     "intro paragraph before h3 is kept (h3 is not an anchor)",
			input:    `<p>Yesterday we had a hackathon to plan Servo.</p><p>This is the lead prose.</p><h3 id="the-big-picture">The big picture</h3><p>body</p>`,
			want:     []string{"Yesterday we had a hackathon to plan Servo.", "This is the lead prose.", "<h3 id=\"the-big-picture\">The big picture</h3>"},
			dontWant: []string{},
		},
		{
			name:     "content before h2 is stripped (h2 is an anchor)",
			input:    `<nav>menu</nav><h2 id="overview">Overview</h2><p>intro under h2</p>`,
			want:     []string{"<h2 id=\"overview\">Overview</h2>"},
			dontWant: []string{"menu"},
		},
		{
			name:     "main element is an anchor",
			input:    `<div class="site-header">Logo</div><main><h2>Title</h2><p>content</p></main>`,
			want:     []string{"<main>", "<h2>Title</h2>"},
			dontWant: []string{"Logo"},
		},
		{
			name:     "h4 is not an anchor (kept with preceding content)",
			input:    `<p>lead narrative text</p><h4 id="notes">Notes</h4><p>details</p>`,
			want:     []string{"lead narrative text", "<h4 id=\"notes\">Notes</h4>"},
			dontWant: []string{},
		},
		{
			name:     "article wrapper is an anchor and keeps its content",
			input:    `<ul id="global-nav"><li>Home</li></ul><article><header><h1>Post title</h1></header><p>intro</p></article>`,
			want:     []string{"<article>", "Post title", "intro"},
			dontWant: []string{"Home", "global-nav"},
		},
		{
			name: "site banner h1 (page title) is skipped for the article title",
			input: `<h1 class="blog-title">ENOSUCHBLOG</h1>` +
				`<h2 class="blog-subtitle"><em>Programming.</em></h2>` +
				`<ul class="navbar"><li>Home</li></ul><hr>` +
				`<h1 class="post-title">GitHub Actions needs OIDC audience constraints</h1>` +
				`<h2 class="post-subtitle">Aug 10, 2026</h2><hr>` +
				`<p>TL;DR</p><h2 id="cicd-and-oidc">CI/CD and OIDC</h2>`,
			want:     []string{"post-title", "GitHub Actions needs OIDC", "TL;DR", "cicd-and-oidc"},
			dontWant: []string{"ENOSUCHBLOG", "blog-subtitle", "navbar", "Home"},
		},
		{
			name:     "site banner skipped even when banner h1 has no content id",
			input:    `<h1 class="site-title">The Blarg</h1><p>tagline</p><h1 id="post-123">A Real Post</h1><p>body</p>`,
			want:     []string{"A Real Post", "<p>body</p>"},
			dontWant: []string{"The Blarg", "tagline"},
		},
		{
			name:     "banner heading without any later content heading falls back to itself",
			input:    `<h1 class="blog-title">Only a title</h1><p>just prose below</p>`,
			want:     []string{"blog-title", "just prose below"},
			dontWant: []string{},
		},
		{
			name:     "content heading via name attribute selected as fallback",
			input:    `<h1 class="logo">Big Site</h1><nav>menu</nav><h2 name="article-start">Section</h2><p>body</p>`,
			want:     []string{`name="article-start"`, "<p>body</p>"},
			dontWant: []string{"Big Site", "menu"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := StripContentBeforeFirstHeading(test.input)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("input=%q: output %q does not contain expected %q", test.input, got, want)
				}
			}
			for _, dontWant := range test.dontWant {
				if strings.Contains(got, dontWant) {
					t.Errorf("input=%q: output %q unexpectedly contains %q", test.input, got, dontWant)
				}
			}
		})
	}
}

func TestStripContentBeforeFirstHeadingNoAnchor(t *testing.T) {
	cases := []string{
		`<p>just some paragraphs</p><p><a href="/">link</a></p>`,
		`plain text only`,
		`<div><p>no qualifying headings here</p></div>`,
		// h3-h6 are NOT anchors; a document with only a sub-heading has no anchor
		// and must be returned unchanged.
		`<p>intro prose</p><h3 id="the-big-picture">The big picture</h3><p>body</p>`,
		`<p>this is a whole paragraph that must not be removed</p><h4>Note</h4>`,
	}
	for _, input := range cases {
		got := StripContentBeforeFirstHeading(input)
		if got != input {
			t.Errorf("input=%q: no anchor should return unchanged, got %q", input, got)
		}
	}
}

func TestStripContentBeforeFirstHeadingMalformed(t *testing.T) {
	// Malformed/unclosed tags must neither panic nor hang. Headings used as
	// anchors can be malformed themselves.
	cases := []string{
		`<h1>Title`,
		`<p>junk</p><h1`,
		`<div class="he`,
		`<div class="hero"><h2>Title</h2>`,
		`<h1><p>unclosed`,
		`<article><p>story`,
		`<main><h2>Title`,
		`<`,
		`<div "hero"`,
	}
	for _, input := range cases {
		// A panic/hang here fails the test.
		_ = StripContentBeforeFirstHeading(input)
	}
}

func TestStripContentBeforeFirstHeadingIdempotent(t *testing.T) {
	cases := []string{
		`<p>junk</p><h1>Title</h1><p>body</p>`,
		`<div>nav</div><div class="hero"><h2>Title</h2></div>`,
		`<nav>links</nav><div id="article-body"><p>content</p></div>`,
		`<p>junk</p><article><p>story</p></article>`,
		`<div><p>nav</p></div><section><div><h1>Title</h1></div><p>body</p></section>`,
	}
	for _, input := range cases {
		once := StripContentBeforeFirstHeading(input)
		twice := StripContentBeforeFirstHeading(once)
		if once != twice {
			t.Errorf("input=%q: not idempotent, once=%q twice=%q", input, once, twice)
		}
	}
}
