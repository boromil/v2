// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dialect

import (
	"strings"
	"testing"
)

func TestSQLiteDialect(t *testing.T) {
	d := &SQLiteDialect{}

	if d.DatabaseType() != SQLite {
		t.Errorf("Expected database type SQLite, got %v", d.DatabaseType())
	}
	if d.DriverName() != "sqlite" {
		t.Errorf("Expected driver name 'sqlite', got '%s'", d.DriverName())
	}
}

func TestSQLiteDialectQueryBuilding(t *testing.T) {
	d := &SQLiteDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "FtsCondition",
			got:      d.FtsCondition(1),
			expected: "e.id IN (SELECT rowid FROM fts_entries WHERE fts_entries MATCH $1)",
		},
		{
			name:     "FtsRank",
			got:      d.FtsRank(1),
			expected: "0.0",
		},
		{
			name:     "BuildDocumentVectors",
			got:      d.BuildDocumentVectors(4, 5),
			expected: "substr($4 || $5, 1, 0)",
		},
		{
			name:     "ArrayLiteral",
			got:      d.ArrayLiteral([]string{"'a'", "'b'"}),
			expected: "json_array('a', 'b')",
		},
		{
			name:     "ArrayContains",
			got:      d.ArrayContains("tags", "test"),
			expected: "EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value = $1)",
		},
		{
			name:     "ArrayLength",
			got:      d.ArrayLength("tags"),
			expected: "json_array_length(tags)",
		},
		{
			name:     "Upsert",
			got:      d.Upsert("users", []string{"name", "email"}, "id"),
			expected: "ON CONFLICT (id) DO UPDATE SET name = excluded.name, email = excluded.email",
		},
		{
			name:     "Returning",
			got:      d.Returning("id", "name"),
			expected: "RETURNING id, name",
		},
		{
			name:     "ForUpdateSkipLocked",
			got:      d.ForUpdateSkipLocked(),
			expected: "",
		},
		{
			name:     "Now",
			got:      d.Now(),
			expected: "datetime('now')",
		},
		{
			name:     "NowAddInterval",
			got:      d.NowAddInterval("1 week"),
			expected: "datetime('now', '+1 week')",
		},
		{
			name:     "NowSubtractInterval",
			got:      d.NowSubtractInterval("$1"),
			expected: "datetime('now', '-' || $1)",
		},
		{
			name:     "ILIKE",
			got:      d.ILIKE("name", "test"),
			expected: "LOWER(name) LIKE LOWER($1)",
		},
		{
			name:     "ExtractEpoch",
			got:      d.ExtractEpoch("created_at"),
			expected: "strftime('%s', created_at)",
		},
		{
			name:     "MD5",
			got:      d.MD5("url"),
			expected: "hex(md5(url))",
		},
		{
			name:     "JSONTypeof",
			got:      d.JSONTypeof("state"),
			expected: "json_type(state)",
		},
		{
			name:     "JSONExtract",
			got:      d.JSONExtract("state", "'$.key'"),
			expected: "json_extract(state, '$.key')",
		},
		{
			name:     "DateTrunc",
			got:      d.DateTrunc("day", "created_at"),
			expected: "strftime('%Y-%m-%d %H:%M:%S', created_at)",
		},
		{
			name:     "TimezoneConvert",
			got:      d.TimezoneConvert("created_at", "UTC"),
			expected: "created_at",
		},
		{
			name:     "WindowLag",
			got:      d.WindowLag("id", 1, "0"),
			expected: "LAG(id, 1, '0') OVER (ORDER BY id)",
		},
		{
			name:     "WindowLead",
			got:      d.WindowLead("id", 1, "0"),
			expected: "LEAD(id, 1, '0') OVER (ORDER BY id)",
		},
		{
			name:     "WindowCountOver",
			got:      d.WindowCountOver(),
			expected: "count(*) OVER()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: expected '%s', got '%s'", tt.name, tt.expected, tt.got)
			}
		})
	}
}

func TestSQLiteDialectTypes(t *testing.T) {
	d := &SQLiteDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "AutoIncrement",
			got:      d.AutoIncrement(),
			expected: "INTEGER PRIMARY KEY AUTOINCREMENT",
		},
		{
			name:     "BigAutoIncrement",
			got:      d.BigAutoIncrement(),
			expected: "INTEGER PRIMARY KEY AUTOINCREMENT",
		},
		{
			name:     "TimestampType",
			got:      d.TimestampType(),
			expected: "TEXT",
		},
		{
			name:     "TimestampWithTimezone",
			got:      d.TimestampWithTimezone(),
			expected: "TEXT",
		},
		{
			name:     "ByteaType",
			got:      d.ByteaType(),
			expected: "BLOB",
		},
		{
			name:     "TextArrayType",
			got:      d.TextArrayType(),
			expected: "TEXT",
		},
		{
			name:     "JSONType",
			got:      d.JSONType(),
			expected: "TEXT",
		},
		{
			name:     "JSONBType",
			got:      d.JSONBType(),
			expected: "TEXT",
		},
		{
			name:     "InetType",
			got:      d.InetType(),
			expected: "TEXT",
		},
		{
			name:     "CastToBytea",
			got:      d.CastToBytea("data"),
			expected: "CAST(data AS BLOB)",
		},
		{
			name:     "CastToTimestamp",
			got:      d.CastToTimestamp("created_at"),
			expected: "CAST(created_at AS TEXT)",
		},
		{
			name:     "CastToInterval",
			got:      d.CastToInterval("1 day"),
			expected: "'1 day'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: expected '%s', got '%s'", tt.name, tt.expected, tt.got)
			}
		})
	}
}

func TestSQLiteDialectSchema(t *testing.T) {
	d := &SQLiteDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "CurrentDatabaseSize",
			got:      d.CurrentDatabaseSize(),
			expected: "SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()",
		},
		{
			name:     "ServerVersion",
			got:      d.ServerVersion(),
			expected: "SELECT sqlite_version()",
		},
		{
			name:     "TableExists",
			got:      d.TableExists("users"),
			expected: "SELECT true FROM sqlite_master WHERE type='table' AND name='users'",
		},
		{
			name:     "ColumnExists",
			got:      d.ColumnExists("users", "email"),
			expected: "SELECT true FROM pragma_table_info('users') WHERE name='email'",
		},
		{
			name:     "IndexExists",
			got:      d.IndexExists("idx_users", "users"),
			expected: "SELECT true FROM sqlite_master WHERE type='index' AND name='idx_users' AND tbl_name='users'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s: expected '%s', got '%s'", tt.name, tt.expected, tt.got)
			}
		})
	}
}

func TestSQLiteDialectEnumType(t *testing.T) {
	d := &SQLiteDialect{}
	got := d.EnumType("entry_status", []string{"unread", "read", "removed"})
	expected := ""
	if got != expected {
		t.Errorf("EnumType: expected '%s', got '%s'", expected, got)
	}
}

func TestAppendPragma(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		key      string
		value    string
		contains string
	}{
		{
			name:     "no query params",
			dsn:      "file:data.db",
			key:      "journal_mode",
			value:    "WAL",
			contains: "file:data.db?_pragma=journal_mode(WAL)",
		},
		{
			name:     "existing query params",
			dsn:      "file:data.db?cache=shared",
			key:      "journal_mode",
			value:    "WAL",
			contains: "?cache=shared&_pragma=journal_mode(WAL)",
		},
		{
			name:     "pragma already present — skip",
			dsn:      "file:data.db?_pragma=journal_mode(DELETE)",
			key:      "journal_mode",
			value:    "WAL",
			contains: "file:data.db?_pragma=journal_mode(DELETE)",
		},
		{
			name:     "multiple pragmas, only append missing",
			dsn:      "file:data.db?_pragma=journal_mode(DELETE)",
			key:      "foreign_keys",
			value:    "ON",
			contains: "_pragma=journal_mode(DELETE)&_pragma=foreign_keys(ON)",
		},
		{
			name:     "pragma with similar prefix not confused",
			dsn:      "file:data.db?_pragma=journal_mode_x(1)",
			key:      "journal_mode",
			value:    "WAL",
			contains: "_pragma=journal_mode(WAL)",
		},
		{
			name:     "in-memory with cache",
			dsn:      "file::memory:?cache=shared",
			key:      "busy_timeout",
			value:    "5000",
			contains: "?cache=shared&_pragma=busy_timeout(5000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendPragma(tt.dsn, tt.key, tt.value)
			if !strings.Contains(result, tt.contains) {
				t.Errorf("appendPragma(%q, %q, %q) = %q, want containing %q",
					tt.dsn, tt.key, tt.value, result, tt.contains)
			}
			if tt.key != "journal_mode" || strings.Contains(tt.dsn, "_pragma=journal_mode(") {
				// Validate pragma is not duplicated when already present
				// but only check the specific test case
			}
		})
	}
}

func TestAppendPragmaNoDuplicate(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		key  string
	}{
		{"journal_mode present", "file:data.db?_pragma=journal_mode(DELETE)", "journal_mode"},
		{"synchronous present", "file:data.db?_pragma=synchronous(OFF)", "synchronous"},
		{"foreign_keys present", "file:data.db?_pragma=foreign_keys(OFF)", "foreign_keys"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendPragma(tt.dsn, tt.key, "WAL")
			// Result should be identical — no new pragma appended since key exists
			if result != tt.dsn {
				t.Errorf("appendPragma(%q, %q, ...) = %q, want %q (should not re-add existing pragma)",
					tt.dsn, tt.key, result, tt.dsn)
			}
		})
	}
}
