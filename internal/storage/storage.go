// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"miniflux.app/v2/internal/database/dialect"

	"github.com/lib/pq"
)

// Storage handles all operations related to the database.
type Storage struct {
	db      *sql.DB
	dialect dialect.Dialect
}

// NewStorage returns a new Storage.
func NewStorage(db *sql.DB, d dialect.Dialect) *Storage {
	return &Storage{db, d}
}

// encodeArray encodes a slice as a database-compatible parameter.
// PostgreSQL uses pq.Array for native array support; SQLite uses JSON.
func (s *Storage) encodeArray(v any) any {
	if s.dialect.DatabaseType() == dialect.SQLite {
		data, err := json.Marshal(v)
		if err != nil {
			panic(fmt.Sprintf("storage: failed to encode array: %v", err))
		}
		return string(data)
	}
	return pq.Array(v)
}

// inClause returns a SQL fragment for list membership.
func (s *Storage) inClause(column string, n int) string {
	return s.dialect.InClause(column, n)
}

// notInClause returns a SQL fragment for negated list membership.
func (s *Storage) notInClause(column string, n int) string {
	if s.dialect.DatabaseType() == dialect.SQLite {
		return fmt.Sprintf("%s NOT IN (SELECT value FROM json_each($%d))", column, n)
	}
	return fmt.Sprintf("%s <> ALL($%d)", column, n)
}

// DatabaseVersion returns the version of the database which is in use.
func (s *Storage) DatabaseVersion() string {
	var dbVersion string
	err := s.db.QueryRow(s.dialect.ServerVersion()).Scan(&dbVersion)
	if err != nil {
		return err.Error()
	}

	return dbVersion
}

// DatabaseTypeName returns the human-readable name of the database backend
// ("PostgreSQL" or "SQLite"), used for display in the about page.
func (s *Storage) DatabaseTypeName() string {
	switch s.dialect.DatabaseType() {
	case dialect.SQLite:
		return "SQLite"
	default:
		return "PostgreSQL"
	}
}

// Ping checks if the database connection works.
func (s *Storage) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return s.db.PingContext(ctx)
}

// DBStats returns database statistics.
func (s *Storage) DBStats() sql.DBStats {
	return s.db.Stats()
}

// DBSize returns how much size the database is using in a pretty way.
func (s *Storage) DBSize() (string, error) {
	var size string

	err := s.db.QueryRow(s.dialect.CurrentDatabaseSize()).Scan(&size)
	if err != nil {
		return "", err
	}

	return size, nil
}
