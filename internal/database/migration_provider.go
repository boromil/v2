// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import "database/sql"

// MigrationProvider defines the interface for database-specific migrations.
type MigrationProvider interface {
	// GetMigrations returns the ordered list of migration functions.
	GetMigrations() []func(tx *sql.Tx) error
	// SchemaVersion returns the latest schema version number.
	SchemaVersion() int
}
