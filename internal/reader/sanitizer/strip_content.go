// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package sanitizer

import (
	"bytes"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// anchorTagNames are element tag names that, by themselves, mark the top of a
// document's primary content. They follow the HTML5 content hierarchy:
//
//   - <main> and <article> are content containers (role main / self-contained
//     composition); content inside them is the real article.
//   - <header> is a section/article header and typically holds the title.
//   - <h1> and <h2> are top-level / major section headings. A page should have
//     a single <h1> (the title), with <h2> for major sections.
//
// <h3>-<h6> are intentionally NOT anchors: they are sub-section headings that
// legitimately appear AFTER lead/intro prose, so cutting on them would wrongly
// discard real content (e.g. a blog post's intro paragraph).
var anchorTagNames = map[string]bool{
	"h1": true, "h2": true,
	"main": true, "article": true, "header": true,
}

// anchorAttributeKeywords are substrings that mark an element as an anchor when
// found (case-insensitively) in its class or id attribute value. Values are
// first normalized (lowercased, with all non-alphanumeric characters stripped)
// before matching, so "ArticleBody", "article-body" and "article body" all
// detect the "article-body" keyword.
var anchorAttributeKeywords = []string{"title", "hero", "headline", "articlebody"}

// bannerHeadingKeywords are substrings that, when found (case-insensitively)
// in the class/id of an h1/h2 element, mark that heading as site chrome (the
// "title of the whole page" / blog name) rather than the start of the actual
// article content.
//
// Many blogs render their site name as a top-of-page <h1> (blog-title,
// site-title, masthead, …). The generic "title" keyword would select that as
// the first anchor, which strips nothing (it sits at the very top of <body>)
// and leaves all the header/nav chrome in the entry. These keywords let us
// recognize such banner headings so the fallback below can skip past them.
var bannerHeadingKeywords = []string{
	"site", "blog", "logo", "brand", "masthead", "banner", "navbar", "menu",
}

// contentHeadingKeywords are substrings that mark an h1/h2 element as the
// article's real title/start (case-insensitive). Used only by the fallback
// path: when the primary detector lands on a banner heading, we keep scanning
// for the first later heading that actually signals content so the strip cuts
// at the article rather than the site title.
var contentHeadingKeywords = []string{
	"post", "article", "entry", "story", "content", "main", "page", "title",
}

// StripContentBeforeFirstHeading removes everything strictly before the first
// "anchor" element in the document, keeping the anchor and all content after it.
//
// An element qualifies as an anchor if any of the following holds:
//   - its tag name is one of h1, h2, main, article or header; or
//   - its class or id attribute value (case-insensitive, non-alphanumeric
//     characters ignored) contains "title", "hero", "headline" or
//     "article-body".
//
// <h3>-<h6> are NOT anchors: they are sub-section headings that may legitimately
// follow lead/intro prose. Cutting on the first <h3> would discard real content
// (e.g. an article intro paragraph above it).
//
// When the first anchor is an h1/h2 that only carries the site's banner title
// (blog-title, site-title, logo, …) rather than starting the article content,
// the strip would otherwise cut at the very top of <body> and leave all the
// header/nav chrome in place. In that case we fall back to the first later
// h1/h2 whose id/name/class signals real article content, so the banner is
// dropped and the entry starts at the actual article title.
//
// When no anchor is found, the input is returned unchanged. The function
// operates on RAW HTML and never panics or hangs, even on malformed input.
func StripContentBeforeFirstHeading(rawHTML string) string {
	// The strip is applied iteratively until the result reaches a fixed point.
	//
	// Why: the first anchor element may itself contain another anchor (invalid
	// HTML such as a nested <h1>) or the preamble may contain additional anchors.
	// Re-parsing the rendered output normalizes such invalid nesting (the HTML5
	// parser auto-closes nested headings, promoting inner ones to siblings), so a
	// single pass is not guaranteed to return a value that a second pass leaves
	// unchanged. Iterating guarantees the function is idempotent: f(f(x)) == f(x).
	//
	// Each pass only removes leading nodes, so the output is monotonically
	// shrinking and necessarily converges; the bound is a defensive guard against
	// any unforeseen oscillation.
	const maxPasses = 100

	current := rawHTML
	for pass := 0; pass < maxPasses; pass++ {
		next := stripContentBeforeFirstHeadingSingle(current)
		if next == current {
			return current
		}
		current = next
	}
	return current
}

// stripContentBeforeFirstHeadingSingle performs one strip pass. See
// StripContentBeforeFirstHeading.
func stripContentBeforeFirstHeadingSingle(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return rawHTML
	}

	anchor := findFirstAnchor(doc)
	if anchor == nil {
		return rawHTML
	}

	// Walk from the anchor up to the document, removing, at every level of the
	// chain, all elements that precede each node. This strips the leading
	// preamble/chrome while keeping the anchor's ancestor chain and everything
	// from the anchor onward.
	for n := anchor; ; n = n.Parent {
		removePrecedingSiblings(n)
		if n.Parent == nil || n.Parent.Parent == nil {
			break
		}
	}

	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return rawHTML
	}
	return out.String()
}

// findFirstAnchor returns the first anchor element in document (pre-order)
// order, or nil if none exists.
//
// The primary pass locates the first element matching the tag-based or
// class/id-keyword rules. When that first anchor is an h1/h2 that merely
// carries the site's banner title (blog-title, site-title, …) — i.e. it does
// not actually mark the start of article content — the result does not "yield
// results" (cutting there leaves all header/nav chrome in place), so we fall
// back to scanning the document for the first later h1/h2 whose id/name/class
// clearly signals real content and use that as the cut anchor instead.
func findFirstAnchor(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	first := findFirstAnchorByOrder(n)
	if first == nil {
		return nil
	}
	if !isBannerHeading(first) {
		return first
	}
	if fallback := findFallbackContentHeading(n, first); fallback != nil {
		return fallback
	}
	return first
}

// findFirstAnchorByOrder returns the first anchor element in document
// (pre-order) order, or nil if none exists.
func findFirstAnchorByOrder(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	if isAnchorElement(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if anchor := findFirstAnchorByOrder(child); anchor != nil {
			return anchor
		}
	}
	return nil
}

// isBannerHeading reports whether an anchor element is an h1/h2 that only
// carries the site's headline title (blog-title, site-title, logo, …) rather
// than starting the article content. Dropping such heads lets the fallback skip
// past the site banner and cut at the real content heading.
func isBannerHeading(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	tag := strings.ToLower(n.Data)
	if tag != "h1" && tag != "h2" {
		return false
	}
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		if key != "class" && key != "id" {
			continue
		}
		value := normalizeAttributeValue(attr.Val)
		if value == "" {
			continue
		}
		for _, kw := range bannerHeadingKeywords {
			if strings.Contains(value, kw) {
				return true
			}
		}
	}
	return false
}

// findFallbackContentHeading finds the first h1/h2 that appears strictly after
// the given banner heading and signals real article content via a
// content keyword in its id/name/class, or nil if there is none. Restricted to
// h1/h2 on purpose: like the primary rules, h3-h6 are internal sub-section
// headings that may legitimately follow intro prose, so they must never become
// the cut anchor.
func findFallbackContentHeading(n, after *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(m *html.Node) {
		if found != nil {
			return
		}
		if m != nil && m.Type == html.ElementNode {
			tag := strings.ToLower(m.Data)
			if tag == "h1" || tag == "h2" {
				if m != after && hasContentHeadingSignal(m) {
					found = m
					return
				}
			}
		}
		for child := m.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

// hasContentHeadingSignal reports whether an h1/h2 carries an id, name or class
// attribute whose normalized value indicates article content.
func hasContentHeadingSignal(n *html.Node) bool {
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		if key != "class" && key != "id" && key != "name" {
			continue
		}
		value := normalizeAttributeValue(attr.Val)
		if value == "" {
			continue
		}
		for _, kw := range contentHeadingKeywords {
			if strings.Contains(value, kw) {
				return true
			}
		}
	}
	return false
}

// isAnchorElement reports whether a node is an anchor element.
func isAnchorElement(n *html.Node) bool {
	if n.Type == html.ElementNode && anchorTagNames[strings.ToLower(n.Data)] {
		return true
	}
	for _, attr := range n.Attr {
		key := strings.ToLower(attr.Key)
		if key != "class" && key != "id" {
			continue
		}
		value := normalizeAttributeValue(attr.Val)
		if value == "" {
			continue
		}
		for _, kw := range anchorAttributeKeywords {
			if strings.Contains(value, kw) {
				return true
			}
		}
	}
	return false
}

// normalizeAttributeValue lowercases a class/id value and drops all
// non-alphanumeric characters so that comparisons are robust to casing,
// hyphens, spaces and punctuation.
func normalizeAttributeValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// removePrecedingSiblings detaches every sibling that precedes n from n's
// parent, leaving n as the first child.
func removePrecedingSiblings(n *html.Node) {
	parent := n.Parent
	if parent == nil {
		return
	}
	for c := parent.FirstChild; c != n; {
		next := c.NextSibling
		parent.RemoveChild(c)
		c = next
	}
}
