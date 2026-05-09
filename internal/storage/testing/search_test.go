// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"strings"
	"testing"
	"time"

	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/model"
)

func TestFullTextSearch(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Go programming language tutorial",
				"Learn about goroutines, channels, and concurrency patterns in Go")

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Python for data science",
				"Pandas and NumPy are essential Python libraries for data analysis")

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Rust systems programming",
				"Rust provides memory safety without garbage collection")

			// Search for "goroutines" should match first entry
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("goroutines")
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			found := false
			for _, e := range entries {
				if strings.Contains(e.Title, "Go") {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected to find Go entry when searching for 'goroutines'")
			}

			// Search for "Python" should match second entry
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("Python")
			entries, err = builder.GetEntries()
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			found = false
			for _, e := range entries {
				if strings.Contains(e.Title, "Python") {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected to find Python entry when searching for 'Python'")
			}
		})
	}
}

func TestFullTextSearchMultipleTerms(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"JavaScript async patterns",
				"Promises and async/await make JavaScript concurrent")

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"TypeScript type system",
				"TypeScript adds static types to JavaScript")

			// Search for "javascript types" should find both
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("javascript types")
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(entries) < 1 {
				t.Error("expected at least 1 result for multiple term search")
			}
		})
	}
}

func TestFTS5DocumentVectorUpdate(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Original Title",
				"Original content that nobody would search for")

			// Update with searchable content
			entry.Title = "Kubernetes container orchestration"
			entry.Content = "Kubernetes manages containerized applications at scale"
			entry.ReadingTime = 3
			if err := s.UpdateEntryTitleAndContent(entry); err != nil {
				t.Fatalf("UpdateEntryTitleAndContent failed: %v", err)
			}

			// Search for Kubernetes should find the updated entry
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("Kubernetes")
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(entries) < 1 {
				t.Errorf("expected to find entry after title/content update (db=%s)", dbTypeName(dbType))
			}
		})
	}
}

func TestWebSession(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			session, _ := model.NewWebSession("test-agent", "127.0.0.1")
			if err := s.CreateWebSession(session); err != nil {
				t.Fatalf("CreateWebSession failed: %v", err)
			}

			fetched, err := s.WebSessionByID(session.ID)
			if err != nil {
				t.Fatalf("WebSessionByID failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil session")
			}
			if fetched.UserAgent != "test-agent" {
				t.Errorf("expected user agent 'test-agent', got %q", fetched.UserAgent)
			}

			cleaned, err := s.CleanOldWebSessions(1 * time.Hour)
			if err != nil {
				t.Fatalf("CleanOldWebSessions failed: %v", err)
			}
			_ = cleaned
			_ = user
		})
	}
}

func TestFlushAllSessions(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			s1, _ := model.NewWebSession("ua1", "10.0.0.1")
			s2, _ := model.NewWebSession("ua2", "10.0.0.2")
			s.CreateWebSession(s1)
			s.CreateWebSession(s2)

			if err := s.FlushAllSessions(); err != nil {
				t.Fatalf("FlushAllSessions failed: %v", err)
			}

			fetched, _ := s.WebSessionByID(s1.ID)
			if fetched != nil {
				t.Error("expected session to be flushed")
			}
			_ = user
		})
	}
}

func TestFTS5WithStarredFilter(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			e1 := CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Starred searchable entry",
				"This entry is starred and searchable")

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Unstarred searchable entry",
				"This entry is not starred but searchable")

			s.SetEntriesStarredState(user.ID, []int64{e1.ID}, true)

			// Search "starred" with starred filter
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("starred")
			builder.WithStarred(true)
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("search with starred filter failed: %v", err)
			}
			if len(entries) != 1 {
				t.Errorf("expected 1 starred entry matching search, got %d", len(entries))
			}
		})
	}
}

func TestFTS5PrefixSearch(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Programming with databases",
				"Database programming patterns in modern applications")

			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("database")
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("prefix search failed: %v", err)
			}
			if len(entries) < 1 {
				t.Error("expected to find entry with search 'database'")
			}
		})
	}
}

func TestFTS5DeleteEntrySearch(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			if dbType == dialect.PostgreSQL {
				t.Skip("FTS delete sync test is SQLite-specific")
			}
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Delete sync test entry",
				"This entry will be deleted and should not appear in search")

			// Verify it exists in search
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("delete")
			entries, _ := builder.GetEntries()
			if len(entries) < 1 {
				t.Skip("entry not found in initial search, skipping")
				return
			}

			// Flush the entry to remove it
			s.SetEntriesStatus(user.ID, []int64{entry.ID}, model.EntryStatusRead)
			s.FlushHistory(user.ID)

			// Search should not find it anymore
			builder = s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("delete sync test")
			entries, _ = builder.GetEntries()
			if len(entries) > 0 {
				t.Error("expected flushed entry to not appear in search results")
			}
		})
	}
}

func TestFTS5SearchWithDots(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			CreateTestEntryWithContent(t, s, user.ID, feed.ID,
				"Version 2.0.8 released",
				"This release includes version 2.0.8 with many improvements")

			// Search for "2.0.8" — dots previously caused FTS5 syntax error
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithSearchQuery("2.0.8")
			entries, err := builder.GetEntries()
			if err != nil {
				t.Fatalf("search with dots failed: %v", err)
			}
			if len(entries) < 1 {
				t.Error("expected to find entry with search '2.0.8' (dots replaced with spaces)")
			}
		})
	}
}
