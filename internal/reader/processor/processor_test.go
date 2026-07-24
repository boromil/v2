// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package processor // import "miniflux.app/v2/internal/reader/processor"

import (
	"strings"
	"testing"

	"miniflux.app/v2/internal/model"
)

func TestShouldCrawlEntry(t *testing.T) {
	shortContent := strings.Repeat("a", MinContentLengthToAutoFetch-1)
	atThreshold := strings.Repeat("a", MinContentLengthToAutoFetch)
	longContent := strings.Repeat("a", MinContentLengthToAutoFetch+1)
	// HTML with lots of markup/attributes but little visible text (a typical
	// stub entry: metadata, tags, links — real text is well under the threshold).
	stubHTML := `<div class="entry-meta"><a href="http://example.org">Lobsters</a> – ` +
		`<span>martinfowler.com</span> via <em>kerollmops</em></div>` +
		`<p>Tags: <a href="/t/vibecoding">vibecoding</a></p>`
	// HTML whose visible text exceeds the threshold even though the markup is
	// compact; this must NOT be treated as short.
	fullHTML := "<p>" + strings.Repeat("a", MinContentLengthToAutoFetch) + "</p>"

	scenarios := []struct {
		name     string
		feed     *model.Feed
		entry    *model.Entry
		user     *model.User
		expected bool
	}{
		{
			name:     "feed crawler enabled ignores content length",
			feed:     &model.Feed{Crawler: true},
			entry:    &model.Entry{Content: longContent},
			user:     &model.User{},
			expected: true,
		},
		{
			name:     "feed crawler disabled, preference off",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: shortContent},
			user:     &model.User{AutoFetchShortEntries: false},
			expected: false,
		},
		{
			name:     "preference on, short content",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: shortContent},
			user:     &model.User{AutoFetchShortEntries: true},
			expected: true,
		},
		{
			name:     "preference on, content at threshold (not short)",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: atThreshold},
			user:     &model.User{AutoFetchShortEntries: true},
			expected: false,
		},
		{
			name:     "preference on, long content",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: longContent},
			user:     &model.User{AutoFetchShortEntries: true},
			expected: false,
		},
		{
			name:     "preference on, HTML stub with little visible text",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: stubHTML},
			user:     &model.User{AutoFetchShortEntries: true},
			expected: true,
		},
		{
			name:     "preference on, HTML whose visible text meets threshold",
			feed:     &model.Feed{Crawler: false},
			entry:    &model.Entry{Content: fullHTML},
			user:     &model.User{AutoFetchShortEntries: true},
			expected: false,
		},
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldCrawlEntry(tc.feed, tc.entry, tc.user)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}
