// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"miniflux.app/v2/internal/crypto"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
)

// csrfMiddleware validates CSRF tokens on state-changing requests.
// It mirrors the pattern from internal/ui/csrf_middleware.go.
type csrfMiddleware struct{}

func newCSRFMiddleware() *csrfMiddleware {
	return &csrfMiddleware{}
}

func (m *csrfMiddleware) handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods don't require CSRF validation.
		default:
			if !m.validate(w, r) {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (m *csrfMiddleware) validate(w http.ResponseWriter, r *http.Request) bool {
	session := request.WebSession(r)
	if session == nil {
		response.HTMLForbidden(w, r)
		return false
	}

	csrfToken := session.CSRF()
	formValue := r.FormValue("csrf")
	headerValue := r.Header.Get("X-Csrf-Token")

	if crypto.ConstantTimeCmp(csrfToken, formValue) || crypto.ConstantTimeCmp(csrfToken, headerValue) {
		return true
	}

	// Check Datastar JSON body for csrfToken signal.
	if r.Header.Get("Datastar-Request") != "" && r.Header.Get("Content-Type") == "application/json" {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			r.Body = io.NopCloser(bytes.NewReader(body)) // restore body
			var signals struct {
				CsrfToken string `json:"csrfToken"`
			}
			if json.Unmarshal(body, &signals) == nil && crypto.ConstantTimeCmp(csrfToken, signals.CsrfToken) {
				return true
			}
		}
	}

	slog.Warn("dsui: invalid or missing CSRF token",
		slog.String("url", r.RequestURI),
	)
	response.HTMLBadRequest(w, r, errors.New("invalid or missing CSRF token"))
	return false
}

// secureHeadersMiddleware adds security-related HTTP headers to all responses.
func secureHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
