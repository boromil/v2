// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"crypto/md5"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"sync"
	"time"

	"miniflux.app/v2/internal/database/dialect"

	_ "github.com/lib/pq"
	"modernc.org/sqlite"
)

var registerOnce sync.Once

func registerSQLiteMD5() {
	registerOnce.Do(func() {
		sqlite.MustRegisterFunction("md5", &sqlite.FunctionImpl{
			NArgs:         1,
			Deterministic: true,
			Scalar: func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				if len(args) < 1 {
					return nil, nil
				}
				input, ok := args[0].(string)
				if !ok {
					return nil, nil
				}
				h := md5.Sum([]byte(input))
				return hex.EncodeToString(h[:]), nil
			},
		})
	})
}

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
