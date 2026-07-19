// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"testing"
	"time"

	"miniflux.app/v2/internal/model"
)

// TestIntegrationCRUD verifies the full integrations table schema by reading
// and writing all columns. This catches missing columns in the SQLite schema.
func TestIntegrationCRUD(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			// Integration row is created automatically when user is created
			integration, err := s.Integration(user.ID)
			if err != nil {
				t.Fatalf("Integration failed: %v", err)
			}
			if integration == nil {
				t.Fatal("expected non-nil integration")
			}
			if integration.UserID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, integration.UserID)
			}

			// Update integration with various values
			integration.PinboardEnabled = true
			integration.PinboardToken = "pinboard-token-test"
			integration.PinboardTags = "miniflux test"
			integration.WallabagEnabled = true
			integration.WallabagURL = "https://wallabag.example.com"
			integration.WallabagOnlyURL = true
			integration.WallabagTags = "wallabag test"
			integration.NtfyEnabled = true
			integration.NtfyTopic = "test-topic"
			integration.NtfyInternalLinks = true
			integration.ArchiveorgEnabled = true
			integration.KarakeepEnabled = true
			integration.KarakeepURL = "https://karakeep.example.com"
			integration.KarakeepTags = "karakeep test"
			integration.DiscordEnabled = true
			integration.DiscordWebhookLink = "https://discord.example.com/webhook"
			integration.SlackEnabled = true
			integration.SlackWebhookLink = "https://hooks.slack.com/test"
			integration.CuboxEnabled = true
			integration.CuboxAPILink = "https://cubox.example.com"

			if err := s.UpdateIntegration(integration); err != nil {
				t.Fatalf("UpdateIntegration failed: %v", err)
			}

			// Read back and verify
			fetched, err := s.Integration(user.ID)
			if err != nil {
				t.Fatalf("Integration re-read failed: %v", err)
			}
			if !fetched.PinboardEnabled {
				t.Error("expected PinboardEnabled to be true")
			}
			if fetched.PinboardToken != "pinboard-token-test" {
				t.Errorf("expected PinboardToken 'pinboard-token-test', got %q", fetched.PinboardToken)
			}
			if !fetched.WallabagOnlyURL {
				t.Error("expected WallabagOnlyURL to be true")
			}
			if !fetched.NtfyInternalLinks {
				t.Error("expected NtfyInternalLinks to be true")
			}
			if !fetched.DiscordEnabled {
				t.Error("expected DiscordEnabled to be true")
			}
			if !fetched.ArchiveorgEnabled {
				t.Error("expected ArchiveorgEnabled to be true")
			}
		})
	}
}

func TestEntryPublishedAtScan(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			feed := CreateTestFeed(t, s, user.ID, category.ID)

			now := time.Now()
			entry := &model.Entry{
				UserID:  user.ID,
				FeedID:  feed.ID,
				Hash:    "scan_test_hash",
				Title:   "Scan Test",
				Content: "Testing time scans",
				URL:     "https://example.com/scan-test",
				Date:    now,
			}
			_, err := s.InsertEntryForFeed(user.ID, feed.ID, entry)
			if err != nil {
				t.Fatalf("InsertEntryForFeed failed: %v", err)
			}

			// Fetch entry back — exercises published_at scan via fetchEntries
			builder := s.NewEntryQueryBuilder(user.ID)
			builder.WithEntryIDs(entry.ID)
			fetched, err := builder.GetEntry()
			if err != nil {
				t.Fatalf("GetEntry failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil entry")
			}
			// Verify published_at (Date) was properly scanned as time.Time
			if fetched.Date.IsZero() {
				t.Error("expected non-zero Date after scan")
			}

			// Test with count (GetEntriesWithCount) — exercises withCount=true scan path
			builder2 := s.NewEntryQueryBuilder(user.ID)
			builder2.WithEntryIDs(entry.ID)
			entries, count, err := builder2.GetEntriesWithCount()
			if err != nil {
				t.Fatalf("GetEntriesWithCount failed: %v", err)
			}
			if count != 1 {
				t.Errorf("expected count 1, got %d", count)
			}
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].Date.IsZero() {
				t.Error("expected non-zero Date after withCount scan")
			}
		})
	}
}
