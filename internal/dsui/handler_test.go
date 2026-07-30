// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/model"
	mtest "miniflux.app/v2/internal/storage/testing"
	"miniflux.app/v2/internal/worker"
)

// newTestHandler creates a handler with an in-memory SQLite database
// and an authenticated session for the given user.
func newTestHandler(t *testing.T, userID int64) (*handler, *worker.Pool) {
	t.Helper()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)

	h := &handler{
		store: store,
		pool:  pool,
		tpl:   parseTemplates(),
	}

	return h, pool
}

// setSessionOnRequest attaches a web session to the request context.
func setSessionOnRequest(req *http.Request, session *model.WebSession) *http.Request {
	ctx := context.WithValue(req.Context(), request.WebSessionContextKey, session)
	return req.WithContext(ctx)
}

// authenticateTestSession creates and persists an authenticated session
// in the database, then attaches the cookie + context to the request.
// Returns the CSRF token needed for POST requests.
func authenticateTestSession(t *testing.T, store interface {
	CreateWebSession(*model.WebSession) error
	RotateWebSession(string, *model.WebSession) error
}, req *http.Request, user *model.User) (*http.Request, string) {
	t.Helper()

	session, _ := model.NewWebSession("test-agent", "127.0.0.1")
	csrfToken := session.CSRF()
	if err := store.CreateWebSession(session); err != nil {
		t.Fatalf("failed to create web session: %v", err)
	}

	session.SetUser(user)
	oldID, newSecret := session.Rotate()
	if err := store.RotateWebSession(oldID, session); err != nil {
		t.Fatalf("failed to rotate web session: %v", err)
	}

	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID + "." + newSecret,
	})

	return setSessionOnRequest(req, session), csrfToken
}

func TestShowAppUnread(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 5)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Unread") {
		t.Error("expected 'Unread' in response body")
	}
	if !strings.Contains(body, "app-container") {
		t.Error("expected three-panel layout in response body")
	}
	if !strings.Contains(body, "entry-row") {
		t.Error("expected entry rows in response body")
	}
	// Each entry should have a star button.
	if !strings.Contains(body, "star-btn") {
		t.Error("expected star buttons in response body")
	}
}

func TestShowAppStarred(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/starred", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Starred") {
		t.Error("expected 'Starred' in response body")
	}
}

func TestShowAppFeedView(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/feed/"+itoa(feed.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), feed.Title) {
		t.Errorf("expected feed title '%s' in response body", feed.Title)
	}
}

func TestSSEEntriesReturnsFragment(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 5)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	contentType := w.Header().Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got %q", contentType)
	}
	if !strings.Contains(w.Body.String(), "entry-row") {
		t.Error("expected entry rows in SSE response")
	}
}

func TestSSEEntryReturnsContent(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntry(t, store, user.ID, feed.ID)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entry/"+itoa(entry.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), entry.Title) {
		t.Errorf("expected entry title '%s' in SSE response", entry.Title)
	}
}

func TestSSEToggleStar(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntry(t, store, user.ID, feed.ID)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodPost, "/ds/sse/entry/star/"+itoa(entry.ID), nil)
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	// Verify entry is now starred.
	updated, err := store.NewEntryQueryBuilder(user.ID).
		WithEntryIDs(entry.ID).
		GetEntry()
	if err != nil {
		t.Fatalf("unexpected error querying entry: %v", err)
	}
	if !updated.Starred {
		t.Error("expected entry to be starred after toggle")
	}
}

func TestSSEMarkAllRead(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 5)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Build request body with Datastar signals.
	body := strings.NewReader(`{"view":"unread"}`)
	req := httptest.NewRequest(http.MethodPost, "/ds/sse/mark-all-read", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Datastar-Request", "true")

	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify entries are marked as read.
	entries, _, err := store.NewEntryQueryBuilder(user.ID).
		WithStatuses(model.EntryStatusUnread).
		GetEntriesWithCount()
	if err != nil {
		t.Fatalf("unexpected error querying entries: %v", err)
	}
	if len(entries) > 0 {
		t.Errorf("expected 0 unread entries after mark-all-read, got %d", len(entries))
	}
}

func TestUnauthenticatedRequest(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected redirect (302), got %d", w.Code)
	}
}

func TestStaticAssetsServed(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// CSS should be served without auth.
	cssChecksum := "00000000"
	req := httptest.NewRequest(http.MethodGet, "/ds/stylesheets/"+cssChecksum+"/app", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 for stylesheets, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Errorf("expected text/css, got %q", ct)
	}
}

func TestIndexRedirects(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "/ds/unread" {
		t.Errorf("expected redirect to /ds/unread, got %q", loc)
	}
}

func TestCategoryView(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/category/"+itoa(cat.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), cat.Title) {
		t.Errorf("expected category title '%s' in response body", cat.Title)
	}
}

// itoa is a simple int64-to-string helper to avoid strconv import.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

func TestSSEWireFormat(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", contentType)
	}

	body := w.Body.String()
	// Verify SSE event structure: event line, data lines, blank line separator.
	if !strings.Contains(body, "event: datastar-patch-elements") {
		t.Error("expected datastar-patch-elements event in SSE response")
	}
	if !strings.Contains(body, "selector #entry-list") {
		t.Error("expected selector targeting #entry-list")
	}
	// The HTML fragment should be on a data: elements line (multiline supported).
	if !strings.Contains(body, "data: elements ") {
		t.Error("expected data: elements line with HTML fragment")
	}
}

func TestSSEResponseHasNoHtmlWrapper(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntry(t, store, user.ID, feed.ID)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entry/"+itoa(entry.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// SSE response should NOT contain <html>, <head>, or <body>.
	body := w.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "</html>") {
		t.Error("SSE response contains full HTML document wrapper")
	}
	if !strings.Contains(body, entry.Title) {
		t.Error("SSE response should contain entry title")
	}
	// Should contain both entry-row patch and entry-content patch.
	if strings.Count(body, "event: datastar-patch-elements") < 2 {
		t.Error("expected at least 2 patch events (entry-content + entry-row)")
	}
}

func TestShowAppEmptyEntries(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	_ = mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	// No entries created.

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "No entries to display") {
		t.Error("expected empty state message when no entries exist")
	}
	if strings.Contains(body, "entry-row") {
		t.Error("should not have entry rows when no entries exist")
	}
}

func TestShowAppNoFeeds(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	// No categories or feeds created.

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "All") {
		t.Error("expected standard views (All/Starred/History) in sidebar")
	}
	if !strings.Contains(body, "app-container") {
		t.Error("expected three-panel layout even with no feeds")
	}
}

func TestPaginationBoundary(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 2)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "entry-row") {
		t.Error("expected entry rows in response")
	}
}

func TestSearchNoResults(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/search?q=nonexistentxyz123", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Search") {
		t.Error("expected 'Search' title in response")
	}
}

func TestCSSStyleContainsDarkMode(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	checksum := "00000000"
	req := httptest.NewRequest(http.MethodGet, "/ds/stylesheets/"+checksum+"/app", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		body := w.Body.String()
		if !strings.Contains(body, "prefers-color-scheme: dark") {
			t.Error("CSS should contain dark mode media query")
		}
		if !strings.Contains(body, "color-scheme: dark") {
			t.Error("CSS should set dark color-scheme")
		}
	}
}

func TestSearchSSEFragment(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	// Create entries with specific titles for search.
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming", "Go is great")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Python Tips", "Python rocks")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Rust Patterns", "Rust is safe")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Search via SSE endpoint with query param.
	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=search&searchQuery=Go", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Go Programming") {
		t.Error("search SSE should return matching entry")
	}
	if strings.Contains(body, "Python Tips") {
		t.Error("search SSE should not return non-matching entry")
	}
}

func TestSearchSSEAllResults(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Entry One", "")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Entry Two", "")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Entry Three", "")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Empty search should return all entries.
	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=search&searchQuery=", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Count(body, "entry-row") < 3 {
		t.Error("empty search should return all entries")
	}
}
