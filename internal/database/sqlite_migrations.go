// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	_ "embed"
)

type sqliteMigrationProvider struct{}

func (p *sqliteMigrationProvider) GetMigrations() []func(tx *sql.Tx) error {
	return sqliteMigrations[:]
}

func (p *sqliteMigrationProvider) SchemaVersion() int {
	return sqliteSchemaVersion
}

// NewSQLiteMigrationProvider returns a MigrationProvider for SQLite.
func NewSQLiteMigrationProvider() MigrationProvider {
	return &sqliteMigrationProvider{}
}

//go:embed schema/sqlite/schema.sql
var sqliteSchema string

var sqliteSchemaVersion = 1

var sqliteMigrations = []func(tx *sql.Tx) error{
	func(tx *sql.Tx) (err error) {
		_, err = tx.Exec(sqliteSchema)
		return err
	},
}
