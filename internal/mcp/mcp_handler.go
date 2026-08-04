// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp // import "miniflux.app/v2/internal/mcp"

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/model"
	readerHandler "miniflux.app/v2/internal/reader/handler"
	"miniflux.app/v2/internal/storage"
	"miniflux.app/v2/internal/validator"
)

// MCPHandler handles Model Context Protocol requests.
// It supports the HTTP+SSE transport from MCP spec 2024-11-05:
//   - GET /v1/mcp opens an SSE stream and emits an `endpoint` event pointing at POST /v1/mcp?session_id=...
//   - POST /v1/mcp?session_id=... accepts JSON-RPC 2.0; responses are written to the matching SSE stream
//
// POST without a live session also works in plain request/response mode for clients (e.g. curl)
// that don't want to maintain a persistent SSE connection.
type MCPHandler struct {
	store    *storage.Storage
	sessions *sessionStore
}

// NewMCPHandler returns an http.Handler for the MCP endpoint.
func NewMCPHandler(store *storage.Storage) http.Handler {
	return &MCPHandler{store: store, sessions: newSessionStore()}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      any             `json:"id"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// mcpResponse is a JSON-RPC 2.0 response. Result is a free-form any so we can
// return either a tool-call content payload or a direct result object (e.g. for
// initialize).
type mcpResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *mcpError `json:"error,omitempty"`
}

type mcpResult struct {
	Content []mcpContent `json:"content"`
}

type mcpContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// mcpTool is a typed tool definition (Go 1.26: prefer typed struct over map[string]any).
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// JSON-RPC 2.0 error codes.
const (
	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternal       = -32603
	errCodeServerError    = -32000
	errCodeUnauthorized   = -32003
)

// ServeHTTP dispatches JSON-RPC 2.0 requests.
// The endpoint is mounted inside the v1 mux so the existing API-key and basic-auth
// middleware apply; auth state is read via request.IsAuthenticated / request.UserID.
func (h *MCPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !request.IsAuthenticated(r) {
		writeJSONRPCError(w, nil, errCodeUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.openSSE(w, r)
	case http.MethodPost:
		h.handlePost(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// openSSE opens a Server-Sent Events stream and emits the `endpoint` event
// per MCP spec 2024-11-05. The connection stays open until the client disconnects;
// POST responses for this session are fanned out to the stream.
func (h *MCPHandler) openSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.NewString()
	h.sessions.register(sessionID)
	defer h.sessions.close(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Per MCP spec 2024-11-05, the endpoint event payload is the bare URI string.
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", "/v1/mcp?session_id="+sessionID)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-h.sessions.channel(sessionID):
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// handlePost processes one JSON-RPC request. If a session_id query param matches an
// active SSE stream, the response is written to that stream (SSE transport); otherwise
// the response is written directly to the HTTP response (plain request/response mode).
func (h *MCPHandler) handlePost(w http.ResponseWriter, r *http.Request) {
	var req mcpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPCError(w, nil, errCodeParseError, "Parse error: "+err.Error())
		return
	}

	userID := request.UserID(r)

	sessionID := r.URL.Query().Get("session_id")
	if sess, ok := h.sessions.get(sessionID); ok {
		// SSE mode: process and route the response through the stream.
		sessCh := sess.send
		h.dispatchAndSend(userID, req, func(payload []byte) {
			select {
			case sessCh <- payload:
			case <-time.After(5 * time.Second):
				log.Printf("mcp: session %s send buffer full, dropping response", sessionID)
			}
		})
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Plain mode: write JSON-RPC response inline.
	h.dispatchAndSend(userID, req, func(payload []byte) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
}

// dispatchAndSend resolves a JSON-RPC method and serializes the response through send.
func (h *MCPHandler) dispatchAndSend(userID int64, req mcpRequest, send func([]byte)) {
	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = initializeResult()
	case "notifications/initialized":
		// No-op acknowledgement; client is signalling it's ready. No response needed,
		// but we send an empty result so opencode's request/response correlation completes.
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefinitions()}
	case "tools/call":
		resp = h.runTool(userID, req)
	default:
		resp.Error = &mcpError{Code: errCodeMethodNotFound, Message: "Unknown method: " + req.Method}
	}

	send([]byte(jsonMustMarshal(resp)))
}

// runTool dispatches a tools/call request to the named tool and returns the response
// (either Result or Error populated).
func (h *MCPHandler) runTool(userID int64, req mcpRequest) mcpResponse {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return mcpResponse{JSONRPC: "2.0", ID: req.ID, Error: &mcpError{Code: errCodeInvalidParams, Message: err.Error()}}
	}
	if len(call.Arguments) == 0 {
		call.Arguments = []byte("{}")
	}

	resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}

	var result *mcpResult
	var err error
	switch call.Name {
	case "list_feeds":
		result, err = h.toolListFeeds(userID, call.Arguments)
	case "get_entries":
		result, err = h.toolGetEntries(userID, call.Arguments)
	case "mark_entries":
		result, err = h.toolMarkEntries(userID, call.Arguments)
	case "toggle_bookmark":
		result, err = h.toolToggleBookmark(userID, call.Arguments)
	case "list_categories":
		result, err = h.toolListCategories(userID)
	case "mark_feed_as_read":
		result, err = h.toolMarkFeedAsRead(userID, call.Arguments)
	case "mark_category_as_read":
		result, err = h.toolMarkCategoryAsRead(userID, call.Arguments)
	case "refresh_feed":
		result, err = h.toolRefreshFeed(userID, call.Arguments)
	default:
		resp.Error = &mcpError{Code: errCodeMethodNotFound, Message: "Unknown tool: " + call.Name}
		return resp
	}
	if err != nil {
		resp.Error = errPayload(err)
	} else if result != nil {
		resp.Result = *result
	}
	return resp
}

// errPayload converts a Go error into a JSON-RPC error payload.
// All caller errors map to -32000 (server error); param-validation messages
// are returned as plain strings within that code.
func errPayload(err error) *mcpError {
	if err == nil {
		return nil
	}
	return &mcpError{Code: errCodeServerError, Message: err.Error()}
}

// initializeResult returns the server capabilities advertised to MCP clients.
// Tools are the only capability this server exposes today.
func initializeResult() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
		"serverInfo": map[string]any{
			"name":    "miniflux",
			"version": "2",
		},
	}
}

func toolDefinitions() []mcpTool {
	intProp := func(desc string) map[string]any {
		return map[string]any{"type": "integer", "description": desc}
	}
	strProp := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return []mcpTool{
		{
			Name:        "list_feeds",
			Description: "List RSS feeds the user is subscribed to.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category_id": intProp("Filter by category ID"),
					"limit":       intProp("Maximum number of feeds to return (default 100, max 200)"),
					"offset":      intProp("Number of feeds to skip for pagination"),
				},
			},
		},
		{
			Name:        "get_entries",
			Description: "Query feed entries with filters, sorting and pagination.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"feed_id":     intProp("Filter by feed ID"),
					"category_id": intProp("Filter by category ID"),
					"status":      strProp("Filter by entry status (unread, read, or removed)"),
					"starred":     map[string]any{"type": "boolean", "description": "Only starred entries"},
					"search":      strProp("Full-text search query"),
					"order":       strProp("Sort field (default published_at)"),
					"direction":   map[string]any{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc", "description": "Sort direction"},
					"limit":       intProp("Maximum number of entries (default 50, max 200)"),
					"offset":      intProp("Number of entries to skip"),
				},
			},
		},
		{
			Name:        "mark_entries",
			Description: "Update the status of one or more entries.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entry_ids": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "Entry IDs to update"},
					"status":    map[string]any{"type": "string", "enum": []string{"unread", "read"}, "description": "New status"},
				},
				"required": []string{"entry_ids", "status"},
			},
		},
		{
			Name:        "toggle_bookmark",
			Description: "Toggle the starred (bookmarked) flag on a single entry.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entry_id": intProp("Entry ID to toggle"),
				},
				"required": []string{"entry_id"},
			},
		},
		{
			Name:        "list_categories",
			Description: "List all feed categories for the user.",
			InputSchema: map[string]any{"type": "object"},
		},
		{
			Name:        "mark_feed_as_read",
			Description: "Mark all unread entries in a feed as read.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"feed_id": intProp("Feed ID"),
				},
				"required": []string{"feed_id"},
			},
		},
		{
			Name:        "mark_category_as_read",
			Description: "Mark all unread entries in a category as read.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category_id": intProp("Category ID"),
				},
				"required": []string{"category_id"},
			},
		},
		{
			Name:        "refresh_feed",
			Description: "Refresh a single feed now (fetch new entries from source).",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"feed_id": intProp("Feed ID to refresh"),
				},
				"required": []string{"feed_id"},
			},
		},
	}
}

// listFeedsParams is the decoded and normalized arguments for the list_feeds tool.
type listFeedsParams struct {
	CategoryID int64 `json:"category_id"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
}

// parseListFeedsParams decodes and normalizes list_feeds arguments.
// Pure: no store, no I/O; deterministic for deterministic input.
func parseListFeedsParams(args json.RawMessage) (listFeedsParams, error) {
	var p listFeedsParams
	if err := json.Unmarshal(args, &p); err != nil {
		return p, err
	}
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p, nil
}

func (h *MCPHandler) toolListFeeds(userID int64, args json.RawMessage) (*mcpResult, error) {
	p, err := parseListFeedsParams(args)
	if err != nil {
		return nil, err
	}

	builder := h.store.NewFeedQueryBuilder(userID).
		WithLimit(p.Limit).
		WithOffset(p.Offset).
		WithSorting(model.DefaultFeedSorting, model.DefaultFeedSortingDirection)
	if p.CategoryID > 0 {
		builder.WithCategoryID(p.CategoryID)
	}

	feeds, err := builder.GetFeeds()
	if err != nil {
		return nil, err
	}

	// Fetch filtered total (same category filter, no limit/offset) for pagination metadata.
	allBuilder := h.store.NewFeedQueryBuilder(userID)
	if p.CategoryID > 0 {
		allBuilder.WithCategoryID(p.CategoryID)
	}
	allFeeds, err := allBuilder.GetFeeds()
	if err != nil {
		return nil, err
	}

	return textResult(map[string]any{
		"total":  len(allFeeds),
		"feeds":  feeds,
		"limit":  p.Limit,
		"offset": p.Offset,
	}), nil
}

// getEntriesParams is the decoded and normalized arguments for the get_entries tool.
type getEntriesParams struct {
	FeedID     int64  `json:"feed_id"`
	CategoryID int64  `json:"category_id"`
	Status     string `json:"status"`
	Starred    *bool  `json:"starred"`
	Search     string `json:"search"`
	Order      string `json:"order"`
	Direction  string `json:"direction"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

// parseGetEntriesParams decodes and normalizes get_entries arguments.
// Pure: no store, no I/O; deterministic for deterministic input.
func parseGetEntriesParams(args json.RawMessage) (getEntriesParams, error) {
	var p getEntriesParams
	if err := json.Unmarshal(args, &p); err != nil {
		return p, err
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	if p.Order == "" {
		p.Order = model.DefaultSortingOrder
	}
	if p.Direction == "" {
		p.Direction = model.DefaultSortingDirection
	}
	if p.Status != "" {
		if err := validator.ValidateEntryStatus(p.Status); err != nil {
			return p, err
		}
	}
	return p, nil
}

func (h *MCPHandler) toolGetEntries(userID int64, args json.RawMessage) (*mcpResult, error) {
	p, err := parseGetEntriesParams(args)
	if err != nil {
		return nil, err
	}

	builder := h.store.NewEntryQueryBuilder(userID).
		WithSorting(p.Order, p.Direction).
		WithLimit(p.Limit).
		WithOffset(p.Offset)
	if p.FeedID > 0 {
		builder.WithFeedID(p.FeedID)
	}
	if p.CategoryID > 0 {
		builder.WithCategoryID(p.CategoryID)
	}
	if p.Status != "" {
		builder.WithStatuses(p.Status)
	}
	if p.Starred != nil {
		builder.WithStarred(*p.Starred)
	}
	if p.Search != "" {
		builder.WithSearchQuery(p.Search)
	}

	entries, total, err := builder.GetEntriesWithCount()
	if err != nil {
		return nil, err
	}

	return textResult(map[string]any{"total": total, "entries": entries}), nil
}

// validateRequiredID rejects a non-positive id, mirroring the "X is required"
// checks shared by several tools. Pure and deterministic.
func validateRequiredID(id int64, name string) error {
	if id <= 0 {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func (h *MCPHandler) toolMarkEntries(userID int64, args json.RawMessage) (*mcpResult, error) {
	var p struct {
		EntryIDs []int64 `json:"entry_ids"`
		Status   string  `json:"status"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if err := validateMarkEntries(p.EntryIDs, p.Status); err != nil {
		return nil, err
	}

	if err := h.store.SetEntriesStatus(userID, p.EntryIDs, p.Status); err != nil {
		return nil, err
	}

	return textResult(map[string]any{"updated": len(p.EntryIDs), "status": p.Status}), nil
}

// validateMarkEntries validates mark_entries arguments (non-empty entry_ids and a
// valid entry status). Pure and deterministic.
func validateMarkEntries(entryIDs []int64, status string) error {
	if len(entryIDs) == 0 {
		return fmt.Errorf("entry_ids is required")
	}
	if err := validator.ValidateEntryStatus(status); err != nil {
		return err
	}
	return nil
}

func (h *MCPHandler) toolToggleBookmark(userID int64, args json.RawMessage) (*mcpResult, error) {
	var p struct {
		EntryID int64 `json:"entry_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if err := validateRequiredID(p.EntryID, "entry_id"); err != nil {
		return nil, err
	}

	if err := h.store.ToggleStarred(userID, p.EntryID); err != nil {
		return nil, err
	}

	return textResult(map[string]any{"entry_id": p.EntryID, "toggled": true}), nil
}

func (h *MCPHandler) toolListCategories(userID int64) (*mcpResult, error) {
	cats, err := h.store.Categories(userID)
	if err != nil {
		return nil, err
	}
	return textResult(map[string]any{"categories": cats}), nil
}

func (h *MCPHandler) toolMarkFeedAsRead(userID int64, args json.RawMessage) (*mcpResult, error) {
	var p struct {
		FeedID int64 `json:"feed_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if err := validateRequiredID(p.FeedID, "feed_id"); err != nil {
		return nil, err
	}

	if err := h.store.MarkFeedAsRead(userID, p.FeedID, time.Now()); err != nil {
		return nil, err
	}
	return textResult(map[string]any{"feed_id": p.FeedID, "marked": true}), nil
}

func (h *MCPHandler) toolMarkCategoryAsRead(userID int64, args json.RawMessage) (*mcpResult, error) {
	var p struct {
		CategoryID int64 `json:"category_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if err := validateRequiredID(p.CategoryID, "category_id"); err != nil {
		return nil, err
	}

	if err := h.store.MarkCategoryAsRead(userID, p.CategoryID, time.Now()); err != nil {
		return nil, err
	}
	return textResult(map[string]any{"category_id": p.CategoryID, "marked": true}), nil
}

func (h *MCPHandler) toolRefreshFeed(userID int64, args json.RawMessage) (*mcpResult, error) {
	var p struct {
		FeedID int64 `json:"feed_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	if err := validateRequiredID(p.FeedID, "feed_id"); err != nil {
		return nil, err
	}

	if !h.store.FeedExists(userID, p.FeedID) {
		return nil, fmt.Errorf("feed %d not found", p.FeedID)
	}

	if localizedErr := readerHandler.RefreshFeed(h.store, userID, p.FeedID, false); localizedErr != nil {
		return nil, fmt.Errorf("refresh failed: %s", localizedErr.Error())
	}
	return textResult(map[string]any{"feed_id": p.FeedID, "refreshed": true}), nil
}

// textResult is a helper that wraps an arbitrary JSON-encodable payload as the single
// text content item of an mcpResult. Used by tool implementations.
func textResult(payload any) *mcpResult {
	return &mcpResult{Content: []mcpContent{{Type: "text", Text: jsonMustMarshal(payload)}}}
}

// writeJSONRPCError writes a JSON-RPC 2.0 error response inline to w.
// Status code stays 200 unless the error is auth-related; JSON-RPC errors go
// in the body, per the transport spec.
func writeJSONRPCError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	if code == errCodeUnauthorized {
		w.WriteHeader(http.StatusUnauthorized)
	}
	_ = json.NewEncoder(w).Encode(mcpResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &mcpError{Code: code, Message: message},
	})
}

// jsonMustMarshal marshals v and logs (but does not panic) on failure.
// All inputs here are simple maps and model types that cannot fail in practice;
// the fallback is a valid JSON object so callers stay well-shaped.
func jsonMustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("mcp: json marshal failed: %v", err)
		return `{"error":"marshal failed"}`
	}
	return string(b)
}

// sessionStore holds SSE sessions so that POST requests can fan-out responses
// to the correct long-lived stream. Sessions expire if unused for sessionTTL.
type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	send     chan []byte
	lastUsed time.Time
}

const sessionTTL = 10 * time.Minute

func newSessionStore() *sessionStore {
	s := &sessionStore{sessions: make(map[string]*session)}
	go s.gc()
	return s
}

// register adds a session with the given (caller-generated) ID.
func (s *sessionStore) register(id string) {
	sess := &session{send: make(chan []byte, 16), lastUsed: time.Now()}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
}

func (s *sessionStore) get(id string) (*session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if ok {
		sess.lastUsed = time.Now()
	}
	return sess, ok
}

func (s *sessionStore) channel(id string) <-chan []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess.send
	}
	return nil
}

func (s *sessionStore) close(id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if sess != nil {
		close(sess.send)
	}
}

func (s *sessionStore) gc() {
	ticker := time.NewTicker(sessionTTL)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.mu.Lock()
		for id, sess := range s.sessions {
			if now.Sub(sess.lastUsed) > sessionTTL {
				delete(s.sessions, id)
				close(sess.send)
			}
		}
		s.mu.Unlock()
	}
}
