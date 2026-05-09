// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dialect // import "miniflux.app/v2/internal/database/dialect"

import (
	"database/sql"
	"fmt"
	"strings"
)

// DatabaseType represents the supported database backends.
type DatabaseType int

const (
	PostgreSQL DatabaseType = iota
	SQLite
)

// Dialect defines the interface for database-specific operations.
type Dialect interface {
	// Connection
	Open(dsn string) (*sql.DB, error)

	// Query building
	FtsCondition(n int) string
	FtsRank(n int) string
	BuildDocumentVectors(titlePH, contentPH int) string
	InClause(column string, n int) string
	ArrayLiteral(values []string) string
	ArrayContains(column, value string) string
	ArrayLength(column string) string
	Upsert(table string, columns []string, conflictKey string) string
	Returning(columns ...string) string
	ForUpdateSkipLocked() string
	Now() string
	// NowAddInterval is available for future use when implementing
	// date-based filtering with positive intervals.
	NowAddInterval(interval string) string
	// NowSubtractInterval accepts either a bind parameter ($1) or a
	// literal interval string ('7 days'), depending on the call site.
	NowSubtractInterval(literalOrPlaceholder string) string
	ILIKE(column, value string) string
	ExtractEpoch(column string) string
	MD5(column string) string
	JSONTypeof(column string) string
	JSONExtract(column, path string) string
	DateTrunc(unit, column string) string
	TimezoneConvert(column, tz string) string
	WindowLag(column string, offset int, defaultValue string) string
	WindowLead(column string, offset int, defaultValue string) string
	WindowCountOver() string

	// Types
	AutoIncrement() string
	BigAutoIncrement() string
	TimestampType() string
	TimestampWithTimezone() string
	ByteaType() string
	TextArrayType() string
	JSONType() string
	JSONBType() string
	InetType() string
	EnumType(name string, values []string) string
	CastToBytea(column string) string
	CastToTimestamp(column string) string
	CastToInterval(value string) string

	// Schema introspection
	CurrentDatabaseSize() string
	ServerVersion() string
	TableExists(tableName string) string
	ColumnExists(tableName, columnName string) string
	IndexExists(indexName, tableName string) string

	// Migration helpers
	DatabaseType() DatabaseType
	DriverName() string
}

// PostgreSQLDialect implements Dialect for PostgreSQL.
type PostgreSQLDialect struct{}

// Open opens a PostgreSQL database connection.
func (d *PostgreSQLDialect) Open(dsn string) (*sql.DB, error) {
	return sql.Open("postgres", dsn)
}

// FtsCondition returns the PostgreSQL full-text search condition.
func (d *PostgreSQLDialect) FtsCondition(n int) string {
	return fmt.Sprintf("e.document_vectors @@ plainto_tsquery($%d)", n)
}

// FtsRank returns the PostgreSQL full-text rank expression.
func (d *PostgreSQLDialect) FtsRank(n int) string {
	return fmt.Sprintf("ts_rank(document_vectors, plainto_tsquery($%d))", n)
}

// BuildDocumentVectors returns the PostgreSQL tsvector expression for updates.
func (d *PostgreSQLDialect) BuildDocumentVectors(titlePH, contentPH int) string {
	return fmt.Sprintf("setweight(to_tsvector($%d), 'A') || setweight(to_tsvector($%d), 'B')", titlePH, contentPH)
}

// InClause returns a PostgreSQL list membership clause.
func (d *PostgreSQLDialect) InClause(column string, n int) string {
	return fmt.Sprintf("%s = ANY($%d)", column, n)
}

// ArrayLiteral returns a PostgreSQL array literal.
func (d *PostgreSQLDialect) ArrayLiteral(values []string) string {
	return fmt.Sprintf("ARRAY[%s]", strings.Join(values, ", "))
}

// ArrayContains returns the PostgreSQL array contains condition.
func (d *PostgreSQLDialect) ArrayContains(column, value string) string {
	return fmt.Sprintf("%s = ANY($1)", column)
}

// ArrayLength returns the PostgreSQL array length expression.
func (d *PostgreSQLDialect) ArrayLength(column string) string {
	return fmt.Sprintf("array_length(%s, 1)", column)
}

// Upsert returns the PostgreSQL upsert clause.
func (d *PostgreSQLDialect) Upsert(table string, columns []string, conflictKey string) string {
	setClauses := make([]string, 0, len(columns))
	for _, col := range columns {
		setClauses = append(setClauses, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
	}
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", conflictKey, strings.Join(setClauses, ", "))
}

// Returning returns the PostgreSQL RETURNING clause.
func (d *PostgreSQLDialect) Returning(columns ...string) string {
	return fmt.Sprintf("RETURNING %s", strings.Join(columns, ", "))
}

// ForUpdateSkipLocked returns the PostgreSQL row locking clause.
func (d *PostgreSQLDialect) ForUpdateSkipLocked() string {
	return "FOR UPDATE SKIP LOCKED"
}

// Now returns the PostgreSQL current timestamp function.
func (d *PostgreSQLDialect) Now() string {
	return "now()"
}

// NowAddInterval returns the PostgreSQL timestamp with interval addition.
func (d *PostgreSQLDialect) NowAddInterval(interval string) string {
	return fmt.Sprintf("now() + interval '%s'", interval)
}

// NowSubtractInterval returns the PostgreSQL NOW() minus an interval.
func (d *PostgreSQLDialect) NowSubtractInterval(placeholder string) string {
	return fmt.Sprintf("NOW() - CAST(%s AS INTERVAL)", placeholder)
}

// ILIKE returns the PostgreSQL case-insensitive LIKE condition.
func (d *PostgreSQLDialect) ILIKE(column, value string) string {
	return fmt.Sprintf("LOWER(%s) LIKE LOWER($1)", column)
}

// ExtractEpoch returns the PostgreSQL epoch extraction expression.
func (d *PostgreSQLDialect) ExtractEpoch(column string) string {
	return fmt.Sprintf("EXTRACT(epoch FROM %s)", column)
}

// MD5 returns the PostgreSQL MD5 function call.
func (d *PostgreSQLDialect) MD5(column string) string {
	return fmt.Sprintf("md5(%s)", column)
}

// JSONTypeof returns the PostgreSQL JSON type check.
func (d *PostgreSQLDialect) JSONTypeof(column string) string {
	return fmt.Sprintf("jsonb_typeof(%s)", column)
}

// JSONExtract returns the PostgreSQL JSON extraction expression.
func (d *PostgreSQLDialect) JSONExtract(column, path string) string {
	return fmt.Sprintf("%s->>%s", column, path)
}

// DateTrunc returns the PostgreSQL date truncation expression.
func (d *PostgreSQLDialect) DateTrunc(unit, column string) string {
	return fmt.Sprintf("date_trunc('%s', %s)", unit, column)
}

// TimezoneConvert returns the PostgreSQL timezone conversion expression.
func (d *PostgreSQLDialect) TimezoneConvert(column, tz string) string {
	return fmt.Sprintf("%s at time zone '%s'", column, tz)
}

// WindowLag returns the PostgreSQL LAG window function.
func (d *PostgreSQLDialect) WindowLag(column string, offset int, defaultValue string) string {
	return fmt.Sprintf("LAG(%s, %d, '%s') OVER (ORDER BY %s)", column, offset, defaultValue, column)
}

// WindowLead returns the PostgreSQL LEAD window function.
func (d *PostgreSQLDialect) WindowLead(column string, offset int, defaultValue string) string {
	return fmt.Sprintf("LEAD(%s, %d, '%s') OVER (ORDER BY %s)", column, offset, defaultValue, column)
}

// WindowCountOver returns the PostgreSQL COUNT(*) OVER() window function.
func (d *PostgreSQLDialect) WindowCountOver() string {
	return "count(*) OVER()"
}

// AutoIncrement returns the PostgreSQL auto-increment type.
func (d *PostgreSQLDialect) AutoIncrement() string {
	return "SERIAL"
}

// BigAutoIncrement returns the PostgreSQL big auto-increment type.
func (d *PostgreSQLDialect) BigAutoIncrement() string {
	return "BIGSERIAL"
}

// TimestampType returns the PostgreSQL timestamp type.
func (d *PostgreSQLDialect) TimestampType() string {
	return "timestamp with time zone"
}

// TimestampWithTimezone returns the PostgreSQL timestamp with timezone type.
func (d *PostgreSQLDialect) TimestampWithTimezone() string {
	return "timestamp with time zone"
}

// ByteaType returns the PostgreSQL bytea type.
func (d *PostgreSQLDialect) ByteaType() string {
	return "bytea"
}

// TextArrayType returns the PostgreSQL text array type.
func (d *PostgreSQLDialect) TextArrayType() string {
	return "text[]"
}

// JSONType returns the PostgreSQL JSON type.
func (d *PostgreSQLDialect) JSONType() string {
	return "jsonb"
}

// JSONBType returns the PostgreSQL JSONB type.
func (d *PostgreSQLDialect) JSONBType() string {
	return "jsonb"
}

// InetType returns the PostgreSQL inet type.
func (d *PostgreSQLDialect) InetType() string {
	return "inet"
}

// EnumType returns the PostgreSQL enum type definition.
func (d *PostgreSQLDialect) EnumType(name string, values []string) string {
	quotedValues := make([]string, len(values))
	for i, v := range values {
		quotedValues[i] = fmt.Sprintf("'%s'", v)
	}
	return fmt.Sprintf("CREATE TYPE %s AS ENUM(%s)", name, strings.Join(quotedValues, ", "))
}

// CastToBytea returns the PostgreSQL bytea cast.
func (d *PostgreSQLDialect) CastToBytea(column string) string {
	return fmt.Sprintf("%s::bytea", column)
}

// CastToTimestamp returns the PostgreSQL timestamp cast.
func (d *PostgreSQLDialect) CastToTimestamp(column string) string {
	return fmt.Sprintf("%s::timestamp with time zone", column)
}

// CastToInterval returns the PostgreSQL interval cast.
func (d *PostgreSQLDialect) CastToInterval(value string) string {
	return fmt.Sprintf("interval '%s'", value)
}

// CurrentDatabaseSize returns the PostgreSQL database size query.
func (d *PostgreSQLDialect) CurrentDatabaseSize() string {
	return "SELECT pg_size_pretty(pg_database_size(current_database()))"
}

// ServerVersion returns the PostgreSQL server version query.
func (d *PostgreSQLDialect) ServerVersion() string {
	return "SELECT current_setting('server_version')"
}

// TableExists returns the PostgreSQL table existence check query.
func (d *PostgreSQLDialect) TableExists(tableName string) string {
	return fmt.Sprintf("SELECT true FROM information_schema.tables WHERE table_name = '%s'", tableName)
}

// ColumnExists returns the PostgreSQL column existence check query.
func (d *PostgreSQLDialect) ColumnExists(tableName, columnName string) string {
	return fmt.Sprintf("SELECT true FROM information_schema.columns WHERE table_name = '%s' AND column_name = '%s'", tableName, columnName)
}

// IndexExists returns the PostgreSQL index existence check query.
func (d *PostgreSQLDialect) IndexExists(indexName, tableName string) string {
	return fmt.Sprintf("SELECT true FROM pg_indexes WHERE indexname = '%s' AND tablename = '%s'", indexName, tableName)
}

// DatabaseType returns PostgreSQL.
func (d *PostgreSQLDialect) DatabaseType() DatabaseType {
	return PostgreSQL
}

// DriverName returns the PostgreSQL driver name.
func (d *PostgreSQLDialect) DriverName() string {
	return "postgres"
}

// SQLiteDialect implements Dialect for SQLite.
type SQLiteDialect struct{}

// Open opens a SQLite database connection and appends production PRAGMAs
// as DSN query parameters so they are applied on every connection open, not
// just the initial pool. This is necessary because database/sql silently
// recycles connections when SetConnMaxLifetime expires, and manually
// executed PRAGMAs only affect the connection they run on.
// PRAGMAs already present in the DSN are not re-appended to avoid overriding
// user-specified values.
func (d *SQLiteDialect) Open(dsn string) (*sql.DB, error) {
	dsn = appendPragma(dsn, "journal_mode", "WAL")
	dsn = appendPragma(dsn, "synchronous", "NORMAL")
	dsn = appendPragma(dsn, "foreign_keys", "ON")
	dsn = appendPragma(dsn, "busy_timeout", "5000")
	dsn = appendPragma(dsn, "cache_size", "-64000")
	dsn = appendPragma(dsn, "temp_store", "memory")
	dsn = appendPragma(dsn, "mmap_size", "268435456")
	dsn = appendPragma(dsn, "auto_vacuum", "INCREMENTAL")
	return sql.Open("sqlite", dsn)
}

func appendPragma(dsn, key, value string) string {
	pragma := fmt.Sprintf("_pragma=%s(%s)", key, value)
	if strings.Contains(dsn, "_pragma="+key+"(") {
		return dsn
	}
	if strings.Contains(dsn, "?") {
		return dsn + "&" + pragma
	}
	return dsn + "?" + pragma
}

// FtsCondition returns the SQLite FTS5 search condition.
func (d *SQLiteDialect) FtsCondition(n int) string {
	return fmt.Sprintf("e.id IN (SELECT rowid FROM fts_entries WHERE fts_entries MATCH $%d)", n)
}

// FtsRank returns the SQLite FTS5 rank expression.
func (d *SQLiteDialect) FtsRank(n int) string {
	return "0.0"
}

// BuildDocumentVectors consumes params but returns empty string for SQLite (FTS5 triggers handle syncing).
func (d *SQLiteDialect) BuildDocumentVectors(titlePH, contentPH int) string {
	return fmt.Sprintf("substr($%d || $%d, 1, 0)", titlePH, contentPH)
}

// InClause returns a SQLite list membership clause.
func (d *SQLiteDialect) InClause(column string, n int) string {
	return fmt.Sprintf("%s IN (SELECT value FROM json_each($%d))", column, n)
}

// ArrayLiteral returns a SQLite JSON array literal.
func (d *SQLiteDialect) ArrayLiteral(values []string) string {
	return fmt.Sprintf("json_array(%s)", strings.Join(values, ", "))
}

// ArrayContains returns the SQLite JSON array contains condition.
func (d *SQLiteDialect) ArrayContains(column, value string) string {
	return fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE json_each.value = $1)", column)
}

// ArrayLength returns the SQLite JSON array length expression.
func (d *SQLiteDialect) ArrayLength(column string) string {
	return fmt.Sprintf("json_array_length(%s)", column)
}

// Upsert returns the SQLite upsert clause.
func (d *SQLiteDialect) Upsert(table string, columns []string, conflictKey string) string {
	setClauses := make([]string, 0, len(columns))
	for _, col := range columns {
		setClauses = append(setClauses, fmt.Sprintf("%s = excluded.%s", col, col))
	}
	return fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", conflictKey, strings.Join(setClauses, ", "))
}

// Returning returns the SQLite RETURNING clause.
func (d *SQLiteDialect) Returning(columns ...string) string {
	return fmt.Sprintf("RETURNING %s", strings.Join(columns, ", "))
}

// ForUpdateSkipLocked returns empty string for SQLite (not supported).
func (d *SQLiteDialect) ForUpdateSkipLocked() string {
	return ""
}

// Now returns the SQLite current timestamp function.
func (d *SQLiteDialect) Now() string {
	return "datetime('now')"
}

// NowAddInterval returns the SQLite timestamp with interval addition.
func (d *SQLiteDialect) NowAddInterval(interval string) string {
	return fmt.Sprintf("datetime('now', '+%s')", interval)
}

// NowSubtractInterval returns the SQLite timestamp with interval subtraction.
func (d *SQLiteDialect) NowSubtractInterval(placeholder string) string {
	return fmt.Sprintf("datetime('now', '-' || %s)", placeholder)
}

// ILIKE returns the SQLite case-insensitive LIKE condition.
func (d *SQLiteDialect) ILIKE(column, value string) string {
	return fmt.Sprintf("LOWER(%s) LIKE LOWER($1)", column)
}

// ExtractEpoch returns the SQLite epoch extraction expression.
func (d *SQLiteDialect) ExtractEpoch(column string) string {
	return fmt.Sprintf("strftime('%%s', %s)", column)
}

// MD5 returns the SQLite MD5 function call (requires custom function).
func (d *SQLiteDialect) MD5(column string) string {
	return fmt.Sprintf("hex(md5(%s))", column)
}

// JSONTypeof returns the SQLite JSON type check.
func (d *SQLiteDialect) JSONTypeof(column string) string {
	return fmt.Sprintf("json_type(%s)", column)
}

// JSONExtract returns the SQLite JSON extraction expression.
func (d *SQLiteDialect) JSONExtract(column, path string) string {
	return fmt.Sprintf("json_extract(%s, %s)", column, path)
}

// DateTrunc returns the SQLite date truncation expression.
func (d *SQLiteDialect) DateTrunc(unit, column string) string {
	return fmt.Sprintf("strftime('%%Y-%%m-%%d %%H:%%M:%%S', %s)", column)
}

// TimezoneConvert returns the SQLite timezone conversion expression.
func (d *SQLiteDialect) TimezoneConvert(column, tz string) string {
	return column // SQLite doesn't have timezone support, store as UTC
}

// WindowLag returns the SQLite LAG window function.
func (d *SQLiteDialect) WindowLag(column string, offset int, defaultValue string) string {
	return fmt.Sprintf("LAG(%s, %d, '%s') OVER (ORDER BY %s)", column, offset, defaultValue, column)
}

// WindowLead returns the SQLite LEAD window function.
func (d *SQLiteDialect) WindowLead(column string, offset int, defaultValue string) string {
	return fmt.Sprintf("LEAD(%s, %d, '%s') OVER (ORDER BY %s)", column, offset, defaultValue, column)
}

// WindowCountOver returns the SQLite COUNT(*) OVER() window function.
func (d *SQLiteDialect) WindowCountOver() string {
	return "count(*) OVER()"
}

// AutoIncrement returns the SQLite auto-increment type.
func (d *SQLiteDialect) AutoIncrement() string {
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// BigAutoIncrement returns the SQLite big auto-increment type.
func (d *SQLiteDialect) BigAutoIncrement() string {
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// TimestampType returns the SQLite timestamp type.
func (d *SQLiteDialect) TimestampType() string {
	return "TEXT"
}

// TimestampWithTimezone returns the SQLite timestamp with timezone type.
func (d *SQLiteDialect) TimestampWithTimezone() string {
	return "TEXT"
}

// ByteaType returns the SQLite BLOB type.
func (d *SQLiteDialect) ByteaType() string {
	return "BLOB"
}

// TextArrayType returns the SQLite JSON array type.
func (d *SQLiteDialect) TextArrayType() string {
	return "TEXT"
}

// JSONType returns the SQLite JSON type.
func (d *SQLiteDialect) JSONType() string {
	return "TEXT"
}

// JSONBType returns the SQLite JSONB type.
func (d *SQLiteDialect) JSONBType() string {
	return "TEXT"
}

// InetType returns the SQLite inet type.
func (d *SQLiteDialect) InetType() string {
	return "TEXT"
}

// EnumType returns the SQLite enum type definition (CHECK constraint).
func (d *SQLiteDialect) EnumType(name string, values []string) string {
	return "" // SQLite uses CHECK constraints instead of enum types
}

// CastToBytea returns the SQLite BLOB cast.
func (d *SQLiteDialect) CastToBytea(column string) string {
	return fmt.Sprintf("CAST(%s AS BLOB)", column)
}

// CastToTimestamp returns the SQLite timestamp cast.
func (d *SQLiteDialect) CastToTimestamp(column string) string {
	return fmt.Sprintf("CAST(%s AS TEXT)", column)
}

// CastToInterval returns the SQLite interval cast.
func (d *SQLiteDialect) CastToInterval(value string) string {
	return fmt.Sprintf("'%s'", value)
}

// CurrentDatabaseSize returns the SQLite database size query.
func (d *SQLiteDialect) CurrentDatabaseSize() string {
	return "SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()"
}

// ServerVersion returns the SQLite server version query.
func (d *SQLiteDialect) ServerVersion() string {
	return "SELECT sqlite_version()"
}

// TableExists returns the SQLite table existence check query.
func (d *SQLiteDialect) TableExists(tableName string) string {
	return fmt.Sprintf("SELECT true FROM sqlite_master WHERE type='table' AND name='%s'", tableName)
}

// ColumnExists returns the SQLite column existence check query.
func (d *SQLiteDialect) ColumnExists(tableName, columnName string) string {
	return fmt.Sprintf("SELECT true FROM pragma_table_info('%s') WHERE name='%s'", tableName, columnName)
}

// IndexExists returns the SQLite index existence check query.
func (d *SQLiteDialect) IndexExists(indexName, tableName string) string {
	return fmt.Sprintf("SELECT true FROM sqlite_master WHERE type='index' AND name='%s' AND tbl_name='%s'", indexName, tableName)
}

// DatabaseType returns SQLite.
func (d *SQLiteDialect) DatabaseType() DatabaseType {
	return SQLite
}

// DriverName returns the SQLite driver name.
func (d *SQLiteDialect) DriverName() string {
	return "sqlite"
}
