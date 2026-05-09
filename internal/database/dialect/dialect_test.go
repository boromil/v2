// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dialect

import (
	"testing"
)

func TestPostgreSQLDialect(t *testing.T) {
	d := &PostgreSQLDialect{}

	if d.DatabaseType() != PostgreSQL {
		t.Errorf("Expected database type PostgreSQL, got %v", d.DatabaseType())
	}
	if d.DriverName() != "postgres" {
		t.Errorf("Expected driver name 'postgres', got '%s'", d.DriverName())
	}
}

func TestPostgreSQLDialectQueryBuilding(t *testing.T) {
	d := &PostgreSQLDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "FtsCondition",
			got:      d.FtsCondition(1),
			expected: "e.document_vectors @@ plainto_tsquery($1)",
		},
		{
			name:     "FtsRank",
			got:      d.FtsRank(1),
			expected: "ts_rank(document_vectors, plainto_tsquery($1))",
		},
		{
			name:     "BuildDocumentVectors",
			got:      d.BuildDocumentVectors(4, 5),
			expected: "setweight(to_tsvector($4), 'A') || setweight(to_tsvector($5), 'B')",
		},
		{
			name:     "ArrayLiteral",
			got:      d.ArrayLiteral([]string{"'a'", "'b'"}),
			expected: "ARRAY['a', 'b']",
		},
		{
			name:     "ArrayContains",
			got:      d.ArrayContains("tags", "test"),
			expected: "tags = ANY($1)",
		},
		{
			name:     "ArrayLength",
			got:      d.ArrayLength("tags"),
			expected: "array_length(tags, 1)",
		},
		{
			name:     "Upsert",
			got:      d.Upsert("users", []string{"name", "email"}, "id"),
			expected: "ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, email = EXCLUDED.email",
		},
		{
			name:     "Returning",
			got:      d.Returning("id", "name"),
			expected: "RETURNING id, name",
		},
		{
			name:     "ForUpdateSkipLocked",
			got:      d.ForUpdateSkipLocked(),
			expected: "FOR UPDATE SKIP LOCKED",
		},
		{
			name:     "Now",
			got:      d.Now(),
			expected: "now()",
		},
		{
			name:     "NowAddInterval",
			got:      d.NowAddInterval("1 week"),
			expected: "now() + interval '1 week'",
		},
		{
			name:     "NowSubtractInterval",
			got:      d.NowSubtractInterval("$1"),
			expected: "NOW() - CAST($1 AS INTERVAL)",
		},
		{
			name:     "ILIKE",
			got:      d.ILIKE("name", "test"),
			expected: "LOWER(name) LIKE LOWER($1)",
		},
		{
			name:     "ExtractEpoch",
			got:      d.ExtractEpoch("created_at"),
			expected: "EXTRACT(epoch FROM created_at)",
		},
		{
			name:     "MD5",
			got:      d.MD5("url"),
			expected: "md5(url)",
		},
		{
			name:     "JSONTypeof",
			got:      d.JSONTypeof("state"),
			expected: "jsonb_typeof(state)",
		},
		{
			name:     "JSONExtract",
			got:      d.JSONExtract("state", "'key'"),
			expected: "state->>'key'",
		},
		{
			name:     "DateTrunc",
			got:      d.DateTrunc("day", "created_at"),
			expected: "date_trunc('day', created_at)",
		},
		{
			name:     "TimezoneConvert",
			got:      d.TimezoneConvert("created_at", "UTC"),
			expected: "created_at at time zone 'UTC'",
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

func TestPostgreSQLDialectTypes(t *testing.T) {
	d := &PostgreSQLDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "AutoIncrement",
			got:      d.AutoIncrement(),
			expected: "SERIAL",
		},
		{
			name:     "BigAutoIncrement",
			got:      d.BigAutoIncrement(),
			expected: "BIGSERIAL",
		},
		{
			name:     "TimestampType",
			got:      d.TimestampType(),
			expected: "timestamp with time zone",
		},
		{
			name:     "TimestampWithTimezone",
			got:      d.TimestampWithTimezone(),
			expected: "timestamp with time zone",
		},
		{
			name:     "ByteaType",
			got:      d.ByteaType(),
			expected: "bytea",
		},
		{
			name:     "TextArrayType",
			got:      d.TextArrayType(),
			expected: "text[]",
		},
		{
			name:     "JSONType",
			got:      d.JSONType(),
			expected: "jsonb",
		},
		{
			name:     "JSONBType",
			got:      d.JSONBType(),
			expected: "jsonb",
		},
		{
			name:     "InetType",
			got:      d.InetType(),
			expected: "inet",
		},
		{
			name:     "CastToBytea",
			got:      d.CastToBytea("data"),
			expected: "data::bytea",
		},
		{
			name:     "CastToTimestamp",
			got:      d.CastToTimestamp("created_at"),
			expected: "created_at::timestamp with time zone",
		},
		{
			name:     "CastToInterval",
			got:      d.CastToInterval("1 day"),
			expected: "interval '1 day'",
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

func TestPostgreSQLDialectSchema(t *testing.T) {
	d := &PostgreSQLDialect{}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "CurrentDatabaseSize",
			got:      d.CurrentDatabaseSize(),
			expected: "SELECT pg_size_pretty(pg_database_size(current_database()))",
		},
		{
			name:     "ServerVersion",
			got:      d.ServerVersion(),
			expected: "SELECT current_setting('server_version')",
		},
		{
			name:     "TableExists",
			got:      d.TableExists("users"),
			expected: "SELECT true FROM information_schema.tables WHERE table_name = 'users'",
		},
		{
			name:     "ColumnExists",
			got:      d.ColumnExists("users", "email"),
			expected: "SELECT true FROM information_schema.columns WHERE table_name = 'users' AND column_name = 'email'",
		},
		{
			name:     "IndexExists",
			got:      d.IndexExists("idx_users", "users"),
			expected: "SELECT true FROM pg_indexes WHERE indexname = 'idx_users' AND tablename = 'users'",
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

func TestPostgreSQLDialectEnumType(t *testing.T) {
	d := &PostgreSQLDialect{}
	got := d.EnumType("entry_status", []string{"unread", "read", "removed"})
	expected := "CREATE TYPE entry_status AS ENUM('unread', 'read', 'removed')"
	if got != expected {
		t.Errorf("EnumType: expected '%s', got '%s'", expected, got)
	}
}
