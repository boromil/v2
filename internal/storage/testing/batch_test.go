// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"testing"
)

func TestBatchBuilderBasic(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://example.com/feed2_"+t.Name()+".xml")

			builder := s.NewBatchBuilder()
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 2 {
				t.Errorf("expected 2 jobs, got %d", len(jobs))
			}
		})
	}
}

func TestBatchBuilderWithUserID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user1 := CreateTestUser(t, s)
			user2 := CreateTestUserWithFields(t, s, "second_user_batch", false)
			category1 := CreateTestCategory(t, s, user1.ID)
			category2 := CreateTestCategory(t, s, user2.ID)
			CreateTestFeed(t, s, user1.ID, category1.ID)
			CreateTestFeed(t, s, user2.ID, category2.ID)

			builder := s.NewBatchBuilder()
			builder.WithUserID(user1.ID)
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 1 {
				t.Errorf("expected 1 job for user1, got %d", len(jobs))
			}
			if jobs[0].UserID != user1.ID {
				t.Errorf("expected user ID %d, got %d", user1.ID, jobs[0].UserID)
			}
		})
	}
}

func TestBatchBuilderWithCategoryID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category1 := CreateTestCategoryWithTitle(t, s, user.ID, "cat_one_batch")
			category2 := CreateTestCategoryWithTitle(t, s, user.ID, "cat_two_batch")
			CreateTestFeed(t, s, user.ID, category1.ID)
			CreateTestFeedWithURL(t, s, user.ID, category2.ID, "https://cat2feed.example.com/feed.xml")

			builder := s.NewBatchBuilder()
			builder.WithCategoryID(category1.ID)
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 1 {
				t.Errorf("expected 1 job for category1, got %d", len(jobs))
			}
		})
	}
}

func TestBatchBuilderWithoutDisabledFeeds(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed1 := CreateTestFeed(t, s, user.ID, category.ID)
			feed2 := CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://disabled-feed.example.com/feed.xml")

			feed2.Disabled = true
			if err := s.UpdateFeed(feed2); err != nil {
				t.Fatalf("UpdateFeed (disable) failed: %v", err)
			}

			builder := s.NewBatchBuilder()
			builder.WithoutDisabledFeeds()
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 1 {
				t.Errorf("expected 1 enabled job, got %d", len(jobs))
			}
			if jobs[0].FeedID != feed1.ID {
				t.Errorf("expected feed1 (enabled), got feed %d", jobs[0].FeedID)
			}
		})
	}
}

func TestBatchBuilderWithBatchSize(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://f1.example.com/feed.xml")
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://f2.example.com/feed.xml")
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://f3.example.com/feed.xml")

			builder := s.NewBatchBuilder()
			builder.WithBatchSize(2)
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 2 {
				t.Errorf("expected 2 jobs (batch size 2), got %d", len(jobs))
			}
		})
	}
}

func TestBatchBuilderWithNextCheckExpired(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeed(t, s, user.ID, category.ID)

			builder := s.NewBatchBuilder()
			builder.WithNextCheckExpired()
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			_ = jobs
		})
	}
}

func TestBatchBuilderWithErrorLimit(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)
			CreateTestFeedWithURL(t, s, user.ID, category.ID, "https://errored-feed.example.com/feed.xml")

			feed.ParsingErrorCount = 5
			feed.ParsingErrorMsg = "too many errors"
			if err := s.UpdateFeedError(feed); err != nil {
				t.Fatalf("UpdateFeedError failed: %v", err)
			}

			builder := s.NewBatchBuilder()
			builder.WithErrorLimit(3)
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) != 1 {
				t.Errorf("expected 1 job below error limit, got %d", len(jobs))
			}
		})
	}
}

func TestBatchBuilderCombinedFilters(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeed(t, s, user.ID, category.ID)

			builder := s.NewBatchBuilder()
			builder.WithUserID(user.ID)
			builder.WithoutDisabledFeeds()
			jobs, err := builder.FetchJobs()
			if err != nil {
				t.Fatalf("FetchJobs failed: %v", err)
			}
			if len(jobs) < 1 {
				t.Error("expected at least 1 job with combined filters")
			}
		})
	}
}
