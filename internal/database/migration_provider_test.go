// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database

import (
	"testing"
)

func TestPostgreSQLMigrationProvider(t *testing.T) {
	provider := NewPostgreSQLMigrationProvider()

	if provider.SchemaVersion() <= 0 {
		t.Errorf("expected positive schema version, got %d", provider.SchemaVersion())
	}

	migrations := provider.GetMigrations()
	if len(migrations) != provider.SchemaVersion() {
		t.Errorf("expected migration count %d, got %d", provider.SchemaVersion(), len(migrations))
	}
}

func TestSQLiteMigrationProvider(t *testing.T) {
	provider := NewSQLiteMigrationProvider()

	if provider.SchemaVersion() <= 0 {
		t.Errorf("expected positive schema version, got %d", provider.SchemaVersion())
	}

	migrations := provider.GetMigrations()
	if len(migrations) != provider.SchemaVersion() {
		t.Errorf("expected migration count %d, got %d", provider.SchemaVersion(), len(migrations))
	}
}

func TestSQLiteSchemaSQL(t *testing.T) {
	if sqliteSchema == "" {
		t.Error("expected embedded SQLite schema to be non-empty")
	}
}
