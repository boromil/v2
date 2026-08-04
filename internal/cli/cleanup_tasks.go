// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cli // import "miniflux.app/v2/internal/cli"

import (
	"log/slog"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/metric"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
)

// runCleanupTasks runs the periodic maintenance tasks (session cleanup,
// entry archiving, orphan icon cleanup) and, probabilistically, an incremental
// vacuum to reclaim free SQLite pages.
//
// intN is the random source used for the probabilistic vacuum decision
// (a func returning a value in [0, n)). Production callers pass math/rand/v2's
// global IntN; tests pass a deterministic function.
func runCleanupTasks(store *storage.Storage, intN func(int) int) {
	if nbWebSessions, err := store.CleanOldWebSessions(config.Opts.CleanupRemoveSessionsInterval()); err != nil {
		slog.Error("Unable to clean old web sessions", slog.Any("error", err))
	} else {
		slog.Info("Sessions cleanup completed",
			slog.Int64("web_sessions_removed", nbWebSessions),
		)
	}

	startTime := time.Now()
	if rowsAffected, err := store.ArchiveEntries(model.EntryStatusRead, config.Opts.CleanupArchiveReadInterval(), config.Opts.CleanupArchiveBatchSize()); err != nil {
		slog.Error("Unable to archive read entries", slog.Any("error", err))
	} else {
		slog.Info("Archiving read entries completed",
			slog.Int64("read_entries_archived", rowsAffected),
		)

		if config.Opts.HasMetricsCollector() {
			metric.ArchiveEntriesDuration.WithLabelValues(model.EntryStatusRead).Observe(time.Since(startTime).Seconds())
		}
	}

	startTime = time.Now()
	if rowsAffected, err := store.ArchiveEntries(model.EntryStatusUnread, config.Opts.CleanupArchiveUnreadInterval(), config.Opts.CleanupArchiveBatchSize()); err != nil {
		slog.Error("Unable to archive unread entries", slog.Any("error", err))
	} else {
		slog.Info("Archiving unread entries completed",
			slog.Int64("unread_entries_archived", rowsAffected),
		)

		if config.Opts.HasMetricsCollector() {
			metric.ArchiveEntriesDuration.WithLabelValues(model.EntryStatusUnread).Observe(time.Since(startTime).Seconds())
		}
	}

	if nbIcons, err := store.CleanupOrphanIcons(); err != nil {
		slog.Error("Unable to clean orphan icons", slog.Any("error", err))
	} else {
		slog.Info("Orphan icons cleanup completed",
			slog.Int64("orphan_icons_removed", nbIcons),
		)
	}

	if shouldVacuumIncremental(intN, config.Opts.DatabaseVacuumPercent()) {
		if err := store.VacuumIncremental(config.Opts.DatabaseVacuumPages()); err != nil {
			slog.Error("Unable to run incremental vacuum", slog.Any("error", err))
		} else {
			slog.Info("Incremental vacuum completed",
				slog.Int("pages", config.Opts.DatabaseVacuumPages()),
			)
		}
	}
}

// shouldVacuumIncremental decides whether an incremental vacuum should be run
// on the current cleanup pass, with a given probability (percent in [0,100]).
// intN returns a value in [0, n); the decision is intN(100) < percent. Passing
// a deterministic intN makes the decision fully reproducible.
func shouldVacuumIncremental(intN func(int) int, percent int) bool {
	if percent <= 0 {
		return false
	}
	if percent >= 100 {
		return true
	}
	return intN(100) < percent
}
