// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miniflux.app/v2/internal/http/request"
)

// withTestAuth mirrors the context setup done by api.validateBasicAuth /
// validateAPIKeyAuth (see internal/api/middleware.go).
func withTestAuth(ctx context.Context, userID int64) context.Context {
	ctx = context.WithValue(ctx, request.UserIDContextKey, userID)
	ctx = context.WithValue(ctx, request.IsAuthenticatedContextKey, true)
	return ctx
}

func TestServeHTTP(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		method     string
		body       string
		auth       bool
		wantStatus int
		// wantErrCode, when non-nil, asserts the JSON-RPC error.code field.
		wantErrCode *int
		// wantNoError asserts that result.error is nil (when true).
		wantNoError bool
	}

	tests := []testCase{
		{
			name:       "method not allowed",
			method:     http.MethodPut,
			body:       `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			auth:       true,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:        "unauthorized",
			method:      http.MethodPost,
			body:        `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			auth:        false,
			wantStatus:  http.StatusUnauthorized,
			wantErrCode: ptr(errCodeUnauthorized),
		},
		{
			name:        "parse error",
			method:      http.MethodPost,
			body:        `not json`,
			auth:        true,
			wantStatus:  http.StatusOK,
			wantErrCode: ptr(errCodeParseError),
		},
		{
			name:        "unknown method",
			method:      http.MethodPost,
			body:        `{"jsonrpc":"2.0","method":"nope","id":7}`,
			auth:        true,
			wantStatus:  http.StatusOK,
			wantErrCode: ptr(errCodeMethodNotFound),
		},
		{
			name:        "tools list success",
			method:      http.MethodPost,
			body:        `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			auth:        true,
			wantStatus:  http.StatusOK,
			wantNoError: true,
		},
		{
			name:        "tools call unknown tool",
			method:      http.MethodPost,
			body:        `{"jsonrpc":"2.0","method":"tools/call","id":2,"params":{"name":"nope"}}`,
			auth:        true,
			wantStatus:  http.StatusOK,
			wantErrCode: ptr(errCodeMethodNotFound),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewMCPHandler(nil) // store not needed for these transport tests
			req := httptest.NewRequest(tt.method, "/v1/mcp", bytes.NewReader([]byte(tt.body)))
			if tt.auth {
				req = req.WithContext(withTestAuth(req.Context(), 1))
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			// For HTTP-level errors (401, 405) the body is plain text — skip JSON-RPC parsing.
			if tt.wantStatus == http.StatusMethodNotAllowed {
				return
			}

			var resp mcpResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())

			switch {
			case tt.wantErrCode != nil:
				require.NotNil(t, resp.Error, "body: %s", w.Body.String())
				assert.Equal(t, *tt.wantErrCode, resp.Error.Code)
			case tt.wantNoError:
				assert.Nil(t, resp.Error)
				assert.NotNil(t, resp.Result)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
