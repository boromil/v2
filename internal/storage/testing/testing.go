// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing // import "miniflux.app/v2/internal/storage/testing"

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database"
	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
)

// SetupTestDB creates an isolated test database, runs migrations, and returns a Storage.
// For SQLite, a temporary file is used (auto-cleaned).
// For PostgreSQL, the TEST_DATABASE_URL env var is required; the test is skipped if unset.
func SetupTestDB(t *testing.T, dbType dialect.DatabaseType) *storage.Storage {
	t.Helper()

	var dsn string
	var d dialect.Dialect

	switch dbType {
	case dialect.SQLite:
		d = &dialect.SQLiteDialect{}
		dsn = filepath.Join(t.TempDir(), "test.db")
	case dialect.PostgreSQL:
		d = &dialect.PostgreSQLDialect{}
		dsn = os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			t.Skip("TEST_DATABASE_URL not set, skipping PostgreSQL integration test")
		}
	}

	if config.Opts == nil {
		var err error
		config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
		if err != nil {
			t.Fatalf("failed to initialize config defaults: %v", err)
		}
	}

	db, err := database.NewConnectionPool(d, dsn, 1, 1, time.Hour)
	if err != nil {
		t.Fatalf("failed to create connection pool: %v", err)
	}

	provider := migrationProviderForDialect(d)
	if err := database.Migrate(db, provider); err != nil {
		db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}

	store := storage.NewStorage(db, d)

	t.Cleanup(func() {
		db.Close()
	})

	return store
}

func migrationProviderForDialect(d dialect.Dialect) database.MigrationProvider {
	switch d.DatabaseType() {
	case dialect.SQLite:
		return database.NewSQLiteMigrationProvider()
	default:
		return database.NewPostgreSQLMigrationProvider()
	}
}

// CreateTestUser creates a user with the given username and returns it.
func CreateTestUser(t *testing.T, s *storage.Storage) *model.User {
	t.Helper()
	return CreateTestUserWithFields(t, s, fmt.Sprintf("testuser_%x", md5.Sum([]byte(t.Name()))), false)
}

// CreateTestAdminUser creates an admin user and returns it.
func CreateTestAdminUser(t *testing.T, s *storage.Storage) *model.User {
	t.Helper()
	return CreateTestUserWithFields(t, s, fmt.Sprintf("testadmin_%x", md5.Sum([]byte(t.Name()))), true)
}

// CreateTestUserWithFields creates a user with specific settings and returns it.
func CreateTestUserWithFields(t *testing.T, s *storage.Storage, username string, isAdmin bool) *model.User {
	t.Helper()

	user, err := s.CreateUser(&model.UserCreationRequest{
		Username: username,
		Password: "testpassword",
		IsAdmin:  isAdmin,
	})
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	return user
}

// CreateTestCategory creates a category for the given user and returns it.
func CreateTestCategory(t *testing.T, s *storage.Storage, userID int64) *model.Category {
	t.Helper()
	return CreateTestCategoryWithTitle(t, s, userID, fmt.Sprintf("TestCategory_%s", t.Name()))
}

// CreateTestCategoryWithTitle creates a category with a specific title and returns it.
func CreateTestCategoryWithTitle(t *testing.T, s *storage.Storage, userID int64, title string) *model.Category {
	t.Helper()

	category, err := s.CreateCategory(userID, &model.CategoryCreationRequest{
		Title: title,
	})
	if err != nil {
		t.Fatalf("failed to create test category: %v", err)
	}
	return category
}

// CreateTestFeed creates a feed with defaults and returns it.
func CreateTestFeed(t *testing.T, s *storage.Storage, userID, categoryID int64) *model.Feed {
	t.Helper()
	return CreateTestFeedWithURL(t, s, userID, categoryID,
		fmt.Sprintf("https://example.com/feed_%s.xml", t.Name()))
}

// CreateTestFeedWithURL creates a feed with a specific URL and returns it.
func CreateTestFeedWithURL(t *testing.T, s *storage.Storage, userID, categoryID int64, feedURL string) *model.Feed {
	t.Helper()

	feed := &model.Feed{
		UserID:  userID,
		FeedURL: feedURL,
		SiteURL: "https://example.com",
		Title:   fmt.Sprintf("Test Feed %s", t.Name()),
		Category: &model.Category{
			ID: categoryID,
		},
	}
	if err := s.CreateFeed(feed); err != nil {
		t.Fatalf("failed to create test feed: %v", err)
	}
	return feed
}

// CreateTestEntry creates a single entry and returns it.
func CreateTestEntry(t *testing.T, s *storage.Storage, userID, feedID int64) *model.Entry {
	t.Helper()
	return CreateTestEntryWithContent(t, s, userID, feedID,
		fmt.Sprintf("Entry Title %s", t.Name()),
		fmt.Sprintf("Entry content for %s", t.Name()))
}

// CreateTestEntryWithContent creates an entry with specific title and content.
func CreateTestEntryWithContent(t *testing.T, s *storage.Storage, userID, feedID int64, title, content string) *model.Entry {
	t.Helper()

	entry := &model.Entry{
		UserID:  userID,
		FeedID:  feedID,
		Hash:    fmt.Sprintf("hash_%s_%d", t.Name(), time.Now().UnixNano()),
		Title:   title,
		Content: content,
		URL:     fmt.Sprintf("https://example.com/entry_%s", t.Name()),
		Date:    time.Now(),
	}

	isNew, err := s.InsertEntryForFeed(userID, feedID, entry)
	if err != nil {
		t.Fatalf("failed to insert test entry: %v", err)
	}
	if !isNew {
		t.Fatal("expected new entry to be created")
	}
	return entry
}

// CreateTestEntries creates multiple entries and returns them.
func CreateTestEntries(t *testing.T, s *storage.Storage, userID, feedID int64, count int) model.Entries {
	t.Helper()

	entries := make(model.Entries, 0, count)
	for i := range count {
		entry := &model.Entry{
			UserID:  userID,
			FeedID:  feedID,
			Hash:    fmt.Sprintf("hash_%s_%d_%d", t.Name(), i, time.Now().UnixNano()),
			Title:   fmt.Sprintf("Entry #%d %s", i, t.Name()),
			Content: fmt.Sprintf("Content for entry %d in %s", i, t.Name()),
			URL:     fmt.Sprintf("https://example.com/entry_%s_%d", t.Name(), i),
			Date:    time.Now().Add(-time.Duration(count-i) * time.Hour),
		}

		isNew, err := s.InsertEntryForFeed(userID, feedID, entry)
		if err != nil {
			t.Fatalf("failed to insert test entry %d: %v", i, err)
		}
		if !isNew {
			t.Fatalf("expected new entry %d to be created", i)
		}
		entries = append(entries, entry)
	}
	return entries
}

// CreateTestAPIKey creates an API key for the given user.
func CreateTestAPIKey(t *testing.T, s *storage.Storage, userID int64) *model.APIKey {
	t.Helper()
	return CreateTestAPIKeyWithDescription(t, s, userID, fmt.Sprintf("test-key-%s", t.Name()))
}

// CreateTestAPIKeyWithDescription creates an API key with a specific description.
func CreateTestAPIKeyWithDescription(t *testing.T, s *storage.Storage, userID int64, description string) *model.APIKey {
	t.Helper()

	apiKey, err := s.CreateAPIKey(userID, description)
	if err != nil {
		t.Fatalf("failed to create test API key: %v", err)
	}
	return apiKey
}
