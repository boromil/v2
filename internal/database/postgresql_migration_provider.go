// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import "database/sql"

type postgreSQLMigrationProvider struct{}

func (p *postgreSQLMigrationProvider) GetMigrations() []func(tx *sql.Tx) error {
	return migrations[:]
}

func (p *postgreSQLMigrationProvider) SchemaVersion() int {
	return schemaVersion
}

// NewPostgreSQLMigrationProvider returns a MigrationProvider for PostgreSQL.
func NewPostgreSQLMigrationProvider() MigrationProvider {
	return &postgreSQLMigrationProvider{}
}
