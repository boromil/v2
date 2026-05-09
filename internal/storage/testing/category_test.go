// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"testing"
)

func TestCreateCategory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			if category.ID == 0 {
				t.Error("expected non-zero category ID")
			}
			if category.UserID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, category.UserID)
			}
		})
	}
}

func TestCategoryTitleExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestCategoryWithTitle(t, s, user.ID, "FoobarCategory")

			if !s.CategoryTitleExists(user.ID, "FoobarCategory") {
				t.Error("expected category title to exist")
			}
			if s.CategoryTitleExists(user.ID, "NonexistentTitle") {
				t.Error("expected nonexistent title to not exist")
			}
		})
	}
}

func TestCategoryIDExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			if !s.CategoryIDExists(user.ID, category.ID) {
				t.Error("expected category ID to exist")
			}
			if s.CategoryIDExists(user.ID, 99999) {
				t.Error("expected nonexistent category ID to not exist")
			}
		})
	}
}

func TestCategory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			fetched, err := s.Category(user.ID, category.ID)
			if err != nil {
				t.Fatalf("Category failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil category")
			}
			if fetched.ID != category.ID {
				t.Errorf("expected category ID %d, got %d", category.ID, fetched.ID)
			}

			noCat, err := s.Category(user.ID, 99999)
			if err != nil {
				t.Fatalf("Category for nonexistent failed: %v", err)
			}
			if noCat != nil {
				t.Error("expected nil for nonexistent category")
			}
		})
	}
}

func TestFirstCategory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestCategoryWithTitle(t, s, user.ID, "ZZZZZZZZZ")
			CreateTestCategoryWithTitle(t, s, user.ID, "AAAAAAAA")

			first, err := s.FirstCategory(user.ID)
			if err != nil {
				t.Fatalf("FirstCategory failed: %v", err)
			}
			if first == nil {
				t.Fatal("expected non-nil first category")
			}
			if first.Title != "AAAAAAAA" {
				t.Errorf("expected first alphabetical category 'AAAAAAAA', got %q", first.Title)
			}
		})
	}
}

func TestCategoryByTitle(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestCategoryWithTitle(t, s, user.ID, "MySpecialCategory")

			fetched, err := s.CategoryByTitle(user.ID, "MySpecialCategory")
			if err != nil {
				t.Fatalf("CategoryByTitle failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil category")
			}
			if fetched.Title != "MySpecialCategory" {
				t.Errorf("expected title 'MySpecialCategory', got %q", fetched.Title)
			}
		})
	}
}

func TestCategories(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			CreateTestCategoryWithTitle(t, s, user.ID, "Cat_One")
			CreateTestCategoryWithTitle(t, s, user.ID, "Cat_Two")

			categories, err := s.Categories(user.ID)
			if err != nil {
				t.Fatalf("Categories failed: %v", err)
			}
			if len(categories) < 2 {
				t.Errorf("expected at least 2 categories, got %d", len(categories))
			}
		})
	}
}

func TestUpdateCategory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			category.Title = "Updated Category Title"
			if err := s.UpdateCategory(category); err != nil {
				t.Fatalf("UpdateCategory failed: %v", err)
			}

			fetched, _ := s.Category(user.ID, category.ID)
			if fetched.Title != "Updated Category Title" {
				t.Errorf("expected title 'Updated Category Title', got %q", fetched.Title)
			}
		})
	}
}

func TestRemoveCategory(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)

			if err := s.RemoveCategory(user.ID, category.ID); err != nil {
				t.Fatalf("RemoveCategory failed: %v", err)
			}
			if s.CategoryIDExists(user.ID, category.ID) {
				t.Error("expected category to be removed")
			}
		})
	}
}

func TestCategoriesWithFeedCount(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category := CreateTestCategory(t, s, user.ID)
			CreateTestFeed(t, s, user.ID, category.ID)

			categories, err := s.CategoriesWithFeedCount(user.ID, "alphabetical")
			if err != nil {
				t.Fatalf("CategoriesWithFeedCount failed: %v", err)
			}
			if len(categories) < 1 {
				t.Error("expected at least 1 category")
			}
		})
	}
}

func TestAnotherCategoryExists(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			category1 := CreateTestCategoryWithTitle(t, s, user.ID, "CategoryX")
			category2 := CreateTestCategoryWithTitle(t, s, user.ID, "CategoryY")

			if s.AnotherCategoryExists(user.ID, category1.ID, "CategoryX") {
				t.Error("expected same category to not conflict with itself")
			}
			if !s.AnotherCategoryExists(user.ID, category1.ID, "CategoryY") {
				t.Error("expected different category title to conflict")
			}
			_ = category2
		})
	}
}

func TestRemoveAndReplaceCategoriesByName(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)
			cat1 := CreateTestCategoryWithTitle(t, s, user.ID, "CatToKeep")
			CreateTestCategoryWithTitle(t, s, user.ID, "CatToRemove")
			CreateTestFeed(t, s, user.ID, cat1.ID)

			// Remove "CatToRemove" — feeds get reassigned to first remaining category
			err := s.RemoveAndReplaceCategoriesByName(user.ID, []string{"CatToRemove"})
			if err != nil {
				t.Fatalf("RemoveAndReplaceCategoriesByName failed: %v", err)
			}

			// Verify CatToRemove is gone
			if s.CategoryTitleExists(user.ID, "CatToRemove") {
				t.Error("expected CatToRemove to be deleted")
			}
			// Verify CatToKeep still exists
			if !s.CategoryTitleExists(user.ID, "CatToKeep") {
				t.Error("expected CatToKeep to remain")
			}
		})
	}
}
