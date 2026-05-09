// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package storage // import "miniflux.app/v2/internal/storage"

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/timezone"
)

// timeParseFormats are tried in order when scanning a timestamp value.
//
// Background: modernc.org/sqlite v1.50.0's built-in parseTime() function
// (conn.go:87) tries a fixed set of time formats against TEXT columns declared
// as DATE/DATETIME/TIMESTAMP. If none match, it returns the raw string instead
// of time.Time. This manifests as a Scan error when database/sql tries to scan
// the string into *time.Time.
//
// The bug surfaces in 6-table LEFT JOIN queries (entries + feeds + categories
// + feed_icons + icons + users). Simpler queries work correctly. It affects
// entries imported from OPML files where timestamps may be stored in formats
// the driver doesn't recognize, including Go's time.Time.String() format
// ("2026-02-22 08:00:00 +0000 UTC") and corrupted double-timezone values
// ("2026-05-08 17:22:08 +1000 +1000") from legacy imports.
//
// The workaround: scan datetime columns into interface{}, then parse with
// scanTime() which tries a broader set of formats and can strip trailing
// timezone tokens.
var timeParseFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05Z07:00",
	time.DateTime,
	time.DateOnly,
}

func scanTime(v any) (time.Time, error) {
	switch v := v.(type) {
	case time.Time:
		return v, nil
	case string:
		for _, f := range timeParseFormats {
			if t, err := time.Parse(f, v); err == nil {
				return t, nil
			}
		}
		// Try stripping a trailing timezone token (some imported entries
		// have doubled timezone info like "+1000 +1000").
		if t, err := stripAndParse(v); err == nil {
			return t, nil
		}
		return time.Time{}, fmt.Errorf("unable to parse time: %q", v)
	default:
		return time.Time{}, fmt.Errorf("unexpected type for time: %T", v)
	}
}

func stripAndParse(s string) (time.Time, error) {
	lastSpace := strings.LastIndexByte(s, ' ')
	if lastSpace < 0 {
		return time.Time{}, fmt.Errorf("no space in timestamp")
	}
	stripped := s[:lastSpace]
	for _, f := range timeParseFormats {
		if t, err := time.Parse(f, stripped); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse stripped time: %q", stripped)
}

// EntryQueryBuilder builds a SQL query to fetch entries.
type EntryQueryBuilder struct {
	store           *Storage
	args            []any
	conditions      []string
	sortExpressions []string
	limit           int
	offset          int
	fetchEnclosures bool
	excludeContent  bool
}

// WithEnclosures fetches enclosures for each entry.
func (e *EntryQueryBuilder) WithEnclosures() *EntryQueryBuilder {
	e.fetchEnclosures = true
	return e
}

// WithoutContent excludes the content column from the query results,
// replacing it with an empty string. This significantly reduces data
// transfer from PostgreSQL on list pages where content is not displayed.
func (e *EntryQueryBuilder) WithoutContent() *EntryQueryBuilder {
	e.excludeContent = true
	return e
}

// WithSearchQuery adds full-text search query to the condition.
func (e *EntryQueryBuilder) WithSearchQuery(query string) *EntryQueryBuilder {
	if query != "" {
		if e.store.dialect.DatabaseType() == dialect.SQLite {
			query = escapeFTS5Query(query)
		}

		nArgs := len(e.args) + 1
		e.conditions = append(e.conditions, e.store.dialect.FtsCondition(nArgs))
		e.args = append(e.args, query)

		// 0.0000001 = 0.1 / (seconds_in_a_day)
		e.WithSorting(
			fmt.Sprintf("%s - %s * 0.0000001",
				e.store.dialect.FtsRank(nArgs),
				fmt.Sprintf("(%s - %s)", e.store.dialect.ExtractEpoch(e.store.dialect.Now()), e.store.dialect.ExtractEpoch("published_at"))),
			"DESC",
		)
	}
	return e
}

// escapeFTS5Query quotes numeric-looking tokens containing dots to prevent
// FTS5 from parsing them as float literals (e.g. "2.0.8" causes "fts5: syntax
// error near '.'"). The problematic token is wrapped in double quotes so FTS5
// treats it as a literal phrase. Other tokens are left unchanged.
func escapeFTS5Query(q string) string {
	tokens := strings.Fields(q)
	for i, t := range tokens {
		if containsNumericDot(t) {
			tokens[i] = `"` + t + `"`
		}
	}
	return strings.Join(tokens, " ")
}

func containsNumericDot(s string) bool {
	hasDigit := false
	for _, c := range s {
		if c >= '0' && c <= '9' {
			hasDigit = true
		}
		if c == '.' && hasDigit {
			return true
		}
	}
	return false
}

// WithStarred adds starred filter.
func (e *EntryQueryBuilder) WithStarred(starred bool) *EntryQueryBuilder {
	if starred {
		e.conditions = append(e.conditions, "e.starred is true")
	} else {
		e.conditions = append(e.conditions, "e.starred is false")
	}
	return e
}

// BeforeChangedDate adds a condition < changed_at
func (e *EntryQueryBuilder) BeforeChangedDate(date time.Time) *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.changed_at < $"+strconv.Itoa(len(e.args)+1))
	e.args = append(e.args, date)
	return e
}

// AfterChangedDate adds a condition > changed_at
func (e *EntryQueryBuilder) AfterChangedDate(date time.Time) *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.changed_at > $"+strconv.Itoa(len(e.args)+1))
	e.args = append(e.args, date)
	return e
}

// BeforePublishedDate adds a condition < published_at
func (e *EntryQueryBuilder) BeforePublishedDate(date time.Time) *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.published_at < $"+strconv.Itoa(len(e.args)+1))
	e.args = append(e.args, date)
	return e
}

// AfterPublishedDate adds a condition > published_at
func (e *EntryQueryBuilder) AfterPublishedDate(date time.Time) *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.published_at > $"+strconv.Itoa(len(e.args)+1))
	e.args = append(e.args, date)
	return e
}

// BeforeEntryID adds a condition < entryID.
func (e *EntryQueryBuilder) BeforeEntryID(entryID int64) *EntryQueryBuilder {
	if entryID != 0 {
		e.conditions = append(e.conditions, "e.id < $"+strconv.Itoa(len(e.args)+1))
		e.args = append(e.args, entryID)
	}
	return e
}

// AfterEntryID adds a condition > entryID.
func (e *EntryQueryBuilder) AfterEntryID(entryID int64) *EntryQueryBuilder {
	if entryID != 0 {
		e.conditions = append(e.conditions, "e.id > $"+strconv.Itoa(len(e.args)+1))
		e.args = append(e.args, entryID)
	}
	return e
}

// WithEntryIDs filter by entry IDs.
func (e *EntryQueryBuilder) WithEntryIDs(entryIDs ...int64) *EntryQueryBuilder {
	if len(entryIDs) == 1 {
		e.conditions = append(e.conditions, fmt.Sprintf("e.id = $%d", len(e.args)+1))
		e.args = append(e.args, entryIDs[0])
	} else if len(entryIDs) > 1 {
		e.conditions = append(e.conditions, e.store.inClause("e.id", len(e.args)+1))
		e.args = append(e.args, e.store.encodeArray(entryIDs))
	}
	return e
}

// WithFeedID filter by feed ID.
func (e *EntryQueryBuilder) WithFeedID(feedID int64) *EntryQueryBuilder {
	if feedID > 0 {
		e.conditions = append(e.conditions, "e.feed_id = $"+strconv.Itoa(len(e.args)+1))
		e.args = append(e.args, feedID)
	}
	return e
}

// WithCategoryID filter by category ID.
func (e *EntryQueryBuilder) WithCategoryID(categoryID int64) *EntryQueryBuilder {
	if categoryID > 0 {
		e.conditions = append(e.conditions, "f.category_id = $"+strconv.Itoa(len(e.args)+1))
		e.args = append(e.args, categoryID)
	}
	return e
}

// WithStatuses filter by a list of entry statuses.
func (e *EntryQueryBuilder) WithStatuses(statuses ...string) *EntryQueryBuilder {
	if len(statuses) == 1 {
		e.conditions = append(e.conditions, fmt.Sprintf("e.status = $%d", len(e.args)+1))
		e.args = append(e.args, statuses[0])
	} else if len(statuses) > 1 {
		e.conditions = append(e.conditions, e.store.inClause("e.status", len(e.args)+1))
		e.args = append(e.args, e.store.encodeArray(statuses))
	}
	return e
}

// WithTags filter by a list of entry tags.
func (e *EntryQueryBuilder) WithTags(tags ...string) *EntryQueryBuilder {
	if len(tags) > 0 {
		for _, cat := range tags {
			if e.store.dialect.DatabaseType() == dialect.SQLite {
				e.conditions = append(e.conditions, fmt.Sprintf("LOWER($%d) IN (SELECT LOWER(value) FROM json_each(e.tags))", len(e.args)+1))
			} else {
				e.conditions = append(e.conditions, fmt.Sprintf("LOWER($%d) = ANY(LOWER(e.tags::text)::text[])", len(e.args)+1))
			}
			e.args = append(e.args, cat)
		}
	}
	return e
}

// WithoutStatus set the entry status that should not be returned.
func (e *EntryQueryBuilder) WithoutStatus(status string) *EntryQueryBuilder {
	if status != "" {
		e.conditions = append(e.conditions, "e.status <> $"+strconv.Itoa(len(e.args)+1))
		e.args = append(e.args, status)
	}
	return e
}

// WithShareCode set the entry share code.
func (e *EntryQueryBuilder) WithShareCode(shareCode string) *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.share_code = $"+strconv.Itoa(len(e.args)+1))
	e.args = append(e.args, shareCode)
	return e
}

// WithShareCodeNotEmpty adds a filter for non-empty share code.
func (e *EntryQueryBuilder) WithShareCodeNotEmpty() *EntryQueryBuilder {
	e.conditions = append(e.conditions, "e.share_code <> ''")
	return e
}

// WithSorting add a sort expression.
func (e *EntryQueryBuilder) WithSorting(column, direction string) *EntryQueryBuilder {
	switch {
	case strings.EqualFold(direction, "ASC"):
		e.sortExpressions = append(e.sortExpressions, pq.QuoteIdentifier(column)+" ASC")
	case strings.EqualFold(direction, "DESC"):
		e.sortExpressions = append(e.sortExpressions, pq.QuoteIdentifier(column)+" DESC")
	}

	return e
}

// WithLimit sets the limit. A non-positive limit is clamped to
// model.MaxEntryLimit so callers cannot request an unbounded result set.
func (e *EntryQueryBuilder) WithLimit(limit int) *EntryQueryBuilder {
	return e.WithLimitAndMaximum(limit, model.MaxEntryLimit)
}

// WithLimitAndMaximum sets the limit, capped at the given maximum.
// A non-positive limit is clamped to the maximum.
func (e *EntryQueryBuilder) WithLimitAndMaximum(limit, maximum int) *EntryQueryBuilder {
	if limit <= 0 || limit > maximum {
		limit = maximum
	}
	e.limit = limit
	return e
}

// WithOffset set the offset.
func (e *EntryQueryBuilder) WithOffset(offset int) *EntryQueryBuilder {
	if offset > 0 {
		e.offset = offset
	}
	return e
}

func (e *EntryQueryBuilder) WithGloballyVisible() *EntryQueryBuilder {
	e.conditions = append(e.conditions, "c.hide_globally IS FALSE")
	e.conditions = append(e.conditions, "f.hide_globally IS FALSE")
	return e
}

// CountEntries count the number of entries that match the condition.
func (e *EntryQueryBuilder) CountEntries() (count int, err error) {
	query := `
		SELECT count(*)
		FROM entries e
			JOIN feeds f ON f.id = e.feed_id
			JOIN categories c ON c.id = f.category_id
		WHERE ` + e.buildCondition()

	err = e.store.db.QueryRow(query, e.args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: unable to count entries: %v", err)
	}

	return count, nil
}

// GetEntry returns a single entry that match the condition.
func (e *EntryQueryBuilder) GetEntry() (*model.Entry, error) {
	e.limit = 1
	entries, err := e.GetEntries()
	if err != nil {
		return nil, err
	}

	if len(entries) != 1 {
		return nil, nil
	}

	entries[0].Enclosures, err = e.store.EnclosuresByEntryID(entries[0].ID)
	if err != nil {
		return nil, err
	}

	return entries[0], nil
}

// GetEntries returns a list of entries that match the condition.
func (e *EntryQueryBuilder) GetEntries() (model.Entries, error) {
	entries, _, err := e.fetchEntries(false)
	return entries, err
}

// GetEntriesWithCount returns a list of entries and the total count of matching
// rows, ignoring limit and offset. It uses a window function for non-empty pages
// and falls back to a separate count when the requested offset returns no rows.
func (e *EntryQueryBuilder) GetEntriesWithCount() (model.Entries, int, error) {
	entries, total, err := e.fetchEntries(true)
	if err != nil {
		return nil, 0, err
	}

	if len(entries) == 0 && e.offset > 0 {
		total, err = e.CountEntries()
		if err != nil {
			return nil, 0, err
		}
	}

	return entries, total, nil
}

// fetchEntries is the shared implementation for GetEntries and GetEntriesWithCount.
// When withCount is true, count(*) OVER() is included in the SELECT and the total
// count of matching rows is returned; otherwise the returned count is 0.
func (e *EntryQueryBuilder) fetchEntries(withCount bool) (model.Entries, int, error) {
	countColumn := ""
	if withCount {
		countColumn = e.store.dialect.WindowCountOver() + ","
	}

	timezoneExpr := e.store.dialect.TimezoneConvert("e.published_at", "u.timezone")
	contentCol := e.contentColumn()

	query := fmt.Sprintf(`
		SELECT
			%s
			e.id,
			e.user_id,
			e.feed_id,
			e.hash,
			%s,
			e.title,
			e.url,
			e.comments_url,
			e.author,
			e.share_code,
			%s,
			e.status,
			e.starred,
			e.reading_time,
			e.created_at,
			e.changed_at,
			e.tags,
			e.language,
			f.title as feed_title,
			f.feed_url,
			f.site_url,
			f.description,
			f.language,
			f.checked_at,
			f.category_id,
			c.title as category_title,
			c.hide_globally as category_hidden,
			f.scraper_rules,
			f.rewrite_rules,
			f.crawler,
			f.user_agent,
			f.cookie,
			f.hide_globally,
			f.no_media_player,
			f.webhook_url,
			fi.icon_id,
			i.external_id AS icon_external_id,
			u.timezone
		FROM
			entries e
		INNER JOIN
			feeds f ON f.id=e.feed_id
		INNER JOIN
			categories c ON c.id=f.category_id
		LEFT JOIN
			feed_icons fi ON fi.feed_id=f.id
		LEFT JOIN
			icons i ON i.id=fi.icon_id
		INNER JOIN
			users u ON u.id=e.user_id
		WHERE `, countColumn, timezoneExpr, contentCol) + e.buildCondition() + " " + e.buildSorting()

	rows, err := e.store.db.Query(query, e.args...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: unable to get entries: %v", err)
	}
	defer rows.Close()

	size := max(e.limit, 0)
	entries := make(model.Entries, 0, size)
	entryMap := make(map[int64]*model.Entry, size)
	entryIDs := make([]int64, 0, size)
	var totalCount int

	for rows.Next() {
		var iconID sql.NullInt64
		var externalIconID sql.NullString
		var tz string
		var tagsStr string

		entry := model.NewEntry()

		// For SQLite, scan datetime columns into interface{} instead of
		// *time.Time.  The modernc.org/sqlite v1.50.0 driver's built-in
		// parseTime() returns the raw string when it can't parse a timestamp
		// value, causing Scan("string" → *time.Time) to fail.
		// See the timeParseFormats doc above for full background.
		// Values are parsed with scanTime() after rows.Scan() succeeds.
		var dateVal, createdVal, changedVal, checkedVal any

		dest := []any{
			&entry.ID,
			&entry.UserID,
			&entry.FeedID,
			&entry.Hash,
			&entry.Date,
			&entry.Title,
			&entry.URL,
			&entry.CommentsURL,
			&entry.Author,
			&entry.ShareCode,
			&entry.Content,
			&entry.Status,
			&entry.Starred,
			&entry.ReadingTime,
			&entry.CreatedAt,
			&entry.ChangedAt,
			&tagsStr,
			&entry.Language,
			&entry.Feed.Title,
			&entry.Feed.FeedURL,
			&entry.Feed.SiteURL,
			&entry.Feed.Description,
			&entry.Feed.Language,
			&entry.Feed.CheckedAt,
			&entry.Feed.Category.ID,
			&entry.Feed.Category.Title,
			&entry.Feed.Category.HideGlobally,
			&entry.Feed.ScraperRules,
			&entry.Feed.RewriteRules,
			&entry.Feed.Crawler,
			&entry.Feed.UserAgent,
			&entry.Feed.Cookie,
			&entry.Feed.HideGlobally,
			&entry.Feed.NoMediaPlayer,
			&entry.Feed.WebhookURL,
			&iconID,
			&externalIconID,
			&tz,
		}

		if withCount {
			dest = append([]any{&totalCount}, dest...)
		}

		if e.store.dialect.DatabaseType() == dialect.SQLite {
			idx := 0
			if withCount {
				idx = 1
			}
			dest[4+idx] = &dateVal
			dest[14+idx] = &createdVal
			dest[15+idx] = &changedVal
			dest[21+idx] = &checkedVal
		}

		err := rows.Scan(dest...)
		if err != nil {
			return nil, 0, fmt.Errorf("store: unable to fetch entry row: %v", err)
		}

		if e.store.dialect.DatabaseType() == dialect.SQLite {
			if t, err := scanTime(dateVal); err == nil {
				entry.Date = t
			} else if dateVal != nil {
				slog.Warn("unable to parse published_at datetime",
					slog.Int64("entry_id", entry.ID),
					slog.String("value", fmt.Sprintf("%v", dateVal)),
					slog.Any("error", err),
				)
			}
			if t, err := scanTime(createdVal); err == nil {
				entry.CreatedAt = t
			} else if createdVal != nil {
				slog.Warn("unable to parse created_at datetime",
					slog.Int64("entry_id", entry.ID),
					slog.String("value", fmt.Sprintf("%v", createdVal)),
					slog.Any("error", err),
				)
			}
			if t, err := scanTime(changedVal); err == nil {
				entry.ChangedAt = t
			} else if changedVal != nil {
				slog.Warn("unable to parse changed_at datetime",
					slog.Int64("entry_id", entry.ID),
					slog.String("value", fmt.Sprintf("%v", changedVal)),
					slog.Any("error", err),
				)
			}
			if t, err := scanTime(checkedVal); err == nil {
				entry.Feed.CheckedAt = t
			} else if checkedVal != nil {
				slog.Warn("unable to parse feed checked_at datetime",
					slog.Int64("entry_id", entry.ID),
					slog.String("value", fmt.Sprintf("%v", checkedVal)),
					slog.Any("error", err),
				)
			}
		}

		if e.store.dialect.DatabaseType() == dialect.SQLite {
			json.Unmarshal([]byte(tagsStr), &entry.Tags)
		} else {
			entry.Tags = parsePostgresArray(tagsStr)
		}

		if iconID.Valid && externalIconID.Valid && externalIconID.String != "" {
			entry.Feed.Icon.FeedID = entry.FeedID
			entry.Feed.Icon.IconID = iconID.Int64
			entry.Feed.Icon.ExternalIconID = externalIconID.String
		} else {
			entry.Feed.Icon.IconID = 0
		}

		// Make sure that timestamp fields contain timezone information (API)
		entry.Date = timezone.Convert(tz, entry.Date)
		entry.CreatedAt = timezone.Convert(tz, entry.CreatedAt)
		entry.ChangedAt = timezone.Convert(tz, entry.ChangedAt)
		entry.Feed.CheckedAt = timezone.Convert(tz, entry.Feed.CheckedAt)

		entry.Feed.ID = entry.FeedID
		entry.Feed.UserID = entry.UserID
		entry.Feed.Icon.FeedID = entry.FeedID
		entry.Feed.Category.UserID = entry.UserID

		entries = append(entries, entry)
		entryMap[entry.ID] = entry
		entryIDs = append(entryIDs, entry.ID)
	}

	if e.fetchEnclosures && len(entryIDs) > 0 {
		enclosures, err := e.store.EnclosuresByEntryIDs(entryIDs)
		if err != nil {
			return nil, 0, fmt.Errorf("store: unable to fetch enclosures: %w", err)
		}

		for entryID, entryEnclosures := range enclosures {
			if entry, exists := entryMap[entryID]; exists {
				entry.Enclosures = entryEnclosures
			}
		}
	}

	return entries, totalCount, nil
}

// GetEntryIDs returns a list of entry IDs that match the condition.
func (e *EntryQueryBuilder) GetEntryIDs() ([]int64, error) {
	query := `
		SELECT
			e.id
		FROM
			entries e
		LEFT JOIN
			feeds f
		ON
			f.id=e.feed_id
		WHERE ` + e.buildCondition() + " " + e.buildSorting()

	rows, err := e.store.db.Query(query, e.args...)
	if err != nil {
		return nil, fmt.Errorf("store: unable to get entries: %v", err)
	}
	defer rows.Close()

	var entryIDs []int64
	for rows.Next() {
		var entryID int64
		if err := rows.Scan(&entryID); err != nil {
			return nil, fmt.Errorf("store: unable to fetch entry row: %v", err)
		}
		entryIDs = append(entryIDs, entryID)
	}

	return entryIDs, nil
}

// GetEntryIDsWithCount returns a list of entry IDs and the total count of
// matching rows (ignoring limit/offset). It uses two queries: one to count
// all matching rows and one to fetch the paginated IDs.
func (e *EntryQueryBuilder) GetEntryIDsWithCount() ([]int64, int, error) {
	total, err := e.CountEntries()
	if err != nil {
		return nil, 0, err
	}

	entryIDs, err := e.GetEntryIDs()
	if err != nil {
		return nil, 0, err
	}

	return entryIDs, total, nil
}

func (e *EntryQueryBuilder) contentColumn() string {
	if e.excludeContent {
		return "'' AS content"
	}
	return "e.content"
}

func (e *EntryQueryBuilder) buildCondition() string {
	return strings.Join(e.conditions, " AND ")
}

func (e *EntryQueryBuilder) buildSorting() string {
	var parts string

	if len(e.sortExpressions) > 0 {
		for i, expr := range e.sortExpressions {
			if !strings.ContainsAny(expr, ".(") {
				e.sortExpressions[i] = "e." + expr
			}
		}
		parts += " ORDER BY " + strings.Join(e.sortExpressions, ", ")
	}

	if e.limit > 0 {
		parts += " LIMIT " + strconv.Itoa(e.limit)
	}

	if e.offset > 0 {
		parts += " OFFSET " + strconv.Itoa(e.offset)
	}

	return parts
}

func parsePostgresArray(s string) []string {
	s = strings.TrimSpace(s)
	if s == "{}" {
		return nil
	}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
			p = p[1 : len(p)-1]
		}
		result[i] = p
	}
	return result
}

// NewEntryQueryBuilder returns a new EntryQueryBuilder.
func (s *Storage) NewEntryQueryBuilder(userID int64) *EntryQueryBuilder {
	return &EntryQueryBuilder{
		store:      s,
		args:       []any{userID},
		conditions: []string{"e.user_id = $1"},
	}
}

// NewAnonymousQueryBuilder returns a new EntryQueryBuilder suitable for anonymous users.
func (s *Storage) NewAnonymousQueryBuilder() *EntryQueryBuilder {
	return &EntryQueryBuilder{
		store: s,
	}
}
