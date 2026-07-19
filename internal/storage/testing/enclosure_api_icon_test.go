// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"testing"
	"time"

	"miniflux.app/v2/internal/model"
)

func TestEnclosureCreation(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			enclosure := &model.Enclosure{
				URL:      "https://example.com/test-enclosure.mp3",
				MimeType: "audio/mpeg",
				Size:     12345,
			}
			entry := &model.Entry{
				UserID:     user.ID,
				FeedID:     feed.ID,
				Hash:       "enclosure_test_hash_" + t.Name(),
				Title:      "Entry with enclosure",
				Content:    "Content with enclosure",
				URL:        "https://example.com/enclosure-entry",
				Date:       time.Now(),
				Enclosures: model.EnclosureList{enclosure},
			}
			_, err := s.InsertEntryForFeed(user.ID, feed.ID, entry)
			if err != nil {
				t.Fatalf("InsertEntryForFeed failed: %v", err)
			}

			enclosures, err := s.EnclosuresByEntryID(entry.ID)
			if err != nil {
				t.Fatalf("EnclosuresByEntryID failed: %v", err)
			}
			if len(enclosures) < 1 {
				t.Error("expected at least 1 enclosure")
			}
		})
	}
}

func TestGetEnclosuresForEntries(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			e1 := &model.Entry{
				UserID: user.ID, FeedID: feed.ID,
				Hash: "enc_test_e1", Title: "E1", URL: "https://a.com/e1",
				Date: time.Now(),
				Enclosures: model.EnclosureList{
					{URL: "https://a.com/e1_enc.mp3", MimeType: "audio/mpeg", Size: 100},
				},
			}
			e2 := &model.Entry{
				UserID: user.ID, FeedID: feed.ID,
				Hash: "enc_test_e2", Title: "E2", URL: "https://a.com/e2",
				Date: time.Now(),
				Enclosures: model.EnclosureList{
					{URL: "https://a.com/e2_enc.mp4", MimeType: "video/mp4", Size: 200},
				},
			}
			s.InsertEntryForFeed(user.ID, feed.ID, e1)
			s.InsertEntryForFeed(user.ID, feed.ID, e2)

			result, err := s.EnclosuresByEntryIDs([]int64{e1.ID, e2.ID})
			if err != nil {
				t.Fatalf("EnclosuresByEntryIDs failed: %v", err)
			}
			if len(result) != 2 {
				t.Errorf("expected 2 entry groups, got %d", len(result))
			}
		})
	}
}

func TestGetEnclosure(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			entry := &model.Entry{
				UserID: user.ID, FeedID: feed.ID,
				Hash: "enclosure_get_test", Title: "Get Enclosure Test",
				URL: "https://a.com/get_enc", Date: time.Now(),
				Enclosures: model.EnclosureList{
					{URL: "https://a.com/unique_enc.mp3", MimeType: "audio/mpeg", Size: 500},
				},
			}
			_, err := s.InsertEntryForFeed(user.ID, feed.ID, entry)
			if err != nil {
				t.Fatalf("InsertEntryForFeed failed: %v", err)
			}

			enclosures, _ := s.EnclosuresByEntryID(entry.ID)
			if len(enclosures) == 0 {
				t.Fatal("expected enclosures to be created")
			}

			fetched, err := s.EnclosureByID(user.ID, enclosures[0].ID)
			if err != nil {
				t.Fatalf("EnclosureByID failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil enclosure")
			}
		})
	}
}

func TestAPIKeys(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestAPIKeyWithDescription(t, s, user.ID, "key_one")
			CreateTestAPIKeyWithDescription(t, s, user.ID, "key_two")

			keys, err := s.APIKeys(user.ID)
			if err != nil {
				t.Fatalf("APIKeys failed: %v", err)
			}
			if len(keys) < 2 {
				t.Errorf("expected at least 2 API keys, got %d", len(keys))
			}
		})
	}
}

func TestDeleteAPIKey(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			key := CreateTestAPIKey(t, s, user.ID)

			if err := s.DeleteAPIKey(user.ID, key.ID); err != nil {
				t.Fatalf("DeleteAPIKey failed: %v", err)
			}
			keys, _ := s.APIKeys(user.ID)
			if len(keys) != 0 {
				t.Errorf("expected 0 API keys, got %d", len(keys))
			}
		})
	}
}

func TestAPIKeyExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestAPIKeyWithDescription(t, s, user.ID, "exists_check")

			if !s.APIKeyExists(user.ID, "exists_check") {
				t.Error("expected API key to exist")
			}
			if s.APIKeyExists(user.ID, "nonexistent_key") {
				t.Error("expected nonexistent API key to not exist")
			}
		})
	}
}

func TestSetAPIKeyUsedTimestamp(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			key := CreateTestAPIKey(t, s, user.ID)

			if err := s.SetAPIKeyUsedTimestamp(user.ID, key.Token); err != nil {
				t.Fatalf("SetAPIKeyUsedTimestamp failed: %v", err)
			}
		})
	}
}

func TestIcon(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			icon := &model.Icon{
				Hash:       "icon_hash_test",
				MimeType:   "image/png",
				Content:    []byte{0x89, 0x50, 0x4E, 0x47},
				ExternalID: "external_icon_1",
			}
			if err := s.StoreFeedIcon(feed.ID, icon); err != nil {
				t.Fatalf("StoreFeedIcon failed: %v", err)
			}

			if !s.HasFeedIcon(feed.ID) {
				t.Error("expected feed to have icon")
			}
		})
	}
}

func TestIconByFeedID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			icon := &model.Icon{
				Hash:       "feed_icon_hash",
				MimeType:   "image/svg+xml",
				Content:    []byte("<svg></svg>"),
				ExternalID: "feed_icon_ext",
			}
			if err := s.StoreFeedIcon(feed.ID, icon); err != nil {
				t.Fatalf("StoreFeedIcon failed: %v", err)
			}

			fetched, err := s.IconByFeedID(user.ID, feed.ID)
			if err != nil {
				t.Fatalf("IconByFeedID failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil icon")
			}
		})
	}
}
