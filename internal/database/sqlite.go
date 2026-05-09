// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package database // import "miniflux.app/v2/internal/database"

import (
	"crypto/md5"
	"database/sql/driver"
	"encoding/hex"
	"sync"

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
