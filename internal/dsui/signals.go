// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

// EntryRequest represents signals sent from client when requesting entries.
// Fields match Datastar signal names (camelCase) deserialized via ReadSignals.
type EntryRequest struct {
	View        string `json:"view"`
	FeedID      int64  `json:"feedId"`
	CategoryID  int64  `json:"categoryId"`
	SearchQuery string `json:"searchQuery"`
	Offset      int    `json:"offset"`
	EntryID     int64  `json:"entryId"`
}
