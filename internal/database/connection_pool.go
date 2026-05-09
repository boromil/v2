// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"database/sql"
	"time"

	"miniflux.app/v2/internal/database/dialect"

	_ "github.com/lib/pq"
)

// NewConnectionPool configures the database connection pool using the given dialect.
func NewConnectionPool(d dialect.Dialect, dsn string, minConnections, maxConnections int, connectionLifetime time.Duration) (*sql.DB, error) {
	if d.DatabaseType() == dialect.SQLite {
		registerSQLiteMD5()
	}
	db, err := d.Open(dsn)
	if err != nil {
		return nil, err
	}

	if d.DatabaseType() == dialect.SQLite {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(maxConnections)
	}
	db.SetMaxIdleConns(minConnections)
	db.SetConnMaxLifetime(connectionLifetime)

	return db, nil
}
