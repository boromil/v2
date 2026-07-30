// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/storage"
	dsstatic "miniflux.app/v2/internal/dsui/static"
	"miniflux.app/v2/internal/worker"

	"github.com/starfederation/datastar-go/datastar"
)

//go:embed templates/*.html templates/components/*.html
var templateFiles embed.FS

const (
	sessionCookieName = "MinifluxSessionID"
)

// handler provides the HTTP handlers for the Datastar UI.
type handler struct {
	store *storage.Storage
	pool  *worker.Pool
	tpl   *template.Template
}

// Serve returns an http.Handler for the Datastar UI routes.
func Serve(store *storage.Storage, pool *worker.Pool) http.Handler {
	tpl := parseTemplates()

	h := &handler{
		store: store,
		pool:  pool,
		tpl:   tpl,
	}

	mux := http.NewServeMux()

	// Static assets.
	mux.HandleFunc("GET /ds/stylesheets/{checksum}/{filename}", h.showStylesheet)
	mux.HandleFunc("GET /ds/js/{checksum}/{filename}", h.showJavascript)
	

	// Full page renders.
	mux.HandleFunc("GET /ds/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ds/unread", http.StatusFound)
	})
	mux.HandleFunc("GET /ds/unread", h.showApp)
	mux.HandleFunc("GET /ds/starred", h.showApp)
	mux.HandleFunc("GET /ds/history", h.showApp)
	mux.HandleFunc("GET /ds/search", h.showApp)
	mux.HandleFunc("GET /ds/feed/{feedID}", h.showApp)
	mux.HandleFunc("GET /ds/category/{categoryID}", h.showApp)

	// SSE fragment endpoints.
	mux.HandleFunc("GET /ds/sse/entries", h.sseEntries)
	mux.HandleFunc("GET /ds/sse/entry/{entryID}", h.sseEntry)
	mux.HandleFunc("GET /ds/sse/subscriptions", h.sseSubscriptions)
	mux.HandleFunc("POST /ds/sse/entry/star/{entryID}", h.sseToggleStar)
	mux.HandleFunc("POST /ds/sse/entry/status", h.sseToggleEntryStatus)
	mux.HandleFunc("POST /ds/sse/mark-all-read", h.sseMarkAllRead)

	// Apply middleware chain: secure headers -> session -> CSRF -> handlers.
	return secureHeadersMiddleware(sessionMiddleware(store)(newCSRFMiddleware().handle(mux)))
}

// parseTemplates loads and compiles all HTML templates.
func parseTemplates() *template.Template {
	funcMap := template.FuncMap{
		"elapsed": func(t time.Time) string {
			return elapsedTime(t)
		},
		"json": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}
	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFiles,
		"templates/layout.html",
		"templates/app.html",
		"templates/components/subscription_list.html",
		"templates/components/entry_list.html",
		"templates/components/entry_row.html",
		"templates/components/entry_content.html",
		"templates/components/feed_node.html",
		"templates/components/pagination.html",
	))
}

// ─── Static asset handlers ───────────────────────────────────────────────

func (h *handler) showStylesheet(w http.ResponseWriter, r *http.Request) {
	asset, ok := dsstatic.StylesheetBundles["app"]
	if !ok {
		response.HTMLNotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(asset.Data)
}

func (h *handler) showJavascript(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	// Strip extension for lookup key.
	key := filename
	if ext := strings.LastIndex(filename, "."); ext != -1 {
		key = filename[:ext]
	}
	asset, ok := dsstatic.JavascriptBundles[key]
	if !ok {
		response.HTMLNotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Write(asset.Data)
}

// ─── App view model ──────────────────────────────────────────────────────

// appViewModel is the data passed to the layout/app template.
type appViewModel struct {
	Title              string
	Language           string
	Direction          string
	CSRFToken          string
	SearchQuery        string
	StyleChecksum      string
	JSChecksum         string
	KeyboardChecksum   string
	ComponentsChecksum string
	SignalsJSON        template.JS
	ListTitle          string
	CanMarkAllRead     bool
	Entries            []entryView
	SelectedEntry      *entryDetailView
	Pagination         *paginationView
	MenuSections       []menuSection
}

type entryView struct {
	ID       int64
	Title    string
	Status   string
	Starred  bool
	Date     time.Time
	Feed     *feedRef
}

type feedRef struct {
	Title string
	ID    int64
}

type entryDetailView struct {
	ID       int64
	Title    string
	Author   string
	Date     time.Time
	Content  template.HTML
	Starred  bool
	URL      string
	Feed     *feedRef
	Status   string
}

type paginationView struct {
	CurrentPage int
	TotalPages  int
	HasPrev     bool
	HasNext     bool
	PrevOffset  int
	NextOffset  int
	SSEEntriesURL string // Full SSE URL including view/filter params
}

type menuSection struct {
	Label       string
	HasChildren bool
	Items       []menuItem
}

type menuItem struct {
	Label          string
	URL            string
	SSEURL         string
	UnreadCount    string
	Selected       bool
	ExternalIconID string
	Children       []menuItem
}

// ─── Full page handler ───────────────────────────────────────────────────

func (h *handler) showApp(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	offset := request.QueryIntParam(r, "offset", 0)
	searchQuery := request.QueryStringParam(r, "q", "")
	viewName, feedID, categoryID := parseAppRoute(r)

	vm := appViewModel{
		Language:          user.Language,
		Direction:         "ltr",
		CSRFToken:         request.WebSession(r).CSRF(),
		SearchQuery:       searchQuery,
		StyleChecksum:     dsstatic.StylesheetBundles["app"].Checksum,
		JSChecksum:        dsstatic.JavascriptBundles["datastar"].Checksum,
		KeyboardChecksum:  dsstatic.JavascriptBundles["keyboard"].Checksum,
		ComponentsChecksum: dsstatic.JavascriptBundles["components"].Checksum,
		CanMarkAllRead:    viewName == "unread" || viewName == "feed" || viewName == "category",
	}

	// Build subscription menu.
	vm.MenuSections = h.buildMenu(user, viewName, feedID, categoryID)
	vm.ListTitle = listTitleForView(viewName, feedID, categoryID, h.store, user.ID)

	// Load entries.
	entries, total, err := h.queryEntries(user.ID, viewName, feedID, categoryID, searchQuery, offset, user.EntriesPerPage)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	vm.Entries = make([]entryView, len(entries))
	for i, e := range entries {
		vm.Entries[i] = entryView{
			ID:      e.ID,
			Title:   e.Title,
			Status:  e.Status,
			Starred: e.Starred,
			Date:    e.Date,
		}
		if e.Feed != nil {
			vm.Entries[i].Feed = &feedRef{Title: e.Feed.Title, ID: e.Feed.ID}
		}
	}

	// Pagination.
	if total > user.EntriesPerPage {
		totalPages := int(math.Ceil(float64(total) / float64(user.EntriesPerPage)))
		currentPage := offset/user.EntriesPerPage + 1
		vm.Pagination = &paginationView{
			CurrentPage:   currentPage,
			TotalPages:    totalPages,
			HasPrev:       offset > 0,
			HasNext:       offset+user.EntriesPerPage < total,
			PrevOffset:    offset - user.EntriesPerPage,
			NextOffset:    offset + user.EntriesPerPage,
			SSEEntriesURL: buildSSEEntriesURL(viewName, feedID, categoryID, searchQuery),
		}
		if vm.Pagination.PrevOffset < 0 {
			vm.Pagination.PrevOffset = 0
		}
	}

	// Preload content for the first entry so the right panel isn't empty.
	if len(entries) > 0 {
		firstEntry, err := h.store.NewEntryQueryBuilder(user.ID).
			WithEntryIDs(entries[0].ID).
			WithEnclosures().
			GetEntry()
		if err == nil && firstEntry != nil {
			detail := &entryDetailView{
				ID:      firstEntry.ID,
				Title:   firstEntry.Title,
				Author:  firstEntry.Author,
				Date:    firstEntry.Date,
				Content: template.HTML(firstEntry.Content),
				Starred: firstEntry.Starred,
				URL:     firstEntry.URL,
				Status:  firstEntry.Status,
			}
			if firstEntry.Feed != nil {
				detail.Feed = &feedRef{Title: firstEntry.Feed.Title, ID: firstEntry.Feed.ID}
			}
			vm.SelectedEntry = detail
		}
	}

	// Build signals JSON.
	signals := AppSignals{
		View:       viewName,
		FeedID:     feedID,
		CategoryID: categoryID,
		Offset:     offset,
		Loading:    false,
	}
	if vm.SelectedEntry != nil {
		signals.EntryID = vm.SelectedEntry.ID
	}
	signalsJSON, _ := json.Marshal(signals)
	vm.SignalsJSON = template.JS(signalsJSON)

	var buf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&buf, "layout", vm); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("template render error: %w", err))
		return
	}
	response.HTML(w, r, buf.Bytes())
}

// ─── SSE fragment handlers ───────────────────────────────────────────────

func (h *handler) sseEntries(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	// Read from signals first, then fall back to query params.
	var req EntryRequest
	readSignals(r, &req)

	if req.View == "" {
		req.View = request.QueryStringParam(r, "view", "unread")
	}
	if req.FeedID == 0 {
		req.FeedID = request.QueryInt64Param(r, "feedId", 0)
	}
	if req.CategoryID == 0 {
		req.CategoryID = request.QueryInt64Param(r, "categoryId", 0)
	}
	if req.Offset == 0 {
		req.Offset = request.QueryIntParam(r, "offset", 0)
	}
	if req.SearchQuery == "" {
		req.SearchQuery = request.QueryStringParam(r, "searchQuery", "")
	}

	entries, total, err := h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, req.SearchQuery, req.Offset, user.EntriesPerPage)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	evs := make([]entryView, len(entries))
	for i, e := range entries {
		evs[i] = entryView{
			ID:      e.ID,
			Title:   e.Title,
			Status:  e.Status,
			Starred: e.Starred,
			Date:    e.Date,
		}
		if e.Feed != nil {
			evs[i].Feed = &feedRef{Title: e.Feed.Title, ID: e.Feed.ID}
		}
	}

	// Build fragments: entry list + optional pagination.
	fragments := []SSEFragment{}
	var listBuf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&listBuf, "entry_list", map[string]any{"Entries": evs}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_list template: %w", err))
		return
	}
	fragments = append(fragments, SSEFragment{HTML: listBuf.String(), Selector: "#entry-list"})

	if total > user.EntriesPerPage {
		totalPages := int(math.Ceil(float64(total) / float64(user.EntriesPerPage)))
		currentPage := req.Offset/user.EntriesPerPage + 1
		pv := paginationView{
			CurrentPage:   currentPage,
			TotalPages:    totalPages,
			HasPrev:       req.Offset > 0,
			HasNext:       req.Offset+user.EntriesPerPage < total,
			PrevOffset:    req.Offset - user.EntriesPerPage,
			NextOffset:    req.Offset + user.EntriesPerPage,
			SSEEntriesURL: buildSSEEntriesURL(req.View, req.FeedID, req.CategoryID, req.SearchQuery),
		}
		if pv.PrevOffset < 0 {
			pv.PrevOffset = 0
		}
		var pagBuf bytes.Buffer
		if err := h.tpl.ExecuteTemplate(&pagBuf, "pagination", pv); err != nil {
			response.HTMLServerError(w, r, fmt.Errorf("pagination template: %w", err))
			return
		}
		fragments = append(fragments, SSEFragment{HTML: pagBuf.String(), Selector: "#pagination"})
	} else {
		// Clear pagination if all entries fit on one page.
		fragments = append(fragments, SSEFragment{HTML: "", Selector: "#pagination"})
	}

	renderSSEMulti(w, r, fragments)
}

func (h *handler) sseEntry(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	entryID := request.RouteInt64Param(r, "entryID")
	entry, err := h.store.NewEntryQueryBuilder(user.ID).
		WithEntryIDs(entryID).
		WithEnclosures().
		GetEntry()
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}
	if entry == nil {
		response.HTMLNotFound(w, r)
		return
	}

	// Mark as read if configured.
	if entry.Status == model.EntryStatusUnread && user.MarkReadOnView {
		if err := h.store.SetEntriesStatus(user.ID, []int64{entry.ID}, model.EntryStatusRead); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
		entry.Status = model.EntryStatusRead
	}

	detail := &entryDetailView{
		ID:      entry.ID,
		Title:   entry.Title,
		Author:  entry.Author,
		Date:    entry.Date,
		Content: template.HTML(entry.Content),
		Starred: entry.Starred,
		URL:     entry.URL,
		Status:  entry.Status,
	}
	if entry.Feed != nil {
		detail.Feed = &feedRef{Title: entry.Feed.Title, ID: entry.Feed.ID}
	}

	// Render entry content panel and update the entry row styling.
	var contentBuf, rowBuf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&contentBuf, "entry_content", map[string]any{"SelectedEntry": detail}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_content template: %w", err))
		return
	}
	if err := h.tpl.ExecuteTemplate(&rowBuf, "entry_row", map[string]any{
		"ID":      entry.ID,
		"Title":   entry.Title,
		"Status":  entry.Status,
		"Starred": entry.Starred,
		"Date":    entry.Date,
		"Feed":    detail.Feed,
	}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_row template: %w", err))
		return
	}

	renderSSEMulti(w, r, []SSEFragment{
		{HTML: contentBuf.String(), Selector: "#entry-content"},
		{HTML: rowBuf.String(), Selector: fmt.Sprintf("#entry-row-%d", entry.ID)},
	})
}

func (h *handler) sseSubscriptions(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	sections := h.buildMenu(user, "", 0, 0)
	data := map[string]any{"MenuSections": sections}
	renderSSEFragment(w, r, h.tpl, "subscription_list", data, "#subscription-panel")
}

func (h *handler) sseToggleStar(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	entryID := request.RouteInt64Param(r, "entryID")
	entry, err := h.store.NewEntryQueryBuilder(user.ID).
		WithEntryIDs(entryID).
		GetEntry()
	if err != nil || entry == nil {
		response.HTMLNotFound(w, r)
		return
	}

	newStarred := !entry.Starred
	if err := h.store.SetEntriesStarredState(user.ID, []int64{entry.ID}, newStarred); err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	var starBuf bytes.Buffer
	starData := map[string]any{
		"ID":      entry.ID,
		"Starred": newStarred,
	}
	if err := h.tpl.ExecuteTemplate(&starBuf, "star_button", starData); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("star_button template: %w", err))
		return
	}

	sse := datastar.NewSSE(w, r)
	sse.PatchElements(starBuf.String(), datastar.WithSelector(fmt.Sprintf("#star-icon-%d", entry.ID)))
	sse.PatchElements(starBuf.String(), datastar.WithSelector(fmt.Sprintf("#toolbar-star-icon-%d", entry.ID)))
}

func (h *handler) sseToggleEntryStatus(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	var req struct {
		EntryIDs []int64 `json:"entryIds"`
	}
	if err := readSignals(r, &req); err != nil || len(req.EntryIDs) == 0 {
		response.HTMLBadRequest(w, r, fmt.Errorf("missing entryIds"))
		return
	}

	// Toggle: if the first entry is unread, mark all as read; otherwise mark as unread.
	firstEntry, err := h.store.NewEntryQueryBuilder(user.ID).
		WithEntryIDs(req.EntryIDs[0]).
		GetEntry()
	if err != nil || firstEntry == nil {
		response.HTMLNotFound(w, r)
		return
	}

	newStatus := model.EntryStatusUnread
	if firstEntry.Status == model.EntryStatusUnread {
		newStatus = model.EntryStatusRead
	}

	if newStatus == model.EntryStatusRead {
		if err := h.store.SetEntriesStatus(user.ID, req.EntryIDs, model.EntryStatusRead); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	} else {
		// Mark as unread.
		if err := h.store.SetEntriesStatus(user.ID, req.EntryIDs, model.EntryStatusUnread); err != nil {
			slog.Warn("dsui: unable to mark entries as unread", slog.Any("error", err))
		}
	}

	// Send SSE patches for each affected entry row.
	fragments := make([]SSEFragment, 0, len(req.EntryIDs))
	for _, id := range req.EntryIDs {
		entry, err := h.store.NewEntryQueryBuilder(user.ID).
			WithEntryIDs(id).
			GetEntry()
		if err != nil || entry == nil {
			continue
		}
		var rowBuf bytes.Buffer
		rowView := map[string]any{
			"ID":      entry.ID,
			"Title":   entry.Title,
			"Status":  newStatus,
			"Starred": entry.Starred,
			"Date":    entry.Date,
		}
		if entry.Feed != nil {
			rowView["Feed"] = &feedRef{Title: entry.Feed.Title, ID: entry.Feed.ID}
		}
		if err := h.tpl.ExecuteTemplate(&rowBuf, "entry_row", rowView); err != nil {
			slog.Warn("dsui: unable to render entry_row template", slog.Int64("entry_id", id), slog.Any("error", err))
			continue
		}
		fragments = append(fragments, SSEFragment{
			HTML:     rowBuf.String(),
			Selector: fmt.Sprintf("#entry-row-%d", id),
		})
	}

	// Also update the toolbar if the entry is currently displayed.
	if len(req.EntryIDs) > 0 {
		firstID := req.EntryIDs[0]
		entry, err := h.store.NewEntryQueryBuilder(user.ID).
			WithEntryIDs(firstID).
			GetEntry()
		if err == nil && entry != nil {
			toolbarHTML := buildArticleToolbar(entry)
			fragments = append(fragments, SSEFragment{
				HTML:     toolbarHTML,
				Selector: "#article-toolbar",
			})
		}
	}

	if len(fragments) > 0 {
		renderSSEMulti(w, r, fragments)
	} else {
		sendSSERedirect(w, r, "/ds/unread")
	}
}

func (h *handler) sseMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	var req EntryRequest
	if err := readSignals(r, &req); err != nil {
		req = EntryRequest{View: "unread"}
	}
	if req.View == "" {
		req.View = "unread"
	}

	if req.FeedID > 0 {
		if err := h.store.MarkFeedAsRead(user.ID, req.FeedID, time.Now()); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	} else if req.CategoryID > 0 {
		if err := h.store.MarkCategoryAsRead(user.ID, req.CategoryID, time.Now()); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	} else {
		if err := h.store.MarkAllAsRead(user.ID); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	}

	// Rebuild entry list and subscription tree for SSE patches.
	sections := h.buildMenu(user, req.View, req.FeedID, req.CategoryID)
	entries, _, err := h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, req.SearchQuery, 0, user.EntriesPerPage)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	evs := make([]entryView, len(entries))
	for i, e := range entries {
		evs[i] = entryView{
			ID:      e.ID,
			Title:   e.Title,
			Status:  e.Status,
			Starred: e.Starred,
			Date:    e.Date,
		}
		if e.Feed != nil {
			evs[i].Feed = &feedRef{Title: e.Feed.Title, ID: e.Feed.ID}
		}
	}

	var listBuf, subBuf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&listBuf, "entry_list", map[string]any{"Entries": evs}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_list template: %w", err))
		return
	}
	if err := h.tpl.ExecuteTemplate(&subBuf, "subscription_list", map[string]any{"MenuSections": sections}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("subscription_list template: %w", err))
		return
	}

	renderSSEMulti(w, r, []SSEFragment{
		{HTML: listBuf.String(), Selector: "#entry-list"},
		{HTML: subBuf.String(), Selector: "#subscription-panel .feed-tree"},
	})
}

// ─── Query helpers ───────────────────────────────────────────────────────

func (h *handler) queryEntries(userID int64, view string, feedID, categoryID int64, searchQuery string, offset, limit int) (model.Entries, int, error) {
	builder := h.store.NewEntryQueryBuilder(userID).WithGloballyVisible()

	switch view {
	case "unread":
		builder.WithStatuses(model.EntryStatusUnread)
	case "starred":
		builder.WithStarred(true)
	case "history":
		builder.WithStatuses(model.EntryStatusRead)
	case "search":
		if searchQuery != "" {
			builder.WithSearchQuery(searchQuery)
		}
	case "feed":
		builder.WithFeedID(feedID)
	case "category":
		builder.WithCategoryID(categoryID)
	default:
		builder.WithStatuses(model.EntryStatusUnread)
	}

	builder.
		WithSorting("published_at", "desc").
		WithSorting("id", "desc").
		WithOffset(offset).
		WithLimit(limit).
		WithoutContent()

	return builder.GetEntriesWithCount()
}

// ─── Menu builder ────────────────────────────────────────────────────────

func (h *handler) buildMenu(user *model.User, activeView string, activeFeedID, activeCategoryID int64) []menuSection {
	var sections []menuSection

	nav, err := h.store.GetNavMetadata(user.ID)
	if err != nil {
		slog.Warn("dsui: unable to get nav metadata", slog.Any("error", err))
		return sections
	}

	// Standard views.
	sections = append(sections, menuSection{
		Label: "",
		Items: []menuItem{
			{Label: "All", URL: "/ds/unread", SSEURL: "/ds/sse/entries", UnreadCount: fmt.Sprintf("%d", nav.CountUnread), Selected: activeView == "unread"},
			{Label: "Starred", URL: "/ds/starred", SSEURL: "/ds/sse/entries?view=starred", Selected: activeView == "starred"},
			{Label: "History", URL: "/ds/history", SSEURL: "/ds/sse/entries?view=history", Selected: activeView == "history"},
		},
	})

	// Categories with their feeds.
		categories, err := h.store.CategoriesWithFeedCount(user.ID, "title")
		if err != nil {
		slog.Warn("dsui: unable to get categories", slog.Any("error", err))
		return sections
	}

	if len(categories) > 0 {
		for _, cat := range categories {
			feeds, err := h.store.FeedsByCategoryWithCounters(user.ID, cat.ID)
			if err != nil {
				slog.Warn("dsui: unable to get feeds for category", slog.Int64("category_id", cat.ID), slog.Any("error", err))
				continue
			}

			var children []menuItem
			for _, f := range feeds {
				count := ""
				if f.UnreadCount > 0 {
					count = fmt.Sprintf("%d", f.UnreadCount)
				}
				children = append(children, menuItem{
					Label:          f.Title,
					URL:            fmt.Sprintf("/ds/feed/%d", f.ID),
					SSEURL:         fmt.Sprintf("/ds/sse/entries?feedId=%d", f.ID),
					UnreadCount:    count,
					Selected:       activeView == "feed" && activeFeedID == f.ID,
					ExternalIconID: externalIconID(f),
				})
			}

			catCount := ""
			if cat.TotalUnread != nil && *cat.TotalUnread > 0 {
				catCount = fmt.Sprintf("%d", *cat.TotalUnread)
			}
		sections = append(sections, menuSection{
			Label:       cat.Title,
			HasChildren: true,
			Items: []menuItem{{
					Label:       cat.Title,
					URL:         fmt.Sprintf("/ds/category/%d", cat.ID),
					SSEURL:      fmt.Sprintf("/ds/sse/entries?categoryId=%d", cat.ID),
					UnreadCount: catCount,
					Selected:    activeView == "category" && activeCategoryID == cat.ID,
					Children:    children,
				}},
			})
		}
	}

	return sections
}

// externalIconID extracts the external icon ID from a feed for use in URLs.
func externalIconID(f *model.Feed) string {
	if f.Icon != nil && f.Icon.ExternalIconID != "" {
		return f.Icon.ExternalIconID
	}
	return ""
}

// ─── Route parsing ───────────────────────────────────────────────────────

func parseAppRoute(r *http.Request) (view string, feedID, categoryID int64) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/ds/starred"):
		return "starred", 0, 0
	case strings.HasPrefix(path, "/ds/history"):
		return "history", 0, 0
	case strings.HasPrefix(path, "/ds/search"):
		return "search", 0, 0
	case strings.HasPrefix(path, "/ds/feed/"):
		feedID = request.RouteInt64Param(r, "feedID")
		return "feed", feedID, 0
	case strings.HasPrefix(path, "/ds/category/"):
		categoryID = request.RouteInt64Param(r, "categoryID")
		return "category", 0, categoryID
	default:
		return "unread", 0, 0
	}
}

func listTitleForView(view string, feedID, categoryID int64, store *storage.Storage, userID int64) string {
	switch view {
	case "starred":
		return "Starred"
	case "history":
		return "History"
	case "search":
		return "Search"
	case "feed":
		f, err := store.FeedByID(userID, feedID)
		if err == nil && f != nil {
			return f.Title
		}
		return "Feed"
	case "category":
		c, err := store.Category(userID, categoryID)
		if err == nil && c != nil {
			return c.Title
		}
		return "Category"
	default:
		return "Unread"
	}
}

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

// buildSSEEntriesURL creates the base SSE URL for loading entries
// with the current view/filter parameters.
func buildSSEEntriesURL(view string, feedID, categoryID int64, searchQuery string) string {
	u := "/ds/sse/entries?view=" + view
	if feedID > 0 {
		u += fmt.Sprintf("&feedId=%d", feedID)
	}
	if categoryID > 0 {
		u += fmt.Sprintf("&categoryId=%d", categoryID)
	}
	if searchQuery != "" {
		u += "&searchQuery=" + searchQuery
	}
	return u
}

// ─── Time helpers ────────────────────────────────────────────────────────

func elapsedTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m"
		}
		return fmt.Sprintf("%dm", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h"
		}
		return fmt.Sprintf("%dh", h)
	case d < 7*24*time.Hour:
		day := int(d.Hours() / 24)
		if day == 1 {
			return "1d"
		}
		return fmt.Sprintf("%dd", day)
	case d < 30*24*time.Hour:
		w := int(d.Hours() / (24 * 7))
		if w == 1 {
			return "1w"
		}
		return fmt.Sprintf("%dw", w)
	case d < 365*24*time.Hour:
		m := int(d.Hours() / (24 * 30))
		if m == 1 {
			return "1mo"
		}
		return fmt.Sprintf("%dmo", m)
	default:
		y := int(d.Hours() / (24 * 365))
		if y == 1 {
			return "1y"
		}
		return fmt.Sprintf("%dy", y)
	}
}

// buildArticleToolbar renders an HTML toolbar for the article content panel.
func buildArticleToolbar(entry *model.Entry) string {
	toggleLabel := "Mark read"
	if entry.Status == model.EntryStatusRead {
		toggleLabel = "Mark unread"
	}
	starIcon := "☆"
	starLabel := "Star"
	starClass := ""
	if entry.Starred {
		starIcon = "★"
		starLabel = "Starred"
		starClass = "starred"
	}
	return fmt.Sprintf(`<div id="article-toolbar" class="article-toolbar">
    <button class="toolbar-btn"
        data-on:click="@post('/ds/sse/entry/star/%d')">
        <span id="toolbar-star-icon-%d" class="%s">%s</span>
        <span>%s</span>
    </button>
    <a href="%s" target="_blank" rel="noopener" class="toolbar-btn no-underline">
        Open original
    </a>
    <button class="toolbar-btn"
        data-signals-entryIds="[%d]"
        data-on:click="@post('/ds/sse/entry/status')">
        %s
    </button>
</div>`, entry.ID, entry.ID, starClass, starIcon, starLabel,
		entry.URL, entry.ID, toggleLabel)
}
