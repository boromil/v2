// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"html"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database/dialect"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
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
	if !strings.Contains(body, "<title>Unread — Miniflux</title>") {
		t.Error("expected page title 'Unread — Miniflux' in response body")
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
	if !strings.Contains(body, "There are no unread entries") {
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

// TestPaginationBoundary
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

// TestPaginationKeyboardMarkers verifies the prev/next pagination links
// carry stable data-action markers that the ArrowLeft/ArrowRight keyboard
// shortcuts depend on.
func TestPaginationKeyboardMarkers(t *testing.T) {
	tpl := parseTemplates()

	pv := paginationView{
		CurrentPage:   2,
		TotalPages:    3,
		HasPrev:       true,
		HasNext:       true,
		PrevOffset:    0,
		NextOffset:    40,
		SSEEntriesURL: "/ds/sse/entries",
	}
	var buf strings.Builder
	if err := tpl.ExecuteTemplate(&buf, "pagination", pv); err != nil {
		t.Fatalf("executing pagination: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-action="prev-page"`) {
		t.Error("prev link must carry data-action=prev-page")
	}
	if !strings.Contains(out, `data-action="next-page"`) {
		t.Error("next link must carry data-action=next-page")
	}
	if !strings.Contains(out, "&offset=0") || !strings.Contains(out, "&offset=40") {
		t.Error("links must carry prev/next offsets")
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

// TestSearchSSEWithDatastarSignals reproduces EXACTLY what the browser sends
// when a Datastar search form submits via data-on:submit="@get(...)".
//
// Datastar merges the bound signals (data-bind:*) into a JSON payload and sends
// them as a percent-encoded "datastar" query parameter on GET requests. For a
// search term "Go", the browser sends:
//
//	GET /ds/sse/entries?view=search&datastar=%7B%22searchQuery%22%3A%22Go%22%7D
//
// The server-side storage.EntryRequest.SearchQuery (json:"searchQuery") is
// populated by datastar-go's ReadSignals from the "datastar" query param, so the
// handler filters results via builder.WithSearchQuery.
func TestSearchSSEWithDatastarSignals(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	// Create entries with distinct titles for search.
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming", "Go is great")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Python Tips", "Python rocks")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Rust Patterns", "Rust is safe")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Build the URL exactly as the Datastar client does: set the "datastar"
	// query param to the percent-encoded JSON signals object, appended onto the
	// form action URL.
	action := "/ds/sse/entries"
	actionURL, err := url.Parse(action)
	if err != nil {
		t.Fatalf("unexpected error parsing action URL: %v", err)
	}
	q := actionURL.Query()
	q.Set("view", "search")
	// This is the JSON payload of the current signals object (camelCase keys).
	q.Set("datastar", `{"searchQuery":"Go"}`)
	actionURL.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, actionURL.String(), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Go Programming") {
		t.Error("search via datastar signals should return matching entry")
	}
	if strings.Contains(body, "Python Tips") {
		t.Error("search via datastar signals should not return non-matching entry")
	}
	if strings.Contains(body, "Rust Patterns") {
		t.Error("search via datastar signals should not return non-matching entry")
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
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Count(body, "entry-row") < 3 {
		t.Error("empty search should return all entries")
	}
}

// TestSearchURLViewWinsOverSignal is a regression test for the bug where the
// search box's @get('/ds/sse/entries?view=search') was silently ignored.
//
// The page seeds the "view" signal to "unread" (layout.html data-signals).
// When the user types in the search box, Datastar fires the @get and sends ALL
// signals — including "view":"unread" — as the datastar query param. The URL
// has view=search. The handler MUST let the explicit URL param win, otherwise
// queryEntries runs with view=unread (ignoring the search term entirely) and
// search appears completely broken.
func TestSearchURLViewWinsOverSignal(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming", "Go is great")
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Python Tips", "Python rocks")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Exactly what the browser sends: URL view=search, but the datastar signal
	// payload carries view=unread (stale page state) + the typed searchQuery.
	action := "/ds/sse/entries"
	actionURL, _ := url.Parse(action)
	q := actionURL.Query()
	q.Set("view", "search")
	q.Set("datastar", `{"searchQuery":"Go","view":"unread"}`)
	actionURL.RawQuery = q.Encode()

	req := httptest.NewRequest(http.MethodGet, actionURL.String(), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Go Programming") {
		t.Error("URL view=search must win over signal view=unread; search term should filter results")
	}
	if strings.Contains(body, "Python Tips") {
		t.Error("non-matching entry should be excluded when searchQuery is applied")
	}
}

// TestSearchNoMatchShowsEmptyState verifies that a search with zero matches
// returns an empty entry list (not the previous list) and uses ElementPatchMode
// "inner" so the #entry-list container persists across patches.
// postSettings sends a form-encoded POST to /ds/sse/settings with the CSRF
// token in the "csrf" field (mirroring how the Datastar form adapter submits
// form data from the password form). It returns the recorder.
func postSettings(t *testing.T, store interface {
	CreateWebSession(*model.WebSession) error
	RotateWebSession(string, *model.WebSession) error
}, user *model.User, form map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	values := url.Values{}
	for k, v := range form {
		values.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, "/ds/sse/settings", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	pool := worker.NewPool(store.(*storage.Storage), 1)
	handler := Serve(store.(*storage.Storage), pool)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// TestSSESaveSettingsPasswordChange reproduces the password-form submit: the
// password and confirmation arrive as form-urlencoded fields (Datastar's
// contentType: 'form' adapter collects named inputs). It asserts the user's
// stored password is actually updated.
func TestSSESaveSettingsPasswordChange(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)

	// Sanity: the original test password works before the change.
	if err := store.CheckPassword(user.Username, "testpassword"); err != nil {
		t.Fatalf("expected default test password to be valid before update: %v", err)
	}

	w := postSettings(t, store, user, map[string]string{
		"password":     "NewPassword123",
		"confirmation": "NewPassword123",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The SSE response patches the importSuccess signal.
	if !strings.Contains(w.Body.String(), "importSuccess") {
		t.Errorf("expected importSuccess signal in SSE response, got: %s", w.Body.String())
	}

	// Old password must now fail.
	if err := store.CheckPassword(user.Username, "testpassword"); err == nil {
		t.Error("old password should no longer be valid after password change")
	}
	// New password must now succeed.
	if err := store.CheckPassword(user.Username, "NewPassword123"); err != nil {
		t.Errorf("new password should be valid after change: %v", err)
	}
}

// TestSSESaveSettingsPasswordMismatch verifies that a mismatched password and
// confirmation are rejected without persisting any change.
func TestSSESaveSettingsPasswordMismatch(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)

	w := postSettings(t, store, user, map[string]string{
		"password":     "NewPassword123",
		"confirmation": "DifferentPassword",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (SSE error patch), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "importError") {
		t.Errorf("expected importError signal for password mismatch, got: %s", w.Body.String())
	}

	// The original password should be untouched.
	if err := store.CheckPassword(user.Username, "testpassword"); err != nil {
		t.Errorf("original password should be unaffected after mismatch: %v", err)
	}
	if err := store.CheckPassword(user.Username, "NewPassword123"); err == nil {
		t.Error("mismatched new password must not be applied")
	}
}

// TestSSESaveSettingsStripContentBeforeFirstHeading verifies the new boolean
// setting round-trips: posting the checkbox form field persists it on the user,
// and omitting it turns it off.
func TestSSESaveSettingsStripContentBeforeFirstHeading(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)

	// Enable it.
	w := postSettings(t, store, user, map[string]string{"strip_content_before_first_heading": "1"})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved, err := store.UserByID(user.ID)
	if err != nil {
		t.Fatalf("fetch user: %v", err)
	}
	if !saved.StripContentBeforeFirstHeading {
		t.Error("expected StripContentBeforeFirstHeading to be enabled after save")
	}

	// Disable it by omitting the checkbox field.
	w = postSettings(t, store, user, map[string]string{"strip_content_before_first_heading": ""})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	saved, err = store.UserByID(user.ID)
	if err != nil {
		t.Fatalf("fetch user: %v", err)
	}
	if saved.StripContentBeforeFirstHeading {
		t.Error("expected StripContentBeforeFirstHeading to be disabled when checkbox omitted")
	}
}

func TestSearchNoMatchShowsEmptyState(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming", "Go is great")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Browser wire format: URL view=search, signal carries the no-match term
	// plus the stale view=unread.
	signals := `{"searchQuery":"czxczxc","view":"unread"}`
	u := "/ds/sse/entries?view=search&datastar=" + url.QueryEscape(signals)
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "Go Programming") {
		t.Error("no-match search must not return existing entries")
	}
	if !strings.Contains(body, "There are no results for this search") {
		t.Error("no-match search should render the empty state")
	}
	if !strings.Contains(body, "selector #entry-list") || !strings.Contains(body, "mode inner") {
		t.Error("entry-list patch must use mode inner so the container persists")
	}
}

// TestSearchUnreadOnlyFilter verifies that the searchUnreadOnly signal
// restricts search results to unread entries, like the classic UI filter.
func TestSearchUnreadOnlyFilter(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming Unread", "Go is great")
	readEntry := mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Go Programming Read", "Go is great")
	if err := store.SetEntriesStatus(user.ID, []int64{readEntry.ID}, model.EntryStatusRead); err != nil {
		t.Fatalf("marking entry read: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	signals := `{"searchQuery":"Go","searchUnreadOnly":true,"view":"unread"}`
	u := "/ds/sse/entries?view=search&datastar=" + url.QueryEscape(signals)
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Go Programming Unread") {
		t.Error("unread-only search must return the unread entry")
	}
	if strings.Contains(body, "Go Programming Read") {
		t.Error("unread-only search must not return the read entry")
	}
}

// TestArticleToolbarStatusToggleMarker verifies that the read-status (Read/Unread)
// toggle button in article_toolbar carries a stable data-action marker. The
// keyboard 'm' shortcut keys off this marker rather than the button's label,
// which alternates between "Read" and "Unread".
func TestArticleToolbarStatusToggleMarker(t *testing.T) {
	tpl := parseTemplates()

	printer := locale.NewPrinter("en_US")
	tpl, err := tpl.Clone()
	if err != nil {
		t.Fatalf("cloning template: %v", err)
	}
	tpl.Funcs(template.FuncMap{
		"t":      printer.Printf,
		"plural": printer.Plural,
	})

	for _, status := range []string{model.EntryStatusUnread, model.EntryStatusRead} {
		var buf strings.Builder
		data := map[string]any{
			"ID":      int64(42),
			"Starred": false,
			"URL":     "https://example.com/42",
			"Status":  status,
		}
		if err := tpl.ExecuteTemplate(&buf, "article_toolbar", data); err != nil {
			t.Fatalf("executing article_toolbar for %q: %v", status, err)
		}
		out := buf.String()

		if !strings.Contains(out, `data-action="toggle-status"`) {
			t.Errorf("status button outputs for %q must carry data-action=\"toggle-status\"\noutput:\n%s", status, out)
		}
		if !strings.Contains(out, `data-on:click="@post('/ds/sse/entry/status')"`) {
			t.Errorf("status button for %q must post to entry/status\noutput:\n%s", status, out)
		}
		// The button must still render its human-facing label (mark as read
		// for unread entries, mark as unread for read ones). The template
		// localizes labels, so compare against the default English catalog.
		want := printer.Print("entry.status.mark_as_read")
		if status == model.EntryStatusRead {
			want = printer.Print("entry.status.mark_as_unread")
		}
		if !strings.Contains(out, want) {
			t.Errorf("status button for %q must show label %q\noutput:\n%s", status, want, out)
		}
	}
}

// TestEntryRowStarButtonStopsPropagation verifies the star button in each entry
// row uses Datastar's __stop click modifier so a star click never bubbles up to
// the entry-row's @get (which would otherwise load the entry too).
func TestEntryRowStarButtonStopsPropagation(t *testing.T) {
	tpl := parseTemplates()

	var buf strings.Builder
	data := map[string]any{
		"ID":      int64(7),
		"Title":   "Some Entry",
		"Status":  model.EntryStatusUnread,
		"Starred": false,
		"Date":    time.Now(),
		"Feed":    &feedRef{Title: "Feed", ID: int64(1)},
	}
	if err := tpl.ExecuteTemplate(&buf, "entry_row", data); err != nil {
		t.Fatalf("executing entry_row: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `data-on:click__stop="@post('/ds/sse/entry/star/7')"`) {
		t.Errorf("star button must use data-on:click__stop to prevent bubbling to the row @get\noutput:\n%s", out)
	}
	if !strings.Contains(out, `data-on:click="@get('/ds/sse/entry/7')"`) {
		t.Errorf("entry row must keep its click-to-load action\noutput:\n%s", out)
	}
}

// TestKeyboardSelectionResetMarker verifies the rendered app page includes the
// #entry-list container that the keyboard.js selection-reset observer targets.
func TestKeyboardSelectionResetMarker(t *testing.T) {
	tpl := parseTemplates()

	// The "app" template is defined in app.html; a minimal data shape suffices
	// for checking the static structural markers.
	var buf strings.Builder
	data := appViewModel{
		ListTitle:       "Unread",
		Pagination:      nil,
		FeedID:          0,
		CanMarkAllRead:  false,
		CountErrorFeeds: 0,
		SearchQuery:     "",
	}
	if err := tpl.ExecuteTemplate(&buf, "app", data); err != nil {
		t.Fatalf("executing app template: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `id="entry-list"`) {
		t.Errorf("app template must render the #entry-list container for keyboard selection state\noutput:\n%s", out)
	}
	if !strings.Contains(out, `id="entry-content"`) {
		t.Errorf("app template must render the #entry-content container for the mobile observer\noutput:\n%s", out)
	}
	if !strings.Contains(out, `id="mobile-nav"`) {
		t.Errorf("app template must render the #mobile-nav panel switcher\noutput:\n%s", out)
	}
}

// TestSSEFetchContentUsesInnerMode verifies that sseFetchContent patches the
// #entry-content container with ElementPatchModeInner (so its id persists),
// matching sseEntry instead of the default outer mode.
func TestSSEFetchContentUsesInnerMode(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	// sseFetchContent scrapes the entry's URL, so point it at a local server
	// serving HTML content and allow private-network fetches in the test env.
	t.Setenv("FETCHER_ALLOW_PRIVATE_NETWORKS", "1")
	configParser := config.NewConfigParser()
	parsedOptions, err := configParser.ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("unable to configure test options: %v", err)
	}
	prevOpts := config.Opts
	config.Opts = parsedOptions
	t.Cleanup(func() { config.Opts = prevOpts })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Scraped Title</h1><p>Scraped body</p></body></html>`))
	}))
	t.Cleanup(srv.Close)

	entry := &model.Entry{
		UserID:  user.ID,
		FeedID:  feed.ID,
		Hash:    "hash_fetch_content_" + t.Name(),
		Title:   "Fetch Content",
		Content: "original body",
		URL:     srv.URL + "/article",
		Date:    time.Now(),
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert test entry: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodPost, "/ds/sse/fetch-content/"+itoa(entry.ID), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "selector #entry-content") || !strings.Contains(body, "mode inner") {
		t.Errorf("entry-content patch must use mode inner so the container persists, got: %s", body)
	}
}

// TestSSEMarkPageReadUsesInnerMode verifies that sseMarkPageRead patches the
// #entry-list container with ElementPatchModeInner so its id persists.
func TestSSEMarkPageReadUsesInnerMode(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodPost, "/ds/sse/mark-page-read", strings.NewReader(`{"view":"unread"}`))
	req.Header.Set("Content-Type", "application/json")
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "selector #entry-list") || !strings.Contains(body, "mode inner") {
		t.Errorf("entry-list patch must use mode inner so the container persists, got: %s", body)
	}
}

// TestSSEEntryProxifiesMedia verifies that entry content passed through the
// media proxy rewriter: with the default http-only mode, plain-HTTP image
// sources must be rewritten to the proxy URL (parity with the classic UI's
// proxyFilter).
func TestSSEEntryProxifiesMedia(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	entry := mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID,
		"Proxified Media Entry",
		`<p><img src="http://example.com/pic.jpg"></p>`)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entry/"+itoa(entry.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `src="http://example.com/pic.jpg"`) {
		t.Error("expected plain-HTTP image source to be rewritten by the media proxy")
	}
	if !strings.Contains(body, "/proxy/") {
		t.Error("expected proxified image URL in entry content")
	}
}

// TestSSESubscriptionsUsesFeedTreeInner verifies that sseSubscriptions patches
// the #subscription-panel .feed-tree element with ElementPatchModeInner, keeping
// the #subscription-panel aside, its .top-nav and <nav> shell intact.
func TestSSESubscriptionsUsesFeedTreeInner(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	_ = mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/subscriptions", nil)
	req, csrf := authenticateTestSession(t, store, req, user)
	_ = csrf

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "selector #subscription-panel .feed-tree") || !strings.Contains(body, "mode inner") {
		t.Errorf("subscription patch must target the feed tree with mode inner, got: %s", body)
	}
}

// TestSSEEntriesFeedClickOverridesStaleSearchSignals verifies that a nav click
// carrying an explicit feedId URL param is treated as a feed view even when the
// page signals still hold a previous search view/query (regression test for the
// stale-view defect).
func TestSSEEntriesFeedClickOverridesStaleSearchSignals(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Unique Feed Entry", "content")
	// Entry in another feed that would match a stale search query.
	otherCat := mtest.CreateTestCategoryWithTitle(t, store, user.ID, "Other Category")
	otherFeed := mtest.CreateTestFeedWithURL(t, store, user.ID, otherCat.ID, "https://example.com/other_feed.xml")
	_ = mtest.CreateTestEntryWithContent(t, store, user.ID, otherFeed.ID, "Unrelated Entry", "content")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Nav click: URL has feedId, signals carry view=search + searchQuery from
	// an earlier search interaction.
	q := url.Values{}
	q.Set("feedId", itoa(feed.ID))
	q.Set("datastar", `{"view":"search","searchQuery":"`+entry.Title+`"}`)
	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?"+q.Encode(), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, entry.Title) {
		t.Error("expected feed entries in response, got search-style filtering")
	}
	if strings.Contains(body, "Unrelated Entry") {
		t.Error("feed view must not include entries from other feeds")
	}
	// The response must resync the client signals to the authoritative view.
	if !strings.Contains(body, "datastar-patch-signals") {
		t.Error("expected signal patch in SSE response")
	}
	if !strings.Contains(body, `"view":"feed"`) {
		t.Error("expected view signal to be reset to feed")
	}
}

// TestSSEEntriesCategoryClickOverridesStaleSearchSignals is the category-link
// counterpart of the feed-click regression test.
func TestSSEEntriesCategoryClickOverridesStaleSearchSignals(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntryWithContent(t, store, user.ID, feed.ID, "Category Entry", "content")

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	q := url.Values{}
	q.Set("categoryId", itoa(cat.ID))
	q.Set("datastar", `{"view":"search","searchQuery":"nomatch"}`)
	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?"+q.Encode(), nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, entry.Title) {
		t.Error("expected category entries in response, got search-style filtering")
	}
	if !strings.Contains(body, `"view":"category"`) {
		t.Error("expected view signal to be reset to category")
	}
}

// TestSSEEntriesHonorsSortingPreferences verifies that entry lists are sorted
// by the user's entry order/direction preferences like the classic UI
// (regression test for hardcoded published_at DESC).
func TestSSEEntriesHonorsSortingPreferences(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	now := time.Now()
	oldEntry := &model.Entry{
		UserID: user.ID, FeedID: feed.ID,
		Hash: "hash_sort_old_" + t.Name(), Title: "Oldest Entry", Content: "c",
		URL: "https://example.com/sort_old", Date: now.Add(-48 * time.Hour),
	}
	newEntry := &model.Entry{
		UserID: user.ID, FeedID: feed.ID,
		Hash: "hash_sort_new_" + t.Name(), Title: "Newest Entry", Content: "c",
		URL: "https://example.com/sort_new", Date: now.Add(-1 * time.Hour),
	}
	for _, e := range []*model.Entry{newEntry, oldEntry} { // insert newest first
		if _, err := store.InsertEntryForFeed(user.ID, feed.ID, e); err != nil {
			t.Fatalf("failed to insert entry: %v", err)
		}
	}

	user.EntryOrder = model.DefaultSortingOrder
	user.EntryDirection = "asc"
	if err := store.UpdateUser(user); err != nil {
		t.Fatalf("failed to update user prefs: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	oldPos := strings.Index(body, "Oldest Entry")
	newPos := strings.Index(body, "Newest Entry")
	if oldPos == -1 || newPos == -1 {
		t.Fatal("expected both entries in response")
	}
	if oldPos > newPos {
		t.Error("expected oldest entry first with asc direction preference")
	}
}

// TestSSEEntriesOffsetOverflowResetsToFirstPage verifies that requesting an
// offset beyond the last page falls back to page one instead of an empty list
// (parity with the classic UI pagination behavior).
func TestSSEEntriesOffsetOverflowResetsToFirstPage(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	user.EntriesPerPage = 2
	if err := store.UpdateUser(user); err != nil {
		t.Fatalf("failed to update user prefs: %v", err)
	}
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	_ = mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entries?view=unread&offset=1000", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "entry-row") {
		t.Error("expected entry rows on offset overflow, got empty list")
	}
	if !strings.Contains(body, `"offset":0`) {
		t.Error("expected offset signal to be reset to 0")
	}
}

// TestThemeAttributeRendered verifies the data-theme/data-font attributes are
// derived from the user's theme preference so explicit light/dark/serif
// choices apply to the Datastar UI.
func TestThemeAttributeRendered(t *testing.T) {
	cases := []struct {
		theme, themeClass, themeFont string
	}{
		{"system_sans_serif", "", ""},
		{"light_sans_serif", "light", ""},
		{"dark_sans_serif", "dark", ""},
		{"system_serif", "", "serif"},
		{"light_serif", "light", "serif"},
		{"dark_serif", "dark", "serif"},
	}
	for _, tc := range cases {
		if got := themeClass(tc.theme); got != tc.themeClass {
			t.Errorf("themeClass(%q) = %q, want %q", tc.theme, got, tc.themeClass)
		}
		if got := themeFont(tc.theme); got != tc.themeFont {
			t.Errorf("themeFont(%q) = %q, want %q", tc.theme, got, tc.themeFont)
		}
	}

	// End-to-end: preference flows into the rendered attribute.
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	user.Theme = "dark_serif"
	if err := store.UpdateUser(user); err != nil {
		t.Fatalf("failed to update user theme: %v", err)
	}
	cat := mtest.CreateTestCategory(t, store, user.ID)
	_ = mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread", nil)
	req, _ = authenticateTestSession(t, store, req, user)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `data-theme="dark"`) {
		t.Error("expected data-theme=\"dark\" on html element")
	}
	if !strings.Contains(body, `data-font="serif"`) {
		t.Error("expected data-font=\"serif\" on html element")
	}
}

// TestEnclosureURLsProxified verifies enclosure media URLs are rewritten
// through the media proxy when the proxy mode and resource types cover the
// enclosure's MIME type, matching the classic UI's enclosure rendering.
func TestEnclosureURLsProxified(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	entry := &model.Entry{
		UserID:  user.ID,
		FeedID:  feed.ID,
		Hash:    "hash_enclosure_proxy_" + t.Name(),
		Title:   "Enclosure Proxy",
		Content: "<p>body</p>",
		URL:     "https://example.com/article",
		Date:    time.Now(),
		Enclosures: model.EnclosureList{
			&model.Enclosure{
				URL:      "https://example.com/pic.jpg",
				MimeType: "image/jpeg",
				Size:     10,
			},
		},
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert test entry: %v", err)
	}

	t.Setenv("MEDIA_PROXY_MODE", "all")
	configParser := config.NewConfigParser()
	parsedOptions, err := configParser.ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("unable to configure test options: %v", err)
	}
	prevOpts := config.Opts
	config.Opts = parsedOptions
	t.Cleanup(func() { config.Opts = prevOpts })

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entry/"+itoa(entry.ID), nil)
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `src="/proxy/`) {
		t.Errorf("image enclosure must be proxied under MEDIA_PROXY_MODE=all, got: %s", body)
	}
	if strings.Contains(body, `src="https://example.com/pic.jpg"`) {
		t.Error("raw enclosure URL must not appear as a media src when proxying")
	}
}

// TestEntryDetailDateLocalized verifies the article header renders a
// localized relative timestamp via the elapsed template function instead of
// a hardcoded English absolute date ("January 2, 2006" format), matching the
// classic UI's entry header.
func TestEntryDetailDateLocalized(t *testing.T) {
	for _, tc := range []struct {
		language string
		wants    []string
	}{
		{"en_US", []string{"ago"}},
		{"fr_FR", []string{"il y a"}},
		{"de_DE", []string{"vor"}},
	} {
		t.Run(tc.language, func(t *testing.T) {
			printer := locale.NewPrinter(tc.language)
			tpl, err := parseTemplates().Clone()
			if err != nil {
				t.Fatalf("cloning template: %v", err)
			}
			tpl.Funcs(template.FuncMap{
				"t":      printer.Printf,
				"plural": printer.Plural,
				"elapsed": func(t time.Time) string {
					return elapsedLocalized(printer, t)
				},
			})

			data := map[string]any{
				"SelectedEntry": map[string]any{
					"ID":              int64(7),
					"Title":           "Localized date",
					"Author":          "",
					"Date":            time.Now().UTC().Add(-3 * time.Hour),
					"Content":         template.HTML("<p>body</p>"),
					"Starred":         false,
					"URL":             "https://example.com/7",
					"Status":          model.EntryStatusUnread,
					"ReadingTime":     0,
					"ShowReadingTime": false,
				},
			}

			var buf strings.Builder
			if err := tpl.ExecuteTemplate(&buf, "entry_content", data); err != nil {
				t.Fatalf("executing entry_content: %v", err)
			}
			out := buf.String()

			if !strings.Contains(out, "<time") {
				t.Errorf("expected a <time> element in header, got:\n%s", out)
			}
			matched := false
			for _, want := range tc.wants {
				if strings.Contains(out, want) {
					matched = true
				}
			}
			if !matched {
				t.Errorf("expected localized elapsed text (one of %v) for %s, got:\n%s", tc.wants, tc.language, out)
			}
			if strings.Contains(out, "January") {
				t.Errorf("hardcoded English month name must not appear for %s:\n%s", tc.language, out)
			}
		})
	}
}

// TestSSEFetchContentKeepsParityFields verifies the detail re-render after a
// scrape keeps entry parity fields (tags, comments URL, reading time,
// enclosures) instead of dropping them, matching the pre-scrape header.
func TestSSEFetchContentKeepsParityFields(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	t.Setenv("FETCHER_ALLOW_PRIVATE_NETWORKS", "1")
	configParser := config.NewConfigParser()
	parsedOptions, err := configParser.ParseEnvironmentVariables()
	if err != nil {
		t.Fatalf("unable to configure test options: %v", err)
	}
	prevOpts := config.Opts
	config.Opts = parsedOptions
	t.Cleanup(func() { config.Opts = prevOpts })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Scraped</h1><p>Scraped body</p></body></html>`))
	}))
	t.Cleanup(srv.Close)

	entry := &model.Entry{
		UserID:      user.ID,
		FeedID:      feed.ID,
		Hash:        "hash_fetch_parity_" + t.Name(),
		Title:       "Fetch Parity",
		Content:     "original body",
		URL:         srv.URL + "/article",
		Date:        time.Now(),
		CommentsURL: "https://comments.example.com/item?id=1",
		ReadingTime: 4,
		Tags:        []string{"golang"},
		Enclosures: []*model.Enclosure{
			{URL: "https://example.com/audio.mp3", MimeType: "audio/mpeg"},
		},
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert test entry: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodPost, "/ds/sse/fetch-content/"+itoa(entry.ID), strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		"golang",                              // tag chip
		"https://comments.example.com/item?id=1", // comments link
		"audio.mp3",                           // enclosure
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scraped re-render must keep %q\nbody excerpt: %s", want, body)
		}
	}
	if !strings.Contains(body, "Scraped body") {
		t.Error("scraped content must replace the original body")
	}
}

// TestRowRerenderKeepsReadingTime verifies the SSE paths that re-render a
// single entry row (sseEntry, toggle-status) include ReadingTime/
// ShowReadingTime so the row's reading-time chip survives row patches when
// the user preference is enabled.
func TestRowRerenderKeepsReadingTime(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	user.ShowReadingTime = true
	if err := store.UpdateUser(user); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := &model.Entry{
		UserID:      user.ID,
		FeedID:      feed.ID,
		Hash:        "hash_row_reading_time_" + t.Name(),
		Title:       "Row Reading Time",
		Content:     "some body text",
		URL:         "https://example.com/row-reading-time",
		Date:        time.Now(),
		ReadingTime: 7,
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert entry: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// sseEntry path
	req := httptest.NewRequest(http.MethodGet, "/ds/sse/entry/"+itoa(entry.ID), nil)
	req, _ = authenticateTestSession(t, store, req, user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sseEntry: expected 200, got %d", w.Code)
	}
	printer := locale.NewPrinter(user.Language)
	want := printer.Plural("entry.estimated_reading_time", 7, 7)
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("sseEntry row patch must keep reading time %q\nbody: %s", want, w.Body.String())
	}

	// toggle-status path
	req = httptest.NewRequest(http.MethodPost, "/ds/sse/entry/status", strings.NewReader(`{"entryIds":[`+itoa(entry.ID)+`]}`))
	req.Header.Set("Content-Type", "application/json")
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle-status: expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), want) {
		t.Errorf("toggle-status row patch must keep reading time %q\nbody: %s", want, w.Body.String())
	}
}

// TestNonSearchViewIgnoresQParam verifies a stale ?q= on unread/starred/etc.
// never leaks a dead searchQuery into the page's pagination URLs; only the
// /ds/search view consumes the q parameter.
func TestNonSearchViewIgnoresQParam(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	user.EntriesPerPage = 5
	if err := store.UpdateUser(user); err != nil {
		t.Fatalf("failed to update user: %v", err)
	}
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	for i := 0; i < 7; i++ {
		if _, err := store.InsertEntryForFeed(user.ID, feed.ID, &model.Entry{
			UserID:  user.ID,
			FeedID:  feed.ID,
			Hash:    fmt.Sprintf("hash_qparam_%s_%d", t.Name(), i),
			Title:   fmt.Sprintf("QParam Entry %d", i),
			Content: "body",
			URL:     fmt.Sprintf("https://example.com/qparam-%d", i),
			Date:    time.Now(),
		}); err != nil {
			t.Fatalf("failed to insert entry: %v", err)
		}
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodGet, "/ds/unread?q=stale", nil)
	req, _ = authenticateTestSession(t, store, req, user)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "searchQuery=stale") {
		t.Error("unread view must not embed ?q= into pagination URLs")
	}

	// The search view still consumes q.
	req = httptest.NewRequest(http.MethodGet, "/ds/search?q=QParam", nil)
	req, _ = authenticateTestSession(t, store, req, user)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "searchQuery=QParam") {
		t.Error("search view must pass the q param through to pagination")
	}
}

// TestArticleToolbarParityMarkers verifies the toolbar carries stable
// data-action markers for the keyboard parity shortcuts: toggle-star ('f'),
// fetch-content ('d'), and comments-link ('c'/'C'). The handlers in
// keyboard.js select by these markers, not by button text.
func TestArticleToolbarParityMarkers(t *testing.T) {
	tpl := parseTemplates()

	printer := locale.NewPrinter("en_US")
	tpl, err := tpl.Clone()
	if err != nil {
		t.Fatalf("cloning template: %v", err)
	}
	tpl.Funcs(template.FuncMap{
		"t":      printer.Printf,
		"plural": printer.Plural,
	})

	var buf strings.Builder
	data := map[string]any{
		"ID":          int64(42),
		"Starred":     false,
		"URL":         "https://example.com/42",
		"Status":      model.EntryStatusUnread,
		"CommentsURL": "https://example.com/comments",
	}
	if err := tpl.ExecuteTemplate(&buf, "article_toolbar", data); err != nil {
		t.Fatalf("executing article_toolbar: %v", err)
	}
	out := buf.String()

	for _, marker := range []string{
		`data-action="toggle-star"`,
		`data-action="fetch-content"`,
		`data-action="comments-link"`,
	} {
		if !strings.Contains(out, marker) {
			t.Errorf("toolbar must carry %s\noutput:\n%s", marker, out)
		}
	}
}

// TestEntryRowCarriesFeedID verifies entry rows expose data-feed-id so the
// classic-parity "g f" shortcut can navigate to the selected entry's feed.
func TestEntryRowCarriesFeedID(t *testing.T) {
	tpl := parseTemplates()

	var buf strings.Builder
	data := map[string]any{
		"ID":      int64(7),
		"Title":   "Some Entry",
		"Status":  model.EntryStatusUnread,
		"Starred": false,
		"Date":    time.Now(),
		"Feed":    &feedRef{Title: "Feed", ID: int64(12)},
	}
	if err := tpl.ExecuteTemplate(&buf, "entry_row", data); err != nil {
		t.Fatalf("executing entry_row: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `data-feed-id="12"`) {
		t.Errorf("entry row must expose data-feed-id=12 for g f navigation\noutput:\n%s", out)
	}
}

// TestEntryContentEnclosuresDetails verifies enclosures render inside a
// <details class="entry-enclosures"> element so the classic-parity 'a'
// shortcut can toggle their visibility.
func TestEntryContentEnclosuresDetails(t *testing.T) {
	tpl := parseTemplates()

	printer := locale.NewPrinter("en_US")
	tpl, err := tpl.Clone()
	if err != nil {
		t.Fatalf("cloning template: %v", err)
	}
	tpl.Funcs(template.FuncMap{
		"t":      printer.Printf,
		"plural": printer.Plural,
	})

	entry := &entryDetailView{
		ID:        5,
		Title:     "With Enclosures",
		Content:   "<p>body</p>",
		Enclosures: []enclosureView{
			{ID: 1, URL: "https://example.com/a.mp3", MimeType: "audio/mpeg", IsAudio: true},
		},
	}
	var buf strings.Builder
	data := map[string]any{"SelectedEntry": entry, "ShowReadingTime": true}
	if err := tpl.ExecuteTemplate(&buf, "entry_content", data); err != nil {
		t.Fatalf("executing entry_content: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, `<details class="entry-enclosures`) {
		t.Errorf("enclosures must render inside <details class=\"entry-enclosures\">\noutput:\n%s", out)
	}
	if !strings.Contains(out, "entry-enclosure-item") {
		t.Errorf("enclosure item must still render\noutput:\n%s", out)
	}
}

// TestKeyboardJSParityShortcuts statically verifies keyboard.js implements
// the classic-UI parity shortcuts and that the selectors it uses match the
// data-action markers rendered by the templates (tested above). This is a
// source-level guard: if either side renames a marker, this test catches it.
func TestKeyboardJSParityShortcuts(t *testing.T) {
	src, err := os.ReadFile("static/js/keyboard.js")
	if err != nil {
		t.Fatalf("reading keyboard.js: %v", err)
	}
	js := string(src)

	for _, tc := range []struct{ key, selector string }{
		{"f", `button[data-action="toggle-star"]`},
		{"d", `button[data-action="fetch-content"]`},
		{"c", `a[data-action="comments-link"]`},
		{"R", `button[data-action="refresh-all"]`},
	} {
		if !strings.Contains(js, `case "`+tc.key+`"`) {
			t.Errorf("keyboard.js must handle key %q", tc.key)
		}
		if !strings.Contains(js, tc.selector) {
			t.Errorf("keyboard.js must select %q for key %q", tc.selector, tc.key)
		}
	}
	for _, key := range []string{"h", "l", "C", "a", "z"} {
		if !strings.Contains(js, `case "`+key+`"`) {
			t.Errorf("keyboard.js must handle key %q", key)
		}
	}
	if !strings.Contains(js, `"/ds/feed/" + feedID`) {
		t.Error("keyboard.js must navigate to /ds/feed/{id} for g f")
	}
	if !strings.Contains(js, "pendingZ") {
		t.Error("keyboard.js must track the z-sequence (z t) pending state")
	}
	// Escape must work while an input is focused (blur the search box after
	// '/'), so it is handled before the input guard.
	if !strings.Contains(js, `e.key === "Escape"`) {
		t.Error("keyboard.js must handle Escape before the input-focused guard")
	}
}

// TestShortcutsOverlayListsParityKeys verifies the shortcuts overlay documents
// the new parity keys using existing upstream i18n keys.
func TestShortcutsOverlayListsParityKeys(t *testing.T) {
	tpl := parseTemplates()
	printer := locale.NewPrinter("en_US")
	tpl, err := tpl.Clone()
	if err != nil {
		t.Fatalf("cloning template: %v", err)
	}
	tpl.Funcs(template.FuncMap{
		"t":      printer.Printf,
		"plural": printer.Plural,
	})

	var buf strings.Builder
	if err := tpl.ExecuteTemplate(&buf, "shortcuts_overlay", nil); err != nil {
		t.Fatalf("executing shortcuts_overlay: %v", err)
	}
	out := buf.String()

	for _, dt := range []string{
		"<dt>s / f</dt>", "<dt>h / l</dt>", "<dt>g f</dt>", "<dt>d</dt>",
		"<dt>c / C</dt>", "<dt>a</dt>", "<dt>z t</dt>", "<dt>R</dt>",
	} {
		if !strings.Contains(out, dt) {
			t.Errorf("overlay must list %s\noutput:\n%s", dt, out)
		}
	}
	if want := printer.Print("page.keyboard_shortcuts.toggle_entry_attachments"); !strings.Contains(out, want) {
		t.Errorf("overlay must describe the 'a' shortcut with %q\noutput:\n%s", want, out)
	}
}

// TestSSEToggleStarUpdatesToolbarButton verifies the toolbar star button has
// an id that sseToggleStar actually patches (#toolbar-star-btn-{ID}) and that
// the SSE response patches both the row star and the toolbar star. Regression:
// the handler used to target #toolbar-star-icon-{ID}, which never existed, so
// the toolbar star label never updated after a star toggle.
func TestSSEToggleStarUpdatesToolbarButton(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := &model.Entry{
		FeedID:  feed.ID,
		UserID:  user.ID,
		Status:  model.EntryStatusUnread,
		Title:   "Star Me",
		Hash:    "star-toolbar-1",
		Content: "<p>x</p>",
		Date:    time.Now(),
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert entry: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	req := httptest.NewRequest(http.MethodPost, "/ds/sse/entry/star/"+itoa(entry.ID), nil)
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `#toolbar-star-btn-`+itoa(entry.ID)) {
		t.Errorf("SSE response must target #toolbar-star-btn-%s\nbody:\n%s", itoa(entry.ID), body)
	}
	if !strings.Contains(body, `data-action="toggle-star"`) {
		t.Errorf("patched toolbar button must keep the toggle-star marker\nbody:\n%s", body)
	}
}

// TestSSEToggleShareUpdatesToolbarButton verifies the share toggle patches the
// toolbar share element (#toolbar-share-btn-{ID}) so the label flips between
// Share and Unshare and, when shared, exposes the public /share/{code} link.
// Regression: the handler used to patch only the "shared" signal, which no
// template binds, so the toolbar never reflected the new state.
func TestSSEToggleShareUpdatesToolbarButton(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := &model.Entry{
		UserID:  user.ID,
		FeedID:  feed.ID,
		Hash:    "hash_share_" + t.Name(),
		Title:   "Share Me",
		Content: "x",
		Date:    time.Now(),
	}
	if _, err := store.InsertEntryForFeed(user.ID, feed.ID, entry); err != nil {
		t.Fatalf("failed to insert entry: %v", err)
	}

	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Share it.
	req := httptest.NewRequest(http.MethodPost, "/ds/sse/share/"+itoa(entry.ID), nil)
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, `#toolbar-share-btn-`+itoa(entry.ID)) {
		t.Errorf("SSE response must target #toolbar-share-btn-%s\nbody:\n%s", itoa(entry.ID), body)
	}
	printer := locale.NewPrinter(user.Language)
	if want := printer.Print("entry.unshare.label"); !strings.Contains(body, want) {
		t.Errorf("shared state must show the %q button\nbody:\n%s", want, body)
	}
	if !strings.Contains(body, `href="/share/`) {
		t.Errorf("shared state must expose the public share link\nbody:\n%s", body)
	}

	// Unshare it.
	req = httptest.NewRequest(http.MethodPost, "/ds/sse/share/"+itoa(entry.ID), nil)
	req, csrf = authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body = w.Body.String()
	if want := printer.Print("entry.share.label"); !strings.Contains(body, want) {
		t.Errorf("unshared state must show the %q button\nbody:\n%s", want, body)
	}
	if strings.Contains(body, `href="/share/`) {
		t.Errorf("unshared state must not expose a share link\nbody:\n%s", body)
	}
}

// TestFetchOPMLErrorSurfacesFlash verifies a failed remote OPML fetch sets a
// flash cookie and the settings page renders the error banner once (then
// clears it), instead of silently redirecting with no user-visible error.
func TestFetchOPMLErrorSurfacesFlash(t *testing.T) {
	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	pool := worker.NewPool(store, 1)
	handler := Serve(store, pool)

	// Point at a closed port so the fetch fails with a localized error.
	req := httptest.NewRequest(http.MethodPost, "/ds/fetch-opml", strings.NewReader("url=http://127.0.0.1:1/x.opml"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req, csrf := authenticateTestSession(t, store, req, user)
	req.Header.Set("X-Csrf-Token", csrf)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	var flash string
	for _, c := range w.Result().Cookies() {
		if c.Name == "dsui_flash_error" {
			flash = c.Value
		}
	}
	if flash == "" {
		t.Fatalf("expected dsui_flash_error cookie, got none; headers: %v", w.Header())
	}

	// Settings render must show the banner and clear the cookie.
	req = httptest.NewRequest(http.MethodGet, "/ds/settings", nil)
	req.AddCookie(&http.Cookie{Name: "dsui_flash_error", Value: flash})
	req, _ = authenticateTestSession(t, store, req, user)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("settings expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "flash-error") {
		t.Errorf("settings must render the flash error banner\nbody:\n%s", body)
	}
	decoded, _ := url.QueryUnescape(flash)
	unescaped := html.UnescapeString(body)
	if !strings.Contains(unescaped, decoded) {
		t.Errorf("settings must include the flash message %q\nbody:\n%s", decoded, body)
	}

	// The consuming response must expire the cookie so the banner shows once.
	var cleared bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "dsui_flash_error" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("settings response must expire the dsui_flash_error cookie")
	}
}
