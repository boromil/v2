// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"strings"
	"testing"

	"miniflux.app/v2/internal/database/dialect"
)

func TestEntryPaginationBuilderWithSearchQuery(t *testing.T) {
	tests := []struct {
		name     string
		dialect  dialect.Dialect
		expected string
	}{
		{
			name:     "postgres",
			dialect:  &dialect.PostgreSQLDialect{},
			expected: "document_vectors @@ plainto_tsquery",
		},
		{
			name:     "sqlite",
			dialect:  &dialect.SQLiteDialect{},
			expected: "fts_entries MATCH",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Storage{dialect: tt.dialect}
			b := s.NewEntryPaginationBuilder(1, 100, "published_at", "desc")
			b.WithSearchQuery("test query")

			if len(b.conditions) != 2 {
				t.Fatalf("expected 2 conditions, got %d", len(b.conditions))
			}
			if b.conditions[0] != "e.user_id = $1" {
				t.Errorf("expected user_id condition, got %q", b.conditions[0])
			}
			if !strings.Contains(b.conditions[1], tt.expected) {
				t.Errorf("search condition %q does not contain %q", b.conditions[1], tt.expected)
			}
			if len(b.args) != 2 {
				t.Fatalf("expected 2 args, got %d", len(b.args))
			}
			if b.args[1] != "test query" {
				t.Errorf("expected search arg 'test query', got %v", b.args[1])
			}
		})
	}
}

func TestEntryPaginationBuilderWithSearchQueryEmpty(t *testing.T) {
	s := &Storage{dialect: &dialect.SQLiteDialect{}}
	b := s.NewEntryPaginationBuilder(1, 100, "published_at", "desc")
	b.WithSearchQuery("")

	if len(b.conditions) != 1 {
		t.Errorf("expected 1 condition for empty query, got %d", len(b.conditions))
	}
	if len(b.args) != 1 {
		t.Errorf("expected 1 arg for empty query, got %d", len(b.args))
	}
}

func TestEntryPaginationBuilderWithTags(t *testing.T) {
	tests := []struct {
		name     string
		dialect  dialect.Dialect
		contains string
	}{
		{
			name:     "postgres",
			dialect:  &dialect.PostgreSQLDialect{},
			contains: "ANY(LOWER(e.tags::text)::text[])",
		},
		{
			name:     "sqlite",
			dialect:  &dialect.SQLiteDialect{},
			contains: "json_each(e.tags)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Storage{dialect: tt.dialect}
			b := s.NewEntryPaginationBuilder(1, 100, "published_at", "desc")
			b.WithTags([]string{"golang", "testing"})

			if len(b.conditions) != 3 {
				t.Fatalf("expected 3 conditions, got %d", len(b.conditions))
			}
			for _, cond := range b.conditions[1:] {
				if !strings.Contains(cond, tt.contains) {
					t.Errorf("tags condition %q does not contain %q", cond, tt.contains)
				}
			}
			if len(b.args) != 3 {
				t.Fatalf("expected 3 args, got %d", len(b.args))
			}
			if b.args[1] != "golang" || b.args[2] != "testing" {
				t.Errorf("unexpected tag args: %v", b.args[1:])
			}
		})
	}
}

func TestEntryPaginationBuilderWithTagsEmpty(t *testing.T) {
	s := &Storage{dialect: &dialect.SQLiteDialect{}}
	b := s.NewEntryPaginationBuilder(1, 100, "published_at", "desc")
	b.WithTags(nil)

	if len(b.conditions) != 1 {
		t.Errorf("expected 1 condition for nil tags, got %d", len(b.conditions))
	}
}

func TestEntryPaginationBuilderPlaceholderSpacing(t *testing.T) {
	// Verify placeholder indices are sequential and correctly spaced
	s := &Storage{dialect: &dialect.PostgreSQLDialect{}}
	b := s.NewEntryPaginationBuilder(1, 100, "published_at", "desc")
	b.WithStatus("unread")  // adds $2
	b.WithSearchQuery("go") // adds $3

	if !strings.Contains(b.conditions[2], "$3") {
		t.Errorf("expected search condition to use $3, got %q", b.conditions[2])
	}
}
