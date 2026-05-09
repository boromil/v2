// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"testing"

	"miniflux.app/v2/internal/model"
)

func TestCreateUser(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestAdminUser(t, s)

			if user.ID == 0 {
				t.Error("expected non-zero user ID")
			}
			if user.Username == "" {
				t.Error("expected non-empty username")
			}
			if !user.IsAdmin {
				t.Error("expected admin user")
			}
		})
	}
}

func TestCreateRegularUser(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUserWithFields(t, s, "regularuser", false)

			if user.IsAdmin {
				t.Error("expected non-admin user")
			}
		})
	}
}

func TestUserExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUserWithFields(t, s, "existing_user", true)

			if !s.UserExists("existing_user") {
				t.Error("expected user to exist")
			}
			if s.UserExists("nonexistent_user") {
				t.Error("expected nonexistent user to not exist")
			}
			_ = user
		})
	}
}

func TestAnotherUserExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user1 := CreateTestUserWithFields(t, s, "user_one", false)
			CreateTestUserWithFields(t, s, "user_two", false)

			if s.AnotherUserExists(user1.ID, "user_one") {
				t.Error("expected same username to not conflict with itself")
			}
			if !s.AnotherUserExists(user1.ID, "user_two") {
				t.Error("expected different username to conflict")
			}
		})
	}
}

func TestUserByID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			fetched, err := s.UserByID(user.ID)
			if err != nil {
				t.Fatalf("UserByID failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil user")
			}
			if fetched.ID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, fetched.ID)
			}

			noUser, err := s.UserByID(99999)
			if err != nil {
				t.Fatalf("UserByID for nonexistent failed: %v", err)
			}
			if noUser != nil {
				t.Error("expected nil for nonexistent user")
			}
		})
	}
}

func TestUserByUsername(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			CreateTestUserWithFields(t, s, "unique_username", true)

			fetched, err := s.UserByUsername("unique_username")
			if err != nil {
				t.Fatalf("UserByUsername failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil user")
			}
		})
	}
}

func TestUserByField(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			CreateTestUserWithFields(t, s, "field_user", true)

			fetched, err := s.UserByField("username", "field_user")
			if err != nil {
				t.Fatalf("UserByField failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil user")
			}
		})
	}
}

func TestUpdateUser(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			user.Theme = "dark_serif"
			user.EntriesPerPage = 50
			if err := s.UpdateUser(user); err != nil {
				t.Fatalf("UpdateUser failed: %v", err)
			}

			fetched, _ := s.UserByID(user.ID)
			if fetched.Theme != "dark_serif" {
				t.Errorf("expected theme 'dark_serif', got %q", fetched.Theme)
			}
			if fetched.EntriesPerPage != 50 {
				t.Errorf("expected entries_per_page 50, got %d", fetched.EntriesPerPage)
			}
		})
	}
}

func TestSetLastLogin(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			if err := s.SetLastLogin(user.ID); err != nil {
				t.Fatalf("SetLastLogin failed: %v", err)
			}

			fetched, _ := s.UserByID(user.ID)
			if fetched.LastLoginAt == nil || fetched.LastLoginAt.IsZero() {
				t.Error("expected non-zero last_login_at")
			}
		})
	}
}

func TestCheckPassword(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			CreateTestUserWithFields(t, s, "pw_user", false)

			if err := s.CheckPassword("pw_user", "testpassword"); err != nil {
				t.Errorf("expected password to match, got error: %v", err)
			}
			if err := s.CheckPassword("pw_user", "wrongpassword"); err == nil {
				t.Error("expected wrong password to fail")
			}
		})
	}
}

func TestHasPassword(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			hasPassword, err := s.HasPassword(user.ID)
			if err != nil {
				t.Fatalf("HasPassword failed: %v", err)
			}
			if !hasPassword {
				t.Error("expected user to have password")
			}
		})
	}
}

func TestUsers(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			CreateTestUserWithFields(t, s, "list_user_1", false)
			CreateTestUserWithFields(t, s, "list_user_2", false)

			users, err := s.Users()
			if err != nil {
				t.Fatalf("Users failed: %v", err)
			}
			if len(users) < 2 {
				t.Errorf("expected at least 2 users, got %d", len(users))
			}
		})
	}
}

func TestCountUsers(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			CreateTestUser(t, s)

			count, err := s.CountUsers()
			if err != nil {
				t.Fatalf("CountUsers failed: %v", err)
			}
			if count < 1 {
				t.Errorf("expected at least 1 user, got %d", count)
			}
		})
	}
}

func TestRemoveUser(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUserWithFields(t, s, "remove_me", false)

			if err := s.RemoveUser(user.ID); err != nil {
				t.Fatalf("RemoveUser failed: %v", err)
			}
			if s.UserExists("remove_me") {
				t.Error("expected user to be removed")
			}
		})
	}
}

func TestUserByAPIKey(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			apiKey := CreateTestAPIKey(t, s, user.ID)

			fetched, err := s.UserByAPIKey(apiKey.Token)
			if err != nil {
				t.Fatalf("UserByAPIKey failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil user")
			}
			if fetched.ID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, fetched.ID)
			}
		})
	}
}

func TestUserLanguage(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			language := s.UserLanguage(user.ID)
			if language == "" {
				t.Error("expected non-empty language")
			}
		})
	}
}

// TestRefreshFeedEntries tests the feed refresh entry insertion (no updates).
func TestRefreshFeedEntries(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			newEntries := model.Entries{
				{UserID: user.ID, FeedID: feed.ID, Hash: "refresh_hash_1", Title: "New Entry 1", URL: "https://example.com/1"},
				{UserID: user.ID, FeedID: feed.ID, Hash: "refresh_hash_2", Title: "New Entry 2", URL: "https://example.com/2"},
			}

			created, err := s.RefreshFeedEntries(user.ID, feed.ID, newEntries, true)
			if err != nil {
				t.Fatalf("RefreshFeedEntries failed: %v", err)
			}
			if len(created) != 2 {
				t.Errorf("expected 2 new entries, got %d", len(created))
			}
		})
	}
}
