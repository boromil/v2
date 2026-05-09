// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"miniflux.app/v2/internal/crypto"
	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/model"
)

// ErrEntryTombstoned is returned when an entry cannot be created because its
// (feed_id, hash) pair has a tombstone recording a prior deletion.
var ErrEntryTombstoned = errors.New("store: entry is tombstoned")

// CountAllEntries returns the number of entries for each status in the database.
func (s *Storage) CountAllEntries() (map[string]int64, error) {
	rows, err := s.db.Query(`SELECT status, count(*) FROM entries GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("storage: unable to count entries: %w", err)
	}
	defer rows.Close()

	results := make(map[string]int64)
	results[model.EntryStatusUnread] = 0
	results[model.EntryStatusRead] = 0

	for rows.Next() {
		var status string
		var count int64

		if err := rows.Scan(&status, &count); err != nil {
			continue
		}

		results[status] = count
	}

	results["total"] = results[model.EntryStatusUnread] + results[model.EntryStatusRead]
	return results, nil
}

// UpdateEntryTitleAndContent updates entry title and content.
func (s *Storage) UpdateEntryTitleAndContent(entry *model.Entry) error {
	truncatedTitle, truncatedContent := truncateTitleAndContentForTSVectorField(entry.Title, entry.Content)
	query := fmt.Sprintf(`
		UPDATE
			entries
		SET
			title=$1,
			content=$2,
			reading_time=$3,
			document_vectors = %s
		WHERE
			id=$6 AND user_id=$7
	`, s.dialect.BuildDocumentVectors(4, 5))

	if _, err := s.db.Exec(
		query,
		entry.Title,
		entry.Content,
		entry.ReadingTime,
		truncatedTitle,
		truncatedContent,
		entry.ID,
		entry.UserID); err != nil {
		return fmt.Errorf(`store: unable to update entry #%d: %v`, entry.ID, err)
	}

	return nil
}

// createEntry add a new entry.
func (s *Storage) createEntry(tx *sql.Tx, entry *model.Entry) error {
	truncatedTitle, truncatedContent := truncateTitleAndContentForTSVectorField(entry.Title, entry.Content)
	// The WHERE NOT EXISTS guard makes the tombstone check atomic with the insert, so a
	// concurrent archive committing between an earlier existence check and this statement
	// cannot bring a deleted entry back as unread.
	query := fmt.Sprintf(`
		INSERT INTO entries
			(
				title,
				hash,
				url,
				comments_url,
				published_at,
				content,
				author,
				user_id,
				feed_id,
				reading_time,
				changed_at,
				document_vectors,
				tags,
				language
			)
		SELECT
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			%s,
			%s,
			$13,
			$14
		WHERE NOT EXISTS (
			SELECT 1 FROM entry_tombstones WHERE feed_id=$9 AND hash=$2
		)
		%s
	`,
		s.dialect.Now(),
		s.dialect.BuildDocumentVectors(11, 12),
		s.dialect.Returning("id", "status", "created_at", "changed_at"),
	)
	err := tx.QueryRow(
		query,
		entry.Title,
		entry.Hash,
		entry.URL,
		entry.CommentsURL,
		entry.Date,
		entry.Content,
		entry.Author,
		entry.UserID,
		entry.FeedID,
		entry.ReadingTime,
		truncatedTitle,
		truncatedContent,
		s.encodeArray(entry.Tags),
		entry.Language,
	).Scan(
		&entry.ID,
		&entry.Status,
		&entry.CreatedAt,
		&entry.ChangedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrEntryTombstoned
	}
	if err != nil {
		return fmt.Errorf(`store: unable to create entry %q (feed #%d): %v`, entry.URL, entry.FeedID, err)
	}

	for _, enclosure := range entry.Enclosures {
		enclosure.EntryID = entry.ID
		enclosure.UserID = entry.UserID
		err := s.createEnclosure(tx, enclosure)
		if err != nil {
			return err
		}
	}

	return nil
}

// updateEntry updates an entry when a feed is refreshed.
// Note: we do not update the published date because some feeds do not contains any date,
// it default to time.Now() which could change the order of items on the history page.
func (s *Storage) updateEntry(tx *sql.Tx, entry *model.Entry) error {
	truncatedTitle, truncatedContent := truncateTitleAndContentForTSVectorField(entry.Title, entry.Content)
	query := fmt.Sprintf(`
		UPDATE
			entries
		SET
			title=$1,
			url=$2,
			comments_url=$3,
			content=$4,
			author=$5,
			reading_time=$6,
			document_vectors = %s,
			tags=$12,
			language=$13
		WHERE
			user_id=$9 AND feed_id=$10 AND hash=$11
		%s
	`,
		s.dialect.BuildDocumentVectors(7, 8),
		s.dialect.Returning("id"),
	)
	err := tx.QueryRow(
		query,
		entry.Title,
		entry.URL,
		entry.CommentsURL,
		entry.Content,
		entry.Author,
		entry.ReadingTime,
		truncatedTitle,
		truncatedContent,
		entry.UserID,
		entry.FeedID,
		entry.Hash,
		s.encodeArray(entry.Tags),
		entry.Language,
	).Scan(&entry.ID)
	if err != nil {
		return fmt.Errorf(`store: unable to update entry %q: %v`, entry.URL, err)
	}

	for _, enclosure := range entry.Enclosures {
		enclosure.UserID = entry.UserID
		enclosure.EntryID = entry.ID
	}

	return s.updateEnclosures(tx, entry)
}

// entryExists checks if an entry already exists based on its hash when refreshing a feed.
func (s *Storage) entryExists(tx *sql.Tx, entry *model.Entry) (bool, error) {
	var result bool

	// Note: This query uses entries_feed_id_hash_key index (filtering on user_id is not necessary).
	err := tx.QueryRow(`SELECT true FROM entries WHERE feed_id=$1 AND hash=$2 LIMIT 1`, entry.FeedID, entry.Hash).Scan(&result)

	if err != nil && err != sql.ErrNoRows {
		return result, fmt.Errorf(`store: unable to check if entry exists: %v`, err)
	}

	return result, nil
}

func (s *Storage) getEntryIDByHash(tx *sql.Tx, feedID int64, entryHash string) (int64, error) {
	var entryID int64

	err := tx.QueryRow(
		`SELECT id FROM entries WHERE feed_id=$1 AND hash=$2 LIMIT 1`,
		feedID,
		entryHash,
	).Scan(&entryID)

	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf(`store: unable to fetch entry ID: %v`, err)
	}

	return entryID, nil
}

// InsertEntryForFeed inserts a single entry into a feed, optionally updating if it already exists.
// Returns true if a new entry was created, false if an existing one was reused.
func (s *Storage) InsertEntryForFeed(userID, feedID int64, entry *model.Entry) (bool, error) {
	entry.UserID = userID
	entry.FeedID = feedID

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("store: unable to start transaction: %v", err)
	}
	defer tx.Rollback()

	entryID, err := s.getEntryIDByHash(tx, entry.FeedID, entry.Hash)
	if err != nil {
		return false, err
	}
	alreadyExistingEntry := entryID > 0

	if alreadyExistingEntry {
		entry.ID = entryID
	} else {
		if err := s.createEntry(tx, entry); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}

	return !alreadyExistingEntry, nil
}

func (s *Storage) IsNewEntry(feedID int64, entryHash string) bool {
	// An entry is new only if it is neither stored nor tombstoned; otherwise
	// callers (such as the crawler) would do expensive work on every refresh
	// for items that will be discarded.
	query := `
		SELECT
			EXISTS (
				SELECT 1 FROM entries WHERE feed_id=$1 AND hash=$2
			) OR EXISTS (
				SELECT 1 FROM entry_tombstones WHERE feed_id=$1 AND hash=$2
			)
	`
	var known bool
	s.db.QueryRow(query, feedID, entryHash).Scan(&known)
	return !known
}

func (s *Storage) GetReadTime(feedID int64, entryHash string) int {
	var result int

	// Note: This query uses entries_feed_id_hash_key index
	s.db.QueryRow(
		`SELECT
			reading_time
		FROM
			entries
		WHERE
			feed_id=$1 AND
			hash=$2
		`,
		feedID,
		entryHash,
	).Scan(&result)
	return result
}

// RefreshFeedEntries updates feed entries while refreshing a feed.
func (s *Storage) RefreshFeedEntries(userID, feedID int64, entries model.Entries, updateExistingEntries bool) (newEntries model.Entries, err error) {
	for _, entry := range entries {
		entry.UserID = userID
		entry.FeedID = feedID

		tx, err := s.db.Begin()
		if err != nil {
			return nil, fmt.Errorf(`store: unable to start transaction: %v`, err)
		}

		entryExists, err := s.entryExists(tx, entry)
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return nil, fmt.Errorf(`store: unable to rollback transaction: %v (rolled back due to: %v)`, rollbackErr, err)
			}
			return nil, err
		}

		if entryExists {
			if updateExistingEntries {
				err = s.updateEntry(tx, entry)
			}
		} else {
			err = s.createEntry(tx, entry)
			switch {
			case errors.Is(err, ErrEntryTombstoned):
				err = nil
			case err == nil:
				newEntries = append(newEntries, entry)
			}
		}

		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				return nil, fmt.Errorf(`store: unable to rollback transaction: %v (rolled back due to: %v)`, rollbackErr, err)
			}
			return nil, err
		}

		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf(`store: unable to commit transaction: %v`, err)
		}
	}

	return newEntries, nil
}

// ArchiveEntries deletes entries older than the given interval and records tombstones so they are not re-ingested.
func (s *Storage) ArchiveEntries(status string, interval time.Duration, limit int) (int64, error) {
	if interval < 0 || limit <= 0 {
		return 0, nil
	}

	days := max(int(interval/(24*time.Hour)), 1)

	if s.dialect.DatabaseType() == dialect.SQLite {
		tx, err := s.db.Begin()
		if err != nil {
			return 0, fmt.Errorf(`store: unable to begin transaction for archive entries: %v`, err)
		}
		defer tx.Rollback()

		selectQuery := fmt.Sprintf(`
			SELECT id, feed_id, hash
			FROM entries
			WHERE
				status=$1 AND
				starred IS false AND
				share_code='' AND
				created_at < %s
			ORDER BY created_at ASC
			LIMIT $2
		`, s.dialect.NowSubtractInterval(fmt.Sprintf("'%d days'", days)))

		rows, err := tx.Query(selectQuery, status, limit)
		if err != nil {
			return 0, fmt.Errorf(`store: unable to select entries for archive: %v`, err)
		}
		defer rows.Close()

		type toDelete struct {
			id     int64
			feedID int64
			hash   string
		}
		var entries []toDelete
		for rows.Next() {
			var e toDelete
			if err := rows.Scan(&e.id, &e.feedID, &e.hash); err != nil {
				return 0, fmt.Errorf(`store: unable to scan entry for archive: %v`, err)
			}
			entries = append(entries, e)
		}
		rows.Close()

		if len(entries) == 0 {
			return 0, tx.Commit()
		}

		// Batch insert tombstones
		valuePlaceholders := make([]string, len(entries))
		args := make([]any, 0, len(entries)*2)
		for i, e := range entries {
			ph1 := i*2 + 1
			ph2 := i*2 + 2
			valuePlaceholders[i] = fmt.Sprintf("($%d, $%d)", ph1, ph2)
			args = append(args, e.feedID, e.hash)
		}
		insertQuery := fmt.Sprintf(
			`INSERT INTO entry_tombstones (feed_id, hash) VALUES %s ON CONFLICT (feed_id, hash) DO NOTHING`,
			strings.Join(valuePlaceholders, ", "),
		)
		if _, err := tx.Exec(insertQuery, args...); err != nil {
			return 0, fmt.Errorf(`store: unable to insert tombstones for archive: %v`, err)
		}

		// Batch delete entries
		idPlaceholders := make([]string, len(entries))
		ids := make([]any, len(entries))
		for i, e := range entries {
			idPlaceholders[i] = fmt.Sprintf("$%d", i+1)
			ids[i] = e.id
		}
		deleteQuery := fmt.Sprintf(
			`DELETE FROM entries WHERE id IN (%s)`,
			strings.Join(idPlaceholders, ", "),
		)
		if _, err := tx.Exec(deleteQuery, ids...); err != nil {
			return 0, fmt.Errorf(`store: unable to delete archived entries: %v`, err)
		}

		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf(`store: unable to commit archive entries: %v`, err)
		}

		return int64(len(entries)), nil
	}

	query := fmt.Sprintf(`
		WITH to_delete AS (
			SELECT id, feed_id, hash
			FROM entries
			WHERE
				status=$1 AND
				starred is false AND
				share_code='' AND
				created_at < %s
			ORDER BY created_at ASC
			%s
			LIMIT $2
		), deleted AS (
			DELETE FROM entries
			USING to_delete
			WHERE entries.id = to_delete.id
			%s
		)
		INSERT INTO entry_tombstones (feed_id, hash)
		SELECT feed_id, hash FROM deleted WHERE hash <> ''
		ON CONFLICT (feed_id, hash) DO NOTHING
	`,
		s.dialect.NowSubtractInterval(fmt.Sprintf("'%d days'", days)),
		s.dialect.ForUpdateSkipLocked(),
		s.dialect.Returning("entries.feed_id", "entries.hash"),
	)

	result, err := s.db.Exec(query, status, limit)
	if err != nil {
		return 0, fmt.Errorf(`store: unable to archive %s entries: %v`, status, err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf(`store: unable to get the number of rows affected: %v`, err)
	}

	return count, nil
}

// SetEntriesStatus update the status of the given list of entries.
func (s *Storage) SetEntriesStatus(userID int64, entryIDs []int64, status string) error {
	query := fmt.Sprintf(`
		UPDATE
			entries
		SET
			status=$1,
			changed_at=%s
		WHERE
			user_id=$2 AND
			%s
		`,
		s.dialect.Now(),
		s.inClause("id", 3),
	)
	if _, err := s.db.Exec(query, status, userID, s.encodeArray(entryIDs)); err != nil {
		return fmt.Errorf(`store: unable to update entries statuses %v: %v`, entryIDs, err)
	}

	return nil
}

// SetEntriesStatusAndCountVisible updates the status of the given entries and returns how many are visible in global views.
func (s *Storage) SetEntriesStatusAndCountVisible(userID int64, entryIDs []int64, status string) (int, error) {
	if s.dialect.DatabaseType() == dialect.SQLite {
		updateQuery := fmt.Sprintf("UPDATE entries SET status=$1, changed_at=%s WHERE user_id=$2 AND %s",
			s.dialect.Now(), s.inClause("id", 3))
		if _, err := s.db.Exec(updateQuery, status, userID, s.encodeArray(entryIDs)); err != nil {
			return 0, fmt.Errorf(`store: unable to update entries status %v: %v`, entryIDs, err)
		}

		countQuery := fmt.Sprintf(`
			SELECT count(*)
			FROM entries e
				JOIN feeds f ON (f.id = e.feed_id)
				JOIN categories c ON (c.id = f.category_id)
			WHERE e.user_id=$1 AND %s
				AND NOT f.hide_globally AND NOT c.hide_globally
		`, s.inClause("e.id", 2))
		var visible int
		if err := s.db.QueryRow(countQuery, userID, s.encodeArray(entryIDs)).Scan(&visible); err != nil {
			return 0, fmt.Errorf(`store: unable to count visible entries: %v`, err)
		}
		return visible, nil
	}

	query := fmt.Sprintf(`
		WITH updated AS (
			UPDATE entries
			SET
				status=$1,
				changed_at=%s
			WHERE
				user_id=$2 AND
				%s
			RETURNING feed_id
		)
		SELECT count(*)
		FROM updated u
			JOIN feeds f ON (f.id = u.feed_id)
			JOIN categories c ON (c.id = f.category_id)
		WHERE NOT f.hide_globally AND NOT c.hide_globally
	`,
		s.dialect.Now(),
		s.inClause("id", 3),
	)
	var visible int
	if err := s.db.QueryRow(query, status, userID, s.encodeArray(entryIDs)).Scan(&visible); err != nil {
		return 0, fmt.Errorf(`store: unable to update entries status %v: %v`, entryIDs, err)
	}
	return visible, nil
}

// SetEntriesStarredState updates the starred state for the given list of entries.
func (s *Storage) SetEntriesStarredState(userID int64, entryIDs []int64, starred bool) error {
	query := fmt.Sprintf("UPDATE entries SET starred=$1, changed_at=%s WHERE user_id=$2 AND %s", s.dialect.Now(), s.inClause("id", 3))
	result, err := s.db.Exec(query, starred, userID, s.encodeArray(entryIDs))
	if err != nil {
		return fmt.Errorf(`store: unable to update the starred state %v: %v`, entryIDs, err)
	}

	count, _ := result.RowsAffected()
	slog.Debug("Updated starred state for entries",
		slog.Int64("user_id", userID),
		slog.Int64("nb_entries", count),
	)

	return nil
}

// ToggleStarred toggles entry starred value.
func (s *Storage) ToggleStarred(userID int64, entryID int64) error {
	query := fmt.Sprintf("UPDATE entries SET starred = NOT starred, changed_at=%s WHERE user_id=$1 AND id=$2", s.dialect.Now())
	result, err := s.db.Exec(query, userID, entryID)
	if err != nil {
		return fmt.Errorf(`store: unable to toggle starred flag for entry #%d: %v`, entryID, err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf(`store: unable to toggle starred flag for entry #%d: %v`, entryID, err)
	}

	if count == 0 {
		return errors.New(`store: nothing has been updated`)
	}

	return nil
}

// FlushHistory deletes all read entries (non-starred, non-shared) and records tombstones to prevent re-ingestion.
func (s *Storage) FlushHistory(userID int64) error {
	if s.dialect.DatabaseType() == dialect.SQLite {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf(`store: unable to begin transaction for flush history: %v`, err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec(
			`INSERT INTO entry_tombstones (feed_id, hash)
			 SELECT feed_id, hash FROM entries
			 WHERE user_id=$1 AND status=$2 AND starred IS false AND share_code='' AND hash <> ''
			 ON CONFLICT (feed_id, hash) DO NOTHING`,
			userID, model.EntryStatusRead,
		); err != nil {
			return fmt.Errorf(`store: unable to insert tombstones for flush: %v`, err)
		}

		if _, err := tx.Exec(
			`DELETE FROM entries WHERE user_id=$1 AND status=$2 AND starred IS false AND share_code=''`,
			userID, model.EntryStatusRead,
		); err != nil {
			return fmt.Errorf(`store: unable to flush history: %v`, err)
		}

		return tx.Commit()
	}

	query := fmt.Sprintf(`
		WITH deleted AS (
			DELETE FROM entries
			WHERE user_id=$1 AND status=$2 AND starred is false AND share_code=''
			%s
		)
		INSERT INTO entry_tombstones (feed_id, hash)
		SELECT feed_id, hash FROM deleted WHERE hash <> ''
		ON CONFLICT (feed_id, hash) DO NOTHING
	`, s.dialect.Returning("feed_id", "hash"))
	if _, err := s.db.Exec(query, userID, model.EntryStatusRead); err != nil {
		return fmt.Errorf(`store: unable to flush history: %v`, err)
	}

	return nil
}

// MarkAllAsRead updates all user entries to the read status.
func (s *Storage) MarkAllAsRead(userID int64) error {
	query := fmt.Sprintf("UPDATE entries SET status=$1, changed_at=%s WHERE user_id=$2 AND status=$3", s.dialect.Now())
	result, err := s.db.Exec(query, model.EntryStatusRead, userID, model.EntryStatusUnread)
	if err != nil {
		return fmt.Errorf(`store: unable to mark all entries as read: %v`, err)
	}

	count, _ := result.RowsAffected()
	slog.Debug("Marked all entries as read",
		slog.Int64("user_id", userID),
		slog.Int64("nb_entries", count),
	)

	return nil
}

// MarkAllAsReadBeforeDate updates all user entries to the read status before the given date.
func (s *Storage) MarkAllAsReadBeforeDate(userID int64, before time.Time) error {
	query := fmt.Sprintf(`
		UPDATE
			entries
		SET
			status=$1,
			changed_at=%s
		WHERE
			user_id=$2 AND status=$3 AND published_at < $4
	`, s.dialect.Now())
	result, err := s.db.Exec(query, model.EntryStatusRead, userID, model.EntryStatusUnread, before)
	if err != nil {
		return fmt.Errorf(`store: unable to mark all entries as read before %s: %v`, before.Format(time.RFC3339), err)
	}
	count, _ := result.RowsAffected()
	slog.Debug("Marked all entries as read before date",
		slog.Int64("user_id", userID),
		slog.Int64("nb_entries", count),
		slog.String("before", before.Format(time.RFC3339)),
	)
	return nil
}

// MarkGloballyVisibleFeedsAsRead marks as read the unread entries that are
// visible in the global unread view, i.e. those belonging to a feed and a
// category that are both not hidden globally.
func (s *Storage) MarkGloballyVisibleFeedsAsRead(userID int64) error {
	var query string
	if s.dialect.DatabaseType() == dialect.SQLite {
		query = fmt.Sprintf(`
			UPDATE
				entries
			SET
				status=$1,
				changed_at=%s
			WHERE
				user_id=$2
			AND
				status=$3
			AND
				feed_id IN (
					SELECT f.id FROM feeds f
					JOIN categories c ON c.id = f.category_id
					WHERE f.user_id=$2 AND f.hide_globally=$4 AND c.hide_globally=$4
				)
		`, s.dialect.Now())
	} else {
		query = fmt.Sprintf(`
			UPDATE
				entries
			SET
				status=$1,
				changed_at=%s
			FROM
				feeds
				JOIN categories ON (categories.id = feeds.category_id)
			WHERE
				entries.feed_id = feeds.id
				AND entries.user_id=$2
				AND entries.status=$3
				AND feeds.hide_globally IS FALSE
				AND categories.hide_globally IS FALSE
		`, s.dialect.Now())
	}
	result, err := s.db.Exec(query, model.EntryStatusRead, userID, model.EntryStatusUnread, false)
	if err != nil {
		return fmt.Errorf(`store: unable to mark globally visible feeds as read: %v`, err)
	}

	count, _ := result.RowsAffected()
	slog.Debug("Marked globally visible feed entries as read",
		slog.Int64("user_id", userID),
		slog.Int64("nb_entries", count),
	)

	return nil
}

// MarkFeedAsRead updates all feed entries to the read status.
func (s *Storage) MarkFeedAsRead(userID, feedID int64, before time.Time) error {
	query := fmt.Sprintf(`
		UPDATE
			entries
		SET
			status=$1,
			changed_at=%s
		WHERE
			user_id=$2 AND feed_id=$3 AND status=$4 AND published_at < $5
	`, s.dialect.Now())
	result, err := s.db.Exec(query, model.EntryStatusRead, userID, feedID, model.EntryStatusUnread, before)
	if err != nil {
		return fmt.Errorf(`store: unable to mark feed entries as read: %v`, err)
	}

	count, _ := result.RowsAffected()
	slog.Debug("Marked feed entries as read",
		slog.Int64("user_id", userID),
		slog.Int64("feed_id", feedID),
		slog.Int64("nb_entries", count),
		slog.String("before", before.Format(time.RFC3339)),
	)

	return nil
}

// MarkCategoryAsRead updates all category entries to the read status.
func (s *Storage) MarkCategoryAsRead(userID, categoryID int64, before time.Time) error {
	var query string
	if s.dialect.DatabaseType() == dialect.SQLite {
		query = fmt.Sprintf(`
			UPDATE
				entries
			SET
				status=$1,
				changed_at=%s
			WHERE
				user_id=$2
			AND
				status=$3
			AND
				published_at < $4
			AND
				feed_id IN (SELECT f.id FROM feeds f WHERE f.user_id=$2 AND f.category_id=$5)
		`, s.dialect.Now())
	} else {
		query = fmt.Sprintf(`
			UPDATE
				entries
			SET
				status=$1,
				changed_at=%s
			FROM
				feeds
			WHERE
				feed_id=feeds.id
			AND
				feeds.user_id=$2
			AND
				status=$3
			AND
				published_at < $4
			AND
				feeds.category_id=$5
		`, s.dialect.Now())
	}
	result, err := s.db.Exec(query, model.EntryStatusRead, userID, model.EntryStatusUnread, before, categoryID)
	if err != nil {
		return fmt.Errorf(`store: unable to mark category entries as read: %v`, err)
	}

	count, _ := result.RowsAffected()
	slog.Debug("Marked category entries as read",
		slog.Int64("user_id", userID),
		slog.Int64("category_id", categoryID),
		slog.Int64("nb_entries", count),
		slog.String("before", before.Format(time.RFC3339)),
	)

	return nil
}

// EntryShareCode returns the share code of the provided entry.
// It generates a new one if not already defined.
func (s *Storage) EntryShareCode(userID int64, entryID int64) (shareCode string, err error) {
	query := `SELECT share_code FROM entries WHERE user_id=$1 AND id=$2`
	err = s.db.QueryRow(query, userID, entryID).Scan(&shareCode)
	if err != nil {
		err = fmt.Errorf(`store: unable to get share code for entry #%d: %v`, entryID, err)
		return
	}

	if shareCode == "" {
		shareCode = crypto.GenerateRandomStringHex(20)

		query = `UPDATE entries SET share_code = $1 WHERE user_id=$2 AND id=$3`
		_, err = s.db.Exec(query, shareCode, userID, entryID)
		if err != nil {
			err = fmt.Errorf(`store: unable to set share code for entry #%d: %v`, entryID, err)
			return
		}
	}

	return
}

// UnshareEntry removes the share code for the given entry.
func (s *Storage) UnshareEntry(userID int64, entryID int64) (err error) {
	query := `UPDATE entries SET share_code='' WHERE user_id=$1 AND id=$2`
	_, err = s.db.Exec(query, userID, entryID)
	if err != nil {
		err = fmt.Errorf(`store: unable to remove share code for entry #%d: %v`, entryID, err)
	}
	return
}

func truncateTitleAndContentForTSVectorField(title, content string) (string, string) {
	// The length of a tsvector (lexemes + positions) must be less than 1 megabyte.
	// We don't need to index the entire content, and we need to keep a buffer for the positions.
	return truncateStringForTSVectorField(title, 200000), truncateStringForTSVectorField(content, 500000)
}

// truncateStringForTSVectorField truncates a string and don't break UTF-8 characters.
func truncateStringForTSVectorField(s string, maxSize int) string {
	if len(s) < maxSize {
		return s
	}

	// Truncate to fit under the limit, ensuring we don't break UTF-8 characters
	truncated := s[:maxSize-1]

	// Walk backwards to find the last complete UTF-8 character
	for i := len(truncated) - 1; i >= 0; i-- {
		if (truncated[i] & 0x80) == 0 {
			// ASCII character, we can stop here
			return truncated[:i+1]
		}
		if (truncated[i] & 0xC0) == 0xC0 {
			// Start of a multi-byte UTF-8 character
			return truncated[:i]
		}
	}

	// Fallback: return empty string if we can't find a valid UTF-8 boundary
	return ""
}
