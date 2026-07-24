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

var sqliteSchemaVersion = 4

var sqliteMigrations = []func(tx *sql.Tx) error{
	func(tx *sql.Tx) (err error) {
		_, err = tx.Exec(sqliteSchema)
		return err
	},
	func(tx *sql.Tx) (err error) {
		_, err = tx.Exec(`
			ALTER TABLE feeds ADD COLUMN language text not null default '';
			ALTER TABLE entries ADD COLUMN language text not null default '';
		`)
		return err
	},
	func(tx *sql.Tx) (err error) {
		_, err = tx.Exec(`
			UPDATE webauthn_credentials SET name = '' WHERE name IS NULL;
			ALTER TABLE webauthn_credentials ADD COLUMN backup_eligible int;
			ALTER TABLE webauthn_credentials ADD COLUMN backup_state int not null default 0;
		`)
		return err
	},
	func(tx *sql.Tx) (err error) {
		_, err = tx.Exec(`
			ALTER TABLE users ADD COLUMN auto_fetch_short_entries int default 0;
		`)
		return err
	},
}
