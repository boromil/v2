// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"

	"log/slog"
)

// ─── Session middleware ──────────────────────────────────────────────────

func sessionMiddleware(store *storage.Storage) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip session for static assets.
			if isDsStaticRoute(r) {
				next.ServeHTTP(w, r)
				return
			}

			session, secret := loadOrCreateSession(r, store)
			if session == nil {
				response.HTMLServerError(w, r, fmt.Errorf("unable to create session"))
				return
			}
			if secret != "" {
				setSessionCookie(w, session, secret)
			}

			ctx := context.WithValue(r.Context(), request.WebSessionContextKey, session)
			r = r.WithContext(ctx)

			if !request.IsAuthenticated(r) && !isDsPublicRoute(r) {
				response.HTMLRedirect(w, r, dsLoginRedirectURL(r.RequestURI))
				return
			}

			next.ServeHTTP(w, r)

			if session.IsDirty() {
				if err := store.UpdateWebSession(session); err != nil {
					slog.Error("dsui: unable to persist session",
						slog.String("session_id", session.ID),
						slog.Any("error", err),
					)
				}
			}
		})
	}
}

func loadOrCreateSession(r *http.Request, store *storage.Storage) (*model.WebSession, string) {
	cookieValue := request.CookieValue(r, sessionCookieName)
	if cookieValue != "" {
		sessionID, secret, ok := strings.Cut(cookieValue, ".")
		if ok && sessionID != "" && secret != "" {
			session, err := store.WebSessionByID(sessionID)
			if err != nil {
				return nil, ""
			}
			if session != nil && session.VerifySecret(secret) {
				return session, ""
			}
		}
	}

	// Create new session.
	session, secret := model.NewWebSession(r.UserAgent(), request.ClientIP(r))
	if err := store.CreateWebSession(session); err != nil {
		return nil, ""
	}
	return session, secret
}

func setSessionCookie(w http.ResponseWriter, session *model.WebSession, secret string) {
	path := config.Opts.BasePath()
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    session.ID + "." + secret,
		Path:     path,
		Secure:   config.Opts.HTTPS(),
		HttpOnly: true,
		Expires:  time.Now().Add(config.Opts.CleanupRemoveSessionsInterval()),
		SameSite: http.SameSiteLaxMode,
	})
}

func isDsStaticRoute(r *http.Request) bool {
	path := r.URL.Path
	return strings.HasPrefix(path, "/ds/stylesheets/") ||
		strings.HasPrefix(path, "/ds/js/") ||
		path == "/ds/favicon.ico"
}

func isDsPublicRoute(r *http.Request) bool {
	if isDsStaticRoute(r) {
		return true
	}
	// The index route just redirects to /ds/unread.
	if r.URL.Path == "/ds/" || r.URL.Path == "/ds" {
		return true
	}
	return false
}

func dsLoginRedirectURL(requestURI string) string {
	loginURL, _ := url.Parse("/")
	values := loginURL.Query()
	values.Set("redirect_url", requestURI)
	loginURL.RawQuery = values.Encode()
	return loginURL.String()
}
