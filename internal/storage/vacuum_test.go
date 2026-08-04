// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"path/filepath"
	"testing"

	"miniflux.app/v2/internal/database/dialect"

	_ "modernc.org/sqlite"
)

// newVacuumSQLiteStorage opens a dedicated SQLite database configured for
// incremental vacuum, so freelist semantics are deterministic (DELETE journal,
// not the WAL-based connection pool used elsewhere).
func newVacuumSQLiteStorage(t *testing.T) *Storage {
	t.Helper()

	dsn := filepath.Join(t.TempDir(), "vacuum.db")

	s := &Storage{dialect: &dialect.SQLiteDialect{}}

	var err error
	s.db, err = s.dialect.Open(dsn)
	if err != nil {
		t.Fatalf("failed to open test SQLite database: %v", err)
	}
	t.Cleanup(func() { s.db.Close() })

	for _, stmt := range []string{
		"PRAGMA journal_mode=DELETE",
		"PRAGMA auto_vacuum=INCREMENTAL",
		"PRAGMA synchronous=OFF",
		"PRAGMA foreign_keys=OFF",
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			t.Fatalf("failed to run %q: %v", stmt, err)
		}
	}

	// auto_vacuum takes effect only after a VACUUM on an existing/empty DB.
	if _, err := s.db.Exec("VACUUM"); err != nil {
		t.Fatalf("failed to activate incremental vacuum: %v", err)
	}

	return s
}

func TestVacuumIncrementalReclaimsFreePages(t *testing.T) {
	s := newVacuumSQLiteStorage(t)

	if _, err := s.db.Exec("CREATE TABLE t(x)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Populate then delete a large block of rows to create free pages.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("failed to begin: %v", err)
	}
	stmt, err := tx.Prepare("INSERT INTO t(x) VALUES (?)")
	if err != nil {
		t.Fatalf("failed to prepare: %v", err)
	}
	for i := 0; i < 2000; i++ {
		if _, err := stmt.Exec(int64(i)); err != nil {
			t.Fatalf("failed to insert: %v", err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatalf("failed to close stmt: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}
	if _, err := s.db.Exec("DELETE FROM t"); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	var beforeCount int
	if err := s.db.QueryRow("PRAGMA freelist_count").Scan(&beforeCount); err != nil {
		t.Fatalf("failed to read freelist_count: %v", err)
	}
	if beforeCount == 0 {
		t.Fatalf("expected free pages before incremental vacuum, got %d", beforeCount)
	}

	if err := s.VacuumIncremental(8192); err != nil {
		t.Fatalf("unexpected error from VacuumIncremental: %v", err)
	}

	var afterCount int
	if err := s.db.QueryRow("PRAGMA freelist_count").Scan(&afterCount); err != nil {
		t.Fatalf("failed to read freelist_count after vacuum: %v", err)
	}
	if afterCount >= beforeCount {
		t.Fatalf("expected freelist_count to shrink, before=%d after=%d", beforeCount, afterCount)
	}
}

func TestVacuumIncrementalPostgresNoOp(t *testing.T) {
	// A Nil *sql.DB would already return an error from Exec, so a no-op result
	// here proves the PostgreSQL branch short-circuits before touching the DB.
	s := &Storage{dialect: &dialect.PostgreSQLDialect{}}

	if s.db != nil {
		t.Fatal("internal: test relies on s.db being nil for the no-op check")
	}

	if err := s.VacuumIncremental(8192); err != nil {
		t.Fatalf("expected VacuumIncremental to be a no-op on PostgreSQL, got: %v", err)
	}
}
