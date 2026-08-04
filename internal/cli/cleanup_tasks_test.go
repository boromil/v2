// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	mrand "math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database"
	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/storage"

	_ "modernc.org/sqlite"
)

func TestShouldVacuumIncrementalBounds(t *testing.T) {
	// percent <= 0 means never.
	if shouldVacuumIncremental(func(int) int { return 0 }, 0) {
		t.Error("percent 0 must never trigger a vacuum")
	}
	if shouldVacuumIncremental(func(int) int { return 0 }, -5) {
		t.Error("negative percent must never trigger a vacuum")
	}

	// percent >= 100 means always.
	if !shouldVacuumIncremental(func(int) int { return 99 }, 100) {
		t.Error("percent 100 must always trigger a vacuum")
	}
	if !shouldVacuumIncremental(func(int) int { return 0 }, 150) {
		t.Error("percent >100 must always trigger a vacuum")
	}
}

func TestShouldVacuumIncrementalDeterministic(t *testing.T) {
	// Same seed must always yield the same decision (reproducibility).
	rngA := mrand.New(mrand.NewPCG(1, 2))
	rngB := mrand.New(mrand.NewPCG(1, 2))

	if shouldVacuumIncremental(rngA.IntN, 25) != shouldVacuumIncremental(rngB.IntN, 25) {
		t.Error("same seed must produce the same vacuum decision")
	}
}

func TestShouldVacuumIncrementalDistribution(t *testing.T) {
	// Empirical frequency with a high percent must be near-percent (sanity).
	rng := mrand.New(mrand.NewPCG(42, 9))
	percent := 50
	runs := 10000
	trueCount := 0
	for i := 0; i < runs; i++ {
		if shouldVacuumIncremental(rng.IntN, percent) {
			trueCount++
		}
	}

	observed := float64(trueCount) / float64(runs)
	if observed < 0.45 || observed > 0.55 {
		t.Fatalf("expected ~50%% trigger rate, observed %.3f", observed)
	}
}

// TestRunCleanupTasksIncrementalVacuumWiring proves the end-to-end path:
// runCleanupTasks reads config, passes the gated decision, and actually
// invokes VacuumIncremental against a live (file-backed) SQLite database,
// reclaiming freelist pages.
func TestRunCleanupTasksIncrementalVacuumWiring(t *testing.T) {
	d := &dialect.SQLiteDialect{}
	dsn := filepath.Join(t.TempDir(), "cleanup.db")

	db, err := database.NewConnectionPool(d, dsn, 1, 1, time.Hour)
	if err != nil {
		t.Fatalf("failed to open SQLite database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db, database.NewSQLiteMigrationProvider()); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	store := storage.NewStorage(db, d)

	// Force the vacuum to run on every cleanup pass via the environment, the
	// same mechanism a real deployment would use.
	t.Setenv("DATABASE_VACUUM_PERCENT", "100")

	// config.Opts is a package-level global read by runCleanupTasks; wrap any
	// mutation so it cannot leak into other tests in this package.
	restoreOpts := config.Opts
	t.Cleanup(func() { config.Opts = restoreOpts })

	config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("failed to init config: %v", err)
	}

	// Create a scratch table and fill + delete enough rows to guarantee free
	// pages exist on the freelist. This runs against the same migration-backed
	// database that runCleanupTasks will vacuum.
	if _, err := db.Exec("CREATE TABLE scratch(x)"); err != nil {
		t.Fatalf("failed to create scratch table: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	for i := 0; i < 3000; i++ {
		if _, err := tx.Exec("INSERT INTO scratch(x) VALUES (?)", i); err != nil {
			t.Fatalf("failed to insert scratch row: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit scratch tx: %v", err)
	}
	if _, err := db.Exec("DELETE FROM scratch"); err != nil {
		t.Fatalf("failed to clear scratch table: %v", err)
	}

	var before int
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&before); err != nil {
		t.Fatalf("failed to read freelist_count before: %v", err)
	}
	if before == 0 {
		t.Fatal("expected free pages to exist before running cleanup tasks")
	}

	runCleanupTasks(store, func(int) int { return 0 })

	var after int
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&after); err != nil {
		t.Fatalf("failed to read freelist_count after: %v", err)
	}
	if after >= before {
		t.Fatalf("expected incremental vacuum to shrink freelist through runCleanupTasks, before=%d after=%d", before, after)
	}
}

// TestRunCleanupTasksIncrementalVacuumInjectedRNG proves that the
// intermediate-percent (e.g. default 25%) path is controlled by the injected
// intN source rather than the global RNG: with an always-triggering intN the
// vacuum runs and reclaims freelist pages at percent=25, and with a
// never-triggering intN it does not.
func TestRunCleanupTasksIncrementalVacuumInjectedRNG(t *testing.T) {
	d := &dialect.SQLiteDialect{}
	dsn := filepath.Join(t.TempDir(), "cleanup_gated.db")

	db, err := database.NewConnectionPool(d, dsn, 1, 1, time.Hour)
	if err != nil {
		t.Fatalf("failed to open SQLite database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db, database.NewSQLiteMigrationProvider()); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	store := storage.NewStorage(db, d)

	t.Setenv("DATABASE_VACUUM_PERCENT", "25")

	// Protect the package-global config.Opts (see note in the other test).
	restoreOpts := config.Opts
	t.Cleanup(func() { config.Opts = restoreOpts })

	config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("failed to init config: %v", err)
	}

	// Helper: create deterministically-sized free pages on a scratch table.
	seedFreePages := func() int {
		if _, err := db.Exec("CREATE TABLE IF NOT EXISTS scratch(x)"); err != nil {
			t.Fatalf("failed to create scratch table: %v", err)
		}
		if _, err := db.Exec("DELETE FROM scratch"); err != nil {
			t.Fatalf("failed to clear scratch: %v", err)
		}
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("failed to begin: %v", err)
		}
		for i := 0; i < 3000; i++ {
			if _, err := tx.Exec("INSERT INTO scratch(x) VALUES (?)", i); err != nil {
				t.Fatalf("failed to insert: %v", err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("failed to commit: %v", err)
		}
		if _, err := db.Exec("DELETE FROM scratch"); err != nil {
			t.Fatalf("failed to delete: %v", err)
		}
		var n int
		if err := db.QueryRow("PRAGMA freelist_count").Scan(&n); err != nil {
			t.Fatalf("failed to read freelist_count: %v", err)
		}
		return n
	}

	// Never-triggering intN: at percent 25 the vacuum must NOT run, so the
	// freelist stays put.
	before := seedFreePages()
	runCleanupTasks(store, func(int) int { return 100 })
	var after int
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&after); err != nil {
		t.Fatalf("failed to read freelist_count after: %v", err)
	}
	if after != before {
		t.Fatalf("expected freelist unchanged when intN=100 at percent 25, before=%d after=%d", before, after)
	}

	// Always-triggering intN: vacuum runs and reclaims freelist pages.
	before = seedFreePages()
	runCleanupTasks(store, func(int) int { return 0 })
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&after); err != nil {
		t.Fatalf("failed to read freelist_count after: %v", err)
	}
	if after >= before {
		t.Fatalf("expected vacuum to run and shrink freelist when intN=0 at percent 25, before=%d after=%d", before, after)
	}
}
