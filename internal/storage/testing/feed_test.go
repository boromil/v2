// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing // import "miniflux.app/v2/internal/storage/testing"

import (
	"fmt"
	"testing"
)

func TestCreateFeed(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			if feed.ID == 0 {
				t.Error("expected non-zero feed ID")
			}
			if feed.FeedURL == "" {
				t.Error("expected non-empty feed URL")
			}
		})
	}
}

func TestFeedExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			if !s.FeedExists(user.ID, feed.ID) {
				t.Error("expected feed to exist")
			}
			if s.FeedExists(user.ID, 99999) {
				t.Error("expected feed 99999 to not exist")
			}
		})
	}
}

func TestFeedURLExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://unique-feed.example.com/feed.xml")

			if !s.FeedURLExists(user.ID, "https://unique-feed.example.com/feed.xml") {
				t.Error("expected feed URL to exist")
			}
			if s.FeedURLExists(user.ID, "https://nonexistent.example.com/feed.xml") {
				t.Error("expected nonexistent feed URL to not exist")
			}
		})
	}
}

func TestFeedByID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			fetched, err := s.FeedByID(user.ID, feed.ID)
			if err != nil {
				t.Fatalf("FeedByID failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil feed")
			}
			if fetched.ID != feed.ID {
				t.Errorf("expected feed ID %d, got %d", feed.ID, fetched.ID)
			}

			noFeed, err := s.FeedByID(user.ID, 99999)
			if err != nil {
				t.Fatalf("FeedByID for nonexistent failed: %v", err)
			}
			if noFeed != nil {
				t.Error("expected nil for nonexistent feed")
			}
		})
	}
}

func TestFeeds(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, fmt.Sprintf("https://feed1_%s.example.com/feed.xml", t.Name()))
			CreateTestFeedWithURL(t, s, user.ID, category.ID, fmt.Sprintf("https://feed2_%s.example.com/feed.xml", t.Name()))

			feeds, err := s.Feeds(user.ID)
			if err != nil {
				t.Fatalf("Feeds failed: %v", err)
			}
			if len(feeds) < 2 {
				t.Errorf("expected at least 2 feeds, got %d", len(feeds))
			}
		})
	}
}

func TestFeedsWithCounters(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 3)

			feeds, err := s.FeedsWithCounters(user.ID)
			if err != nil {
				t.Fatalf("FeedsWithCounters failed: %v", err)
			}
			if len(feeds) != 1 {
				t.Errorf("expected 1 feed, got %d", len(feeds))
			}
			if feeds[0].UnreadCount != 3 {
				t.Errorf("expected 3 unread, got %d", feeds[0].UnreadCount)
			}
		})
	}
}

func TestUpdateFeed(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			feed.Title = "Updated Feed Title"
			feed.Description = "Updated description"
			if err := s.UpdateFeed(feed); err != nil {
				t.Fatalf("UpdateFeed failed: %v", err)
			}

			fetched, _ := s.FeedByID(user.ID, feed.ID)
			if fetched.Title != "Updated Feed Title" {
				t.Errorf("expected title 'Updated Feed Title', got %q", fetched.Title)
			}
		})
	}
}

func TestUpdateFeedError(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			feed.ParsingErrorCount = 1
			feed.ParsingErrorMsg = "Test error"
			if err := s.UpdateFeedError(feed); err != nil {
				t.Fatalf("UpdateFeedError failed: %v", err)
			}

			fetched, _ := s.FeedByID(user.ID, feed.ID)
			if fetched.ParsingErrorCount != 1 {
				t.Errorf("expected parsing error count 1, got %d", fetched.ParsingErrorCount)
			}
		})
	}
}

func TestRemoveFeed(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			if err := s.RemoveFeed(user.ID, feed.ID); err != nil {
				t.Fatalf("RemoveFeed failed: %v", err)
			}
			if s.FeedExists(user.ID, feed.ID) {
				t.Error("expected feed to be removed")
			}
		})
	}
}

func TestCountAllFeeds(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://feed2.example.com/feed.xml")

			counts, err := s.CountAllFeeds()
			if err != nil {
				t.Fatalf("CountAllFeeds failed: %v", err)
			}
			if counts["enabled"] < 2 {
				t.Errorf("expected at least 2 enabled, got %d", counts["enabled"])
			}
		})
	}
}

func TestFeedsByCategoryWithCounters(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 2)

			feeds, err := s.FeedsByCategoryWithCounters(user.ID, category.ID)
			if err != nil {
				t.Fatalf("FeedsByCategoryWithCounters failed: %v", err)
			}
			if len(feeds) < 1 {
				t.Errorf("expected at least 1 feed, got %d", len(feeds))
			}
		})
	}
}

func TestWeeklyFeedEntryCount(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 5)

			count, err := s.WeeklyFeedEntryCount(user.ID, feed.ID)
			if err != nil {
				t.Fatalf("WeeklyFeedEntryCount failed: %v", err)
			}
			if count < 0 {
				t.Error("weekly count should be non-negative")
			}
		})
	}
}

func TestFetchCounters(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestEntries(t, s, user.ID, feed.ID, 3)

			counters, err := s.FetchCounters(user.ID)
			if err != nil {
				t.Fatalf("FetchCounters failed: %v", err)
			}
			if len(counters.UnreadCounters) == 0 {
				t.Error("expected unread counters to not be empty")
			}
		})
	}
}

func TestCategoryFeedExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			if !s.CategoryFeedExists(user.ID, category.ID, feed.ID) {
				t.Error("expected category feed to exist")
			}
			otherCategory := CreateTestCategoryWithTitle(t, s, user.ID, "Other Category")
			if s.CategoryFeedExists(user.ID, otherCategory.ID, feed.ID) {
				t.Error("expected feed to not exist in wrong category")
			}
		})
	}
}

func TestResetFeedErrors(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			feed.ParsingErrorCount = 5
			feed.ParsingErrorMsg = "test error"
			if err := s.UpdateFeedError(feed); err != nil {
				t.Fatalf("UpdateFeedError failed: %v", err)
			}

			if err := s.ResetFeedErrors(); err != nil {
				t.Fatalf("ResetFeedErrors failed: %v", err)
			}

			fetched, _ := s.FeedByID(user.ID, feed.ID)
			if fetched.ParsingErrorCount != 0 {
				t.Errorf("expected 0 errors after reset, got %d", fetched.ParsingErrorCount)
			}
		})
	}
}

func TestCheckedAt(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			checkedAt, err := s.CheckedAt(user.ID, feed.ID)
			if err != nil {
				t.Fatalf("CheckedAt failed: %v", err)
			}
			if checkedAt.IsZero() {
				t.Error("expected non-zero checked_at")
			}
		})
	}
}

func TestAnotherFeedURLExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://conflict.example.com/feed.xml")

			if s.AnotherFeedURLExists(user.ID, feed.ID, "https://conflict.example.com/feed.xml") {
				t.Error("expected same feed to not conflict with itself")
			}
			feed2 := CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://other.example.com/feed.xml")
			if !s.AnotherFeedURLExists(user.ID, feed.ID, "https://other.example.com/feed.xml") {
				t.Error("expected different feed URL to conflict")
			}
			_ = feed2
		})
	}
}
