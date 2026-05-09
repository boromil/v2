// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// Migrate executes database migrations using the given provider.
func Migrate(db *sql.DB, provider MigrationProvider) error {
	var currentVersion int
	db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion)

	latestVersion := provider.SchemaVersion()
	migrations := provider.GetMigrations()

	slog.Info("Running database migrations",
		slog.Int("current_version", currentVersion),
		slog.Int("latest_version", latestVersion),
	)

	for version := currentVersion; version < latestVersion; version++ {
		newVersion := version + 1

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if err := migrations[version](tx); err != nil {
			tx.Rollback()
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if _, err := db.Exec(`DELETE FROM schema_version`); err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}

		if _, err := db.Exec(`INSERT INTO schema_version (version) VALUES ($1)`, newVersion); err != nil {
			return fmt.Errorf("[Migration v%d] %v", newVersion, err)
		}
	}

	return nil
}

// IsSchemaUpToDate checks if the database schema is up to date.
func IsSchemaUpToDate(db *sql.DB, provider MigrationProvider) error {
	var currentVersion int
	db.QueryRow(`SELECT version FROM schema_version`).Scan(&currentVersion)
	latestVersion := provider.SchemaVersion()
	if currentVersion < latestVersion {
		return fmt.Errorf(`the database schema is not up to date: current=v%d expected=v%d`, currentVersion, latestVersion)
	}
	return nil
}
