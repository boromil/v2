// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui

import (
	"log/slog"

	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/reader/processor"
)

// autoFetchShortEntryContent downloads the original web page for an entry whose
// content is shorter than the configured threshold, when the user has enabled
// the "auto_fetch_short_entries" preference. This makes short/stub entries
// fetch their full content on demand when opened, not only at feed refresh
// time. It mutates entry in place and persists the result. Errors are logged
// but not returned so that a failed fetch never blocks rendering the entry.
func (h *handler) autoFetchShortEntryContent(user *model.User, entry *model.Entry) {
	if entry == nil || user == nil {
		return
	}

	if !shouldAutoFetch(user, entry) {
		return
	}

	feed, err := h.store.NewFeedQueryBuilder(user.ID).
		WithFeedID(entry.FeedID).
		GetFeed()
	if err != nil || feed == nil {
		slog.Debug("Auto-fetch: unable to load feed for entry",
			slog.Int64("user_id", user.ID),
			slog.Int64("entry_id", entry.ID),
			slog.Int64("feed_id", entry.FeedID),
			slog.Any("error", err),
		)
		return
	}

	if err := processor.ProcessEntryWebPage(feed, entry, user); err != nil {
		slog.Debug("Auto-fetch: unable to fetch original content",
			slog.Int64("user_id", user.ID),
			slog.Int64("entry_id", entry.ID),
			slog.String("entry_url", entry.URL),
			slog.Any("error", err),
		)
		return
	}

	if err := h.store.UpdateEntryTitleAndContent(entry); err != nil {
		slog.Warn("Auto-fetch: unable to persist fetched content",
			slog.Int64("user_id", user.ID),
			slog.Int64("entry_id", entry.ID),
			slog.Any("error", err),
		)
	}
}

// shouldAutoFetch reports whether an entry should have its original content
// fetched on demand. It mirrors the threshold used in the feed processor and
// measures only visible text (after stripping HTML) so that markup and
// metadata do not inflate the length.
func shouldAutoFetch(user *model.User, entry *model.Entry) bool {
	return user.AutoFetchShortEntries &&
		processor.EntryContentTextLength(entry.Content) < processor.MinContentLengthToAutoFetch
}
