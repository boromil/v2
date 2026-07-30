// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

// AppSignals represents the Datastar signals for the reader app.
// These are serialized as JSON and seeded into the page via data-signals.
type AppSignals struct {
	View        string `json:"view"`        // "unread", "starred", "history", "feed-{id}", "category-{id}"
	EntryID     int64  `json:"entryId"`     // Currently selected/displayed entry ID
	FeedID      int64  `json:"feedId"`      // Currently selected feed ID (0 = none)
	CategoryID  int64  `json:"categoryId"`  // Currently selected category ID (0 = none)
	Offset      int    `json:"offset"`      // Pagination offset
	SelectedIdx int    `json:"selectedIdx"` // Index of selected entry in list (for keyboard nav)
	Loading     bool   `json:"loading"`     // Loading indicator
}

// EntryRequest represents signals sent from client when requesting entries.
type EntryRequest struct {
	View       string `json:"view"`
	FeedID     int64  `json:"feedId"`
	CategoryID int64  `json:"categoryId"`
	SearchQuery string `json:"searchQuery"`
	Offset     int    `json:"offset"`
	EntryID    int64  `json:"entryId"`
}

// MarkReadRequest represents signals sent when marking entries as read.
type MarkReadRequest struct {
	EntryIDs []int64 `json:"entryIds"`
	FeedID   int64   `json:"feedId"`
}
