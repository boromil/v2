// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Generative / negative-space tests for StripContentBeforeFirstHeading.
//
// StripContentBeforeFirstHeading runs on RAW, untrusted feed HTML (possibly
// malformed) so it must never panic, never hang, and be stable under repeated
// application. The native coverage fuzzer below drives the property asserts:
//
//   - no panic on arbitrary byte input;
//   - idempotency: applying twice equals applying once;
//   - anchor-preservation: if the input has a first anchor (h1/h2/main/article/
//     header, or a title/hero/article-body class-id), the output's first anchor
//     (same detection logic) has an identical signature, and the output begins
//     at that anchor (nothing precedes it); if the input has no anchor (e.g. a
//     document with only h3-h6 sub-headings or plain prose), the output equals
//     the input.
package sanitizer

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// firstAnchorSignature identifies an anchor element by tag name plus its
// normalized class/id attribute values. Two anchors are considered the "same"
// when their signatures match.
func firstAnchorSignature(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return ""
	}
	return nodeSignature(findFirstAnchor(doc))
}

func nodeSignature(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(n.Data))
	b.WriteByte('/')
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		if key == "class" || key == "id" {
			b.WriteString(normalizeAttributeValue(attr.Val))
			b.WriteByte(';')
		}
	}
	return b.String()
}

// FuzzStripContentBeforeFirstHeading asserts the core invariants over arbitrary
// byte input.
func FuzzStripContentBeforeFirstHeading(f *testing.F) {
	seeds := []string{
		// Heading doc (h2 is an anchor).
		`<p>junk</p><h2>Title</h2><p>body</p>`,
		`<html><body><div>nav</div><h2>Section</h2><p>text</p></body></html>`,
		`<h1>Just a heading</h1>`,
		// Hero-class doc.
		`<div>nav</div><div class="hero"><h2>Title</h2><p>lead</p></div>`,
		`<header class="SiteHero"><h1>Title</h1></header>`,
		// id signature.
		`<nav>links</nav><div id="article-body"><p>content</p></div>`,
		`<div id="ArticleBody"><h1>Title</h1></div>`,
		// article / header / main tags.
		`<p>junk</p><article><p>story</p></article>`,
		`<div>x</div><header><h2>Title</h2></header>`,
		`<div class="site-header">Logo</div><main><h2>Title</h2><p>body</p></main>`,
		// Banner (site-title) heading followed by the real article heading.
		`<h1 class="blog-title">Site</h1><p>tag</p><nav>menu</nav><h1 class="post-title">Real post</h1><p>body</p>`,
		`<h1 class="site-title">Blog</h1><h2 id="post-1">Post</h2><p>body</p>`,
		`<h1 class="logo">Only a title</h1><p>just prose below</p>`,
		// h3-h6 are NOT anchors: preceding prose must be preserved.
		`<p>intro prose</p><h3>The big picture</h3><p>body</p>`,
		`<p>lead text</p><h4>Notes</h4><p>details</p>`,
		`<p>a whole paragraph that must be kept</p><h3 id="the-big-picture">The big picture</h3>`,
		// Malformed HTML.
		`<h2>Title`,
		`<p>junk</p><div class="hero"><h2`,
		`<`,
		`<div class="he`,
		`plain text with no anchors`,
		`<p>no anchors here</p>`,
		``,
		`<div class="Hero-Image"><h3>Title</h3></div>`,
		`<img src="b"><br><div>junk</div><h2>Title</h2>`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, rawHTML string) {
		// Work from an immutable snapshot. The function takes/returns strings,
		// but snapshotting guards against any future in-place mutation.
		input := rawHTML

		out := StripContentBeforeFirstHeading(input)
		// Applying the function must never panic (a panic fails the test).

		// Idempotency.
		if again := StripContentBeforeFirstHeading(out); again != out {
			t.Fatalf("not idempotent: input=%q out=%q again=%q", input, out, again)
		}

		inAnchor := firstAnchorSignature(input)
		if inAnchor == "" {
			// No anchor in the input: output must be unchanged.
			if out != input {
				t.Fatalf("no anchor but output changed: input=%q out=%q", input, out)
			}
			return
		}

		// Anchor preservation: output's first anchor must match the input's.
		outAnchor := firstAnchorSignature(out)
		if outAnchor == "" {
			t.Fatalf("input had anchor %q but output has none: input=%q out=%q", inAnchor, input, out)
		}
		if outAnchor != inAnchor {
			t.Fatalf("first anchor changed: input anchor=%q output anchor=%q\ninput=%q\nout=%q", inAnchor, outAnchor, input, out)
		}

		// The output must begin at (or before) the input's first anchor, i.e.
		// there must be no content preceding the output's first anchor.
		if contentPrecedesFirstAnchor(out) {
			t.Fatalf("content precedes first anchor in output: input=%q out=%q", input, out)
		}
	})
}

// contentPrecedesFirstAnchor reports whether any real content precedes the
// first anchor in the given HTML document. After a successful strip the first
// anchor is the very first element under <body>: it has no preceding sibling
// content along its ancestor chain (the structural <html>/<head>/<body>
// wrappers are ignored, and whitespace text is not counted).
func contentPrecedesFirstAnchor(htmlStr string) bool {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return false
	}
	a := findFirstAnchor(doc)
	if a == nil {
		return false
	}
	for n := a; ; n = n.Parent {
		if p := n.Parent; p != nil {
			for sib := p.FirstChild; sib != nil && sib != n; sib = sib.NextSibling {
				if isStructuralWrapper(sib) {
					continue
				}
				if sib.Type == html.TextNode && strings.TrimSpace(sib.Data) == "" {
					continue
				}
				// Any real element or non-whitespace text before the anchor.
				return true
			}
		}
		if n.Parent == nil || isStructuralWrapper(n) {
			break
		}
	}
	return false
}

func isStructuralWrapper(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	switch strings.ToLower(n.Data) {
	case "html", "head", "body":
		return true
	}
	return false
}
