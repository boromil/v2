// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/ui/view"
)

// rateLimitedLogin returns a handler wrapper that rate-limits login requests
// to loginMaxAttempts per loginWindow per client IP. It returns 429 Too Many
// Requests with Retry-After header when the limit is exceeded.
//
// Design: the middleware performs a pre-check (isRateLimited) before the handler
// runs. The handler (checkLogin) records actual failures (recordFailedAttempt).
// This means only bad-credential attempts count toward the limit. Successful
// logins reset the counter. If recordFailedAttempt returns (true, duration),
// checkLogin short-circuits to a 429 response immediately.
func (h *handler) rateLimitedLogin(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := request.ClientIP(r)
		session := request.WebSession(r)
		language := session.Language()

		// Pre-check: reject immediately if already rate limited.
		if h.loginLimiter.isRateLimited(clientIP) {
			slog.Warn("Login rate limit exceeded",
				slog.String("client_ip", clientIP),
				slog.String("user_agent", r.UserAgent()),
			)

			retryAfter := h.loginLimiter.retryAfter(clientIP)
			h.renderRateLimitResponse(w, r, language, retryAfter)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// renderRateLimitResponse renders the login page with the rate-limit error and
// returns HTTP 429 Too Many Requests with a Retry-After header.
//
// retryAfter is the raw remaining window duration; a +1s ceiling is applied
// here so a window with <1s left still tells the client to wait at least 1s
// (RFC 7231 §7.1.3 requires a non-negative integer).
func (h *handler) renderRateLimitResponse(w http.ResponseWriter, r *http.Request, language string, retryAfter time.Duration) {
	v := view.New(h.tpl, r)
	errMsg := locale.NewLocalizedError("error.too_many_login_attempts").Translate(language)
	v.Set("errorMessage", errMsg)
	redirectURL := request.QueryStringParam(r, "redirect_url", "")
	v.Set("redirectURL", redirectURL)

	response.NewBuilder(w, r).
		WithStatus(loginRateLimitCode).
		WithHeader("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1)).
		WithHeader("Content-Type", "text/html; charset=utf-8").
		WithHeader("Cache-Control", "no-store").
		WithBodyAsBytes(v.Render("login")).
		WithoutCompression().
		Write()
}
