// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"miniflux.app/v2/internal/database/dialect"
	mtest "miniflux.app/v2/internal/storage/testing"
)

// callTool invokes a JSON-RPC tools/call against the handler and returns the
// decoded Result content payload. Fails the test on any JSON-RPC error.
func callTool(t *testing.T, h *MCPHandler, userID int64, tool string, args map[string]any) map[string]any {
	t.Helper()

	params, _ := json.Marshal(struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}{Name: tool, Arguments: args})

	body, _ := json.Marshal(mcpRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		ID:      1,
		Params:  params,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/mcp", bytes.NewReader(body))
	req = req.WithContext(withTestAuth(req.Context(), userID))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp mcpResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body: %s", w.Body.String())
	require.Nil(t, resp.Error, "rpc error: %+v", resp.Error)
	require.NotNil(t, resp.Result)

	// Result is a mcpResult with one text content item containing JSON.
	resultMap, ok := resp.Result.(map[string]any)
	require.True(t, ok, "result should be a map, got %T", resp.Result)
	content, ok := resultMap["content"].([]any)
	require.True(t, ok, "result should have content array, got %T", resultMap["content"])
	require.Len(t, content, 1)
	text := content[0].(map[string]any)["text"].(string)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &payload), "content text was: %s", text)
	return payload
}

func TestListFeedsIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	mtest.CreateTestFeedWithURL(t, store, user.ID, cat.ID, "https://example.com/feed_a.xml")
	mtest.CreateTestFeedWithURL(t, store, user.ID, cat.ID, "https://example.com/feed_b.xml")

	// Use a handler with the real store; the default UserIDContextKey is 1,
	// so we reuse the test helpers which also target user 1.
	h := NewMCPHandler(store).(*MCPHandler)

	t.Run("defaults return feeds", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "list_feeds", map[string]any{})
		feeds, ok := payload["feeds"].([]any)
		require.True(t, ok, "expected feeds array, got %T", payload["feeds"])
		assert.Len(t, feeds, 2)
		total := payload["total"].(float64)
		assert.Equal(t, float64(2), total)
	})

	t.Run("limit respected", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "list_feeds", map[string]any{"limit": 1})
		feeds := payload["feeds"].([]any)
		assert.Len(t, feeds, 1)
	})

	t.Run("offset past end returns empty", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "list_feeds", map[string]any{"offset": 99})
		feeds := payload["feeds"].([]any)
		assert.Empty(t, feeds)
	})
}

func TestGetEntriesIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	mtest.CreateTestEntries(t, store, user.ID, feed.ID, 5)

	h := NewMCPHandler(store).(*MCPHandler)

	t.Run("defaults return entries with total", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "get_entries", map[string]any{})
		entries := payload["entries"].([]any)
		assert.Len(t, entries, 5)
		total := payload["total"].(float64)
		assert.Equal(t, float64(5), total)
	})

	t.Run("limit respected", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "get_entries", map[string]any{"limit": 2})
		entries := payload["entries"].([]any)
		assert.Len(t, entries, 2)
	})

	t.Run("offset past total returns empty", func(t *testing.T) {
		payload := callTool(t, h, user.ID, "get_entries", map[string]any{"offset": 99})
		entries := payload["entries"].([]any)
		assert.Empty(t, entries)
	})
}

func TestMarkEntriesIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entries := mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	h := NewMCPHandler(store).(*MCPHandler)

	entryIDs := []int64{entries[0].ID, entries[1].ID}
	args := map[string]any{
		"entry_ids": entryIDs,
		"status":    "read",
	}
	payload := callTool(t, h, user.ID, "mark_entries", args)
	assert.Equal(t, float64(2), payload["updated"])

	// Verify status actually changed in DB.
	got, err := store.NewEntryQueryBuilder(user.ID).WithEntryIDs(entries[0].ID).GetEntry()
	require.NoError(t, err)
	assert.Equal(t, "read", got.Status)

	got2, err := store.NewEntryQueryBuilder(user.ID).WithEntryIDs(entries[2].ID).GetEntry()
	require.NoError(t, err)
	assert.Equal(t, "unread", got2.Status, "untouched entry should remain unread")
}

func TestToggleBookmarkIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entry := mtest.CreateTestEntry(t, store, user.ID, feed.ID)

	h := NewMCPHandler(store).(*MCPHandler)

	payload := callTool(t, h, user.ID, "toggle_bookmark", map[string]any{"entry_id": entry.ID})
	assert.Equal(t, true, payload["toggled"])

	// Verify starred flag flipped in DB.
	got, err := store.NewEntryQueryBuilder(user.ID).WithEntryIDs(entry.ID).GetEntry()
	require.NoError(t, err)
	assert.True(t, got.Starred)
}

func TestListCategoriesIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	mtest.CreateTestCategoryWithTitle(t, store, user.ID, "News")
	mtest.CreateTestCategoryWithTitle(t, store, user.ID, "Tech")

	h := NewMCPHandler(store).(*MCPHandler)

	payload := callTool(t, h, user.ID, "list_categories", map[string]any{})
	cats, ok := payload["categories"].([]any)
	require.True(t, ok, "expected categories array, got %T", payload["categories"])
	assert.GreaterOrEqual(t, len(cats), 2)
}

func TestMarkFeedAsReadIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entries := mtest.CreateTestEntries(t, store, user.ID, feed.ID, 3)

	h := NewMCPHandler(store).(*MCPHandler)

	payload := callTool(t, h, user.ID, "mark_feed_as_read", map[string]any{"feed_id": feed.ID})
	assert.Equal(t, true, payload["marked"])

	// Verify all entries in the feed are now read.
	for _, e := range entries {
		got, err := store.NewEntryQueryBuilder(user.ID).WithEntryIDs(e.ID).GetEntry()
		require.NoError(t, err)
		assert.Equal(t, "read", got.Status, "entry %d should be read", e.ID)
	}
}

func TestMarkCategoryAsReadIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)
	entries := mtest.CreateTestEntries(t, store, user.ID, feed.ID, 2)

	h := NewMCPHandler(store).(*MCPHandler)

	payload := callTool(t, h, user.ID, "mark_category_as_read", map[string]any{"category_id": cat.ID})
	assert.Equal(t, true, payload["marked"])

	for _, e := range entries {
		got, err := store.NewEntryQueryBuilder(user.ID).WithEntryIDs(e.ID).GetEntry()
		require.NoError(t, err)
		assert.Equal(t, "read", got.Status)
	}
}

func TestRefreshFeedIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)
	cat := mtest.CreateTestCategory(t, store, user.ID)
	feed := mtest.CreateTestFeed(t, store, user.ID, cat.ID)

	h := NewMCPHandler(store).(*MCPHandler)

	// The test feed points to a synthetic URL, so RefreshFeed will fail to fetch it.
	// We verify the tool surfaces the error correctly (not a panic).
	args, _ := json.Marshal(map[string]any{"feed_id": feed.ID})
	_, err := h.toolRefreshFeed(user.ID, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refresh failed")
}

func TestRefreshFeedNotFoundIntegration(t *testing.T) {
	t.Parallel()

	store := mtest.SetupTestDB(t, dialect.SQLite)
	user := mtest.CreateTestUser(t, store)

	h := NewMCPHandler(store).(*MCPHandler)

	args, _ := json.Marshal(map[string]any{"feed_id": 99999})
	_, err := h.toolRefreshFeed(user.ID, args)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
