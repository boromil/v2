// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing // import "miniflux.app/v2/internal/storage/testing"

import (
	"testing"
	"time"

	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/model"
)

func dbTypeName(d dialect.DatabaseType) string {
	switch d {
	case dialect.SQLite:
		return "sqlite"
	default:
		return "postgres"
	}
}

func TestCountAllEntries(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 5)

			counts, err := s.CountAllEntries()
			if err != nil {
				t.Fatalf("CountAllEntries failed: %v", err)
			}
			if counts["unread"] != 5 {
				t.Errorf("expected 5 unread, got %d", counts["unread"])
			}
			if counts["total"] != 5 {
				t.Errorf("expected 5 total, got %d", counts["total"])
			}
		})
	}
}

func TestInsertEntryForFeed(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			if entry.ID == 0 {
				t.Error("expected non-zero entry ID")
			}
			if entry.Status != model.EntryStatusUnread {
				t.Errorf("expected status unread, got %q", entry.Status)
			}
			if entry.CreatedAt.IsZero() {
				t.Error("expected non-zero created_at")
			}

			alreadyExists := s.IsNewEntry(feed.ID, entry.Hash)
			if alreadyExists {
				t.Error("expected entry to not be new (should already exist)")
			}
		})
	}
}

func TestInsertEntryForFeedDuplicateHash(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry1 := &model.Entry{
				UserID:  user.ID,
				FeedID:  feed.ID,
				Hash:    "duplicate_hash_test",
				Title:   "Original Title",
				Content: "Original content",
				URL:     "https://example.com/entry1",
				Date:    time.Now(),
			}

			isNew, err := s.InsertEntryForFeed(user.ID, feed.ID, entry1)
			if err != nil {
				t.Fatalf("first insert failed: %v", err)
			}
			if !isNew {
				t.Error("expected first insert to be new")
			}

			entry2 := &model.Entry{
				UserID:  user.ID,
				FeedID:  feed.ID,
				Hash:    "duplicate_hash_test",
				Title:   "Different Title",
				Content: "Different content",
				URL:     "https://example.com/entry2",
				Date:    time.Now(),
			}

			isNew, err = s.InsertEntryForFeed(user.ID, feed.ID, entry2)
			if err != nil {
				t.Fatalf("second insert failed: %v", err)
			}
			if isNew {
				t.Error("expected second insert with same hash to not be new")
			}
		})
	}
}

func TestUpdateEntryTitleAndContent(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			entry.Title = "Updated Title"
			entry.Content = "Updated Content"
			entry.ReadingTime = 5

			if err := s.UpdateEntryTitleAndContent(entry); err != nil {
				t.Fatalf("UpdateEntryTitleAndContent failed: %v", err)
			}

			// Verify via query builder
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithEntryID(entry.ID)
			fetched, err := builder.GetEntry()
			if err != nil {
				t.Fatalf("GetEntry failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected entry to exist")
			}
			if fetched.Title != "Updated Title" {
				t.Errorf("expected title %q, got %q", "Updated Title", fetched.Title)
			}
			if fetched.ReadingTime != 5 {
				t.Errorf("expected reading_time 5, got %d", fetched.ReadingTime)
			}
		})
	}
}

func TestSetEntriesStatus(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entries := CreateTestEntries(t, s, user.ID, feed.ID, 3)

			ids := []int64{entries[0].ID, entries[1].ID}
			if err := s.SetEntriesStatus(user.ID, ids, model.EntryStatusRead); err != nil {
				t.Fatalf("SetEntriesStatus failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatus(model.EntryStatusRead)
			count, err := builder.CountEntries()
			if err != nil {
				t.Fatalf("CountEntries failed: %v", err)
			}
			if count != 2 {
				t.Errorf("expected 2 read entries, got %d", count)
			}
		})
	}
}

func TestSetEntriesStatusAndCountVisible(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entries := CreateTestEntries(t, s, user.ID, feed.ID, 5)

			ids := []int64{entries[0].ID, entries[3].ID}
			visible, err := s.SetEntriesStatusAndCountVisible(user.ID, ids, model.EntryStatusRead)
			if err != nil {
				t.Fatalf("SetEntriesStatusAndCountVisible failed: %v", err)
			}
			if visible != 2 {
				t.Errorf("expected 2 visible entries marked, got %d", visible)
			}
		})
	}
}

func TestSetEntriesStarredState(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			if err := s.SetEntriesStarredState(user.ID, []int64{entry.ID}, true); err != nil {
				t.Fatalf("SetEntriesStarredState failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStarred(true)
			starred, err := builder.CountEntries()
			if err != nil {
				t.Fatalf("CountEntries failed: %v", err)
			}
			if starred != 1 {
				t.Errorf("expected 1 starred entry, got %d", starred)
			}

			if err := s.SetEntriesStarredState(user.ID, []int64{entry.ID}, false); err != nil {
				t.Fatalf("unset starred failed: %v", err)
			}
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithStarred(false)
			notStarred, err := builder.CountEntries()
			if err != nil {
				t.Fatalf("CountEntries failed: %v", err)
			}
			if notStarred != 1 {
				t.Errorf("expected 1 non-starred entry, got %d", notStarred)
			}
		})
	}
}

func TestToggleStarred(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			if err := s.ToggleStarred(user.ID, entry.ID); err != nil {
				t.Fatalf("first ToggleStarred failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStarred(true)
			count, _ := builder.CountEntries()
			if count != 1 {
				t.Errorf("expected 1 starred after toggle ON, got %d", count)
			}

			if err := s.ToggleStarred(user.ID, entry.ID); err != nil {
				t.Fatalf("second ToggleStarred failed: %v", err)
			}

			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithStarred(false)
			count, _ = builder.CountEntries()
			if count != 1 {
				t.Errorf("expected 1 non-starred after toggle OFF, got %d", count)
			}
		})
	}
}

func TestMarkAllAsRead(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 5)

			if err := s.MarkAllAsRead(user.ID); err != nil {
				t.Fatalf("MarkAllAsRead failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatus(model.EntryStatusUnread)
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 unread, got %d", count)
			}
		})
	}
}

func TestMarkFeedAsRead(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 3)

			if err := s.MarkFeedAsRead(user.ID, feed.ID, time.Now()); err != nil {
				t.Fatalf("MarkFeedAsRead failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithFeedID(feed.ID)
			builder.WithStatus(model.EntryStatusUnread)
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 unread for feed, got %d", count)
			}
		})
	}
}

func TestMarkCategoryAsRead(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 3)

			if err := s.MarkCategoryAsRead(user.ID, category.ID, time.Now()); err != nil {
				t.Fatalf("MarkCategoryAsRead failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithCategoryID(category.ID)
			builder.WithStatus(model.EntryStatusUnread)
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 unread for category, got %d", count)
			}
		})
	}
}

func TestEntryShareCode(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			shareCode, err := s.EntryShareCode(user.ID, entry.ID)
			if err != nil {
				t.Fatalf("EntryShareCode failed: %v", err)
			}
			if shareCode == "" {
				t.Error("expected non-empty share code")
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithShareCode(shareCode)
			entryByCode, err := builder.GetEntry()
			if err != nil {
				t.Fatalf("GetEntry by share code failed: %v", err)
			}
			if entryByCode == nil {
				t.Fatal("expected to find entry by share code")
			}
		})
	}
}

func TestUnshareEntry(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entry := CreateTestEntry(t, s, user.ID, feed.ID)

			_, err := s.EntryShareCode(user.ID, entry.ID)
			if err != nil {
				t.Fatalf("EntryShareCode failed: %v", err)
			}

			if err := s.UnshareEntry(user.ID, entry.ID); err != nil {
				t.Fatalf("UnshareEntry failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithShareCodeNotEmpty()
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 entries with share code, got %d", count)
			}
		})
	}
}

func TestFlushHistory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			// Create some read entries
			entries := CreateTestEntries(t, s, user.ID, feed.ID, 3)
			if err := s.SetEntriesStatus(user.ID, []int64{entries[0].ID, entries[1].ID}, model.EntryStatusRead); err != nil {
				t.Fatalf("SetEntriesStatus failed: %v", err)
			}

			if err := s.FlushHistory(user.ID); err != nil {
				t.Fatalf("FlushHistory failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatus(model.EntryStatusRead)
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 read entries after flush, got %d", count)
			}
		})
	}
}

func TestEntryQueryBuilderBasicFilters(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entries := CreateTestEntries(t, s, user.ID, feed.ID, 10)

			// Filter by feed
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithFeedID(feed.ID)
			feedEntries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries by feed failed: %v", err)
			}
			if len(feedEntries) != 10 {
				t.Errorf("expected 10 entries for feed, got %d", len(feedEntries))
			}

			// Filter by category
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithCategoryID(category.ID)
			catEntries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries by category failed: %v", err)
			}
			if len(catEntries) < 10 {
				t.Errorf("expected at least 10 entries for category, got %d", len(catEntries))
			}

			// Filter by multiple entry IDs
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithEntryIDs([]int64{entries[0].ID, entries[2].ID, entries[5].ID})
			multiEntries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries by IDs failed: %v", err)
			}
			if len(multiEntries) != 3 {
				t.Errorf("expected 3 entries by IDs, got %d", len(multiEntries))
			}

			// Pagination with limit and offset
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithLimit(3)
			builder.WithOffset(2)
			pagedEntries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries with pagination failed: %v", err)
			}
			if len(pagedEntries) != 3 {
				t.Errorf("expected 3 entries with limit, got %d", len(pagedEntries))
			}
		})
	}
}

func TestEntryQueryBuilderStatusesFilter(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			entries := CreateTestEntries(t, s, user.ID, feed.ID, 5)

			if err := s.SetEntriesStatus(user.ID, []int64{entries[0].ID, entries[2].ID}, model.EntryStatusRead); err != nil {
				t.Fatalf("SetEntriesStatus failed: %v", err)
			}

			// Query with multiple statuses
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatuses([]string{model.EntryStatusUnread, model.EntryStatusRead})
			allEntries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries with statuses failed: %v", err)
			}
			if len(allEntries) != 5 {
				t.Errorf("expected 5 entries, got %d", len(allEntries))
			}
		})
	}
}

func TestGetReadTime(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := &model.Entry{
				UserID:      user.ID,
				FeedID:      feed.ID,
				Hash:        "readtime_test",
				Title:       "Read Time Test",
				Content:     "Content",
				ReadingTime: 8,
				URL:         "https://example.com/readtime",
				Date:        time.Now(),
			}
			_, err := s.InsertEntryForFeed(user.ID, feed.ID, entry)
			if err != nil {
				t.Fatalf("InsertEntryForFeed failed: %v", err)
			}

			readTime := s.GetReadTime(feed.ID, "readtime_test")
			if readTime != 8 {
				t.Errorf("expected read time 8, got %d", readTime)
			}
		})
	}
}

func TestMarkAllAsReadBeforeDate(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 5)

			futureMarkDate := time.Now().Add(24 * time.Hour)
			if err := s.MarkAllAsReadBeforeDate(user.ID, futureMarkDate); err != nil {
				t.Fatalf("MarkAllAsReadBeforeDate failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatus(model.EntryStatusUnread)
			count, _ := builder.CountEntries()
			if count != 0 {
				t.Errorf("expected 0 unread entries, got %d", count)
			}
		})
	}
}

func allDBTypes() []dialect.DatabaseType {
	return []dialect.DatabaseType{dialect.PostgreSQL, dialect.SQLite}
}

func TestMarkGloballyVisibleFeedsAsRead(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			visibleFeed := CreateTestFeed(t, s, user.ID, category.ID)
			hiddenFeed := &model.Feed{
				UserID:       user.ID,
				Category:     &model.Category{ID: category.ID},
				FeedURL:      "https://example.com/hidden-feed",
				HideGlobally: true,
			}
			if err := s.CreateFeed(hiddenFeed); err != nil {
				t.Fatalf("CreateFeed (hidden) failed: %v", err)
			}

			CreateTestEntries(t, s, user.ID, visibleFeed.ID, 3)
			CreateTestEntries(t, s, user.ID, hiddenFeed.ID, 2)

			if err := s.MarkGloballyVisibleFeedsAsRead(user.ID); err != nil {
				t.Fatalf("MarkGloballyVisibleFeedsAsRead failed: %v", err)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithStatus(model.EntryStatusUnread)
			count, _ := builder.CountEntries()
			if count != 2 {
				t.Errorf("expected 2 unread entries (from hidden feed), got %d", count)
			}
		})
	}
}

func TestArchiveEntries(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			CreateTestEntries(t, s, user.ID, feed.ID, 3)

			// Freshly created entries have created_at = NOW, so they won't
			// be older than the minimum archive interval of 1 day. This
			// test verifies the SQL path compiles and executes, and that
			// entries not meeting the age threshold are correctly skipped.
			count, err := s.ArchiveEntries(model.EntryStatusUnread, 24*time.Hour, 10)
			if err != nil {
				t.Fatalf("ArchiveEntries failed: %v", err)
			}
			if count != 0 {
				t.Errorf("expected 0 archived entries (too recent), got %d", count)
			}

			builder := s.NewEntryQueryBuilder(user.ID)
			allCount, _ := builder.CountEntries()
			if allCount != 3 {
				t.Errorf("expected all 3 entries to remain, got %d", allCount)
			}
		})
	}
}

func TestEntryQuerySortById(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 5)

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSorting("id", "DESC")
			builder.WithLimit(3)
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("GetEntries sorted by id failed: %v", err)
			}
			if len(entries) != 3 {
				t.Errorf("expected 3 entries, got %d", len(entries))
			}
			// Verify DESC order: first entry should have the highest ID
			if len(entries) >= 2 && entries[0].ID < entries[1].ID {
				t.Error("expected DESC order (highest id first)")
			}
		})
	}
}
