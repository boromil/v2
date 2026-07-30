// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/mediaproxy"
	"miniflux.app/v2/internal/proxyrotator"
	"miniflux.app/v2/internal/reader/fetcher"
	"miniflux.app/v2/internal/reader/opml"
	"miniflux.app/v2/internal/reader/processor"
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
	mux.HandleFunc("GET /ds/settings", h.showSettings)
	mux.HandleFunc("GET /ds/feed/{feedID}", h.showApp)
	mux.HandleFunc("GET /ds/category/{categoryID}", h.showApp)

	// OPML import (regular form POST).
	mux.HandleFunc("POST /ds/import-opml", h.importOPML)
	mux.HandleFunc("POST /ds/fetch-opml", h.fetchOPML)

	// SSE fragment endpoints.
	mux.HandleFunc("GET /ds/sse/entries", h.sseEntries)
	mux.HandleFunc("GET /ds/sse/subscriptions", h.sseSubscriptions)
	mux.HandleFunc("POST /ds/refresh", h.refreshFeeds)
	mux.HandleFunc("POST /ds/refresh/{feedID}", h.refreshFeed)
	mux.HandleFunc("GET /ds/sse/entry/{entryID}", h.sseEntry)
	mux.HandleFunc("POST /ds/sse/entry/star/{entryID}", h.sseToggleStar)
	mux.HandleFunc("POST /ds/sse/entry/status", h.sseToggleEntryStatus)
	mux.HandleFunc("POST /ds/sse/mark-all-read", h.sseMarkAllRead)
	mux.HandleFunc("POST /ds/sse/mark-page-read", h.sseMarkPageRead)
	mux.HandleFunc("POST /ds/sse/fetch-content/{entryID}", h.sseFetchContent)
	mux.HandleFunc("POST /ds/sse/share/{entryID}", h.sseToggleShare)
	mux.HandleFunc("POST /ds/sse/settings", h.sseSaveSettings)

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
		"templates/settings.html",
		"templates/components/subscription_list.html",
		"templates/components/entry_list.html",
		"templates/components/entry_row.html",
		"templates/components/entry_content.html",
		"templates/components/article_toolbar.html",
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
	ViewName           string
	FeedID             int64
	CategoryID         int64
	Offset             int
	CountUnread        int
	CountErrorFeeds    int
	StyleChecksum      string
	JSChecksum         string
	KeyboardChecksum   string
	ComponentsChecksum string
	ListTitle          string
	CanMarkAllRead     bool
	Entries            []entryView
	SelectedEntry      *entryDetailView
	Pagination         *paginationView
	MenuSections       []menuSection
	IsSettings         bool
	Form               *settingsFormData
	Themes             []selectOption
	Languages          []selectOption
	Timezones          []selectOption
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
	ID         int64
	Title      string
	Author     string
	Date       time.Time
	Content    template.HTML
	Starred    bool
	URL        string
	Feed       *feedRef
	Status     string
	ShareCode  string
	Enclosures []enclosureView
}

type enclosureView struct {
	URL      string
	MimeType string
	Size     int64
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
		ViewName:          viewName,
		FeedID:            feedID,
		CategoryID:        categoryID,
		Offset:            offset,
		StyleChecksum:     dsstatic.StylesheetBundles["app"].Checksum,
		JSChecksum:        dsstatic.JavascriptBundles["datastar"].Checksum,
		KeyboardChecksum:  dsstatic.JavascriptBundles["keyboard"].Checksum,
		ComponentsChecksum: dsstatic.JavascriptBundles["components"].Checksum,
		CanMarkAllRead:    viewName == "unread" || viewName == "feed" || viewName == "category",
	}

	// Build subscription menu.
	vm.MenuSections = h.buildMenu(user, viewName, feedID, categoryID)
	vm.ListTitle = listTitleForView(viewName, feedID, categoryID, h.store, user.ID)
	nav, _ := h.store.GetNavMetadata(user.ID)
	vm.CountUnread = nav.CountUnread
	vm.CountErrorFeeds = nav.CountErrorFeeds

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
			detail := entryToDetailView(firstEntry)
			vm.SelectedEntry = detail
		}
	}

	var buf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&buf, "layout", vm); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("template render error: %w", err))
		return
	}
	response.HTML(w, r, buf.Bytes())
}

func (h *handler) showSettings(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	form := settingsFormFromUser(user)

	vm := appViewModel{
		Language:           user.Language,
		Direction:          "ltr",
		CSRFToken:          request.WebSession(r).CSRF(),
		StyleChecksum:      dsstatic.StylesheetBundles["app"].Checksum,
		JSChecksum:         dsstatic.JavascriptBundles["datastar"].Checksum,
		KeyboardChecksum:   dsstatic.JavascriptBundles["keyboard"].Checksum,
		ComponentsChecksum: dsstatic.JavascriptBundles["components"].Checksum,
		Title:              "Settings — Miniflux",
		IsSettings:         true,
		Form:               form,
		Themes:             themeOptions(),
		Languages:          languageOptions(),
		Timezones:          timezoneOptions(),
	}

	var buf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&buf, "layout", vm); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("template render error: %w", err))
		return
	}
	response.HTML(w, r, buf.Bytes())
}

func (h *handler) sseSaveSettings(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	// Read settings from Datastar signals (JSON body) or form values.
	var form *settingsFormData
	if r.Header.Get("Datastar-Request") != "" {
		form = &settingsFormData{}
		if err := readSignals(r, form); err != nil {
			slog.Warn("dsui: failed to read settings signals", slog.Any("error", err))
			sse := datastar.NewSSE(w, r)
			sse.MarshalAndPatchSignals(map[string]any{"importError": "Invalid settings data"})
			return
		}
		slog.Info("dsui: settings saved via signals", slog.String("language", form.Language), slog.String("theme", form.Theme))
	} else {
		// Fallback: regular form POST (password change form)
		form = parseSettingsForm(r)
	}
	if form.Password != "" && form.Password != form.Confirmation {
		sse := datastar.NewSSE(w, r)
		sse.MarshalAndPatchSignals(map[string]any{"importError": "Passwords do not match"})
		return
	}

	form.applyToUser(user)
	if err := h.store.UpdateUser(user); err != nil {
		sse := datastar.NewSSE(w, r)
		sse.MarshalAndPatchSignals(map[string]any{"importError": "Failed to save settings"})
		return
	}

	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(map[string]any{"importSuccess": "Settings saved"})
}

func (h *handler) fetchOPML(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	opmlURL := strings.TrimSpace(r.FormValue("url"))
	if opmlURL == "" {
		response.HTMLRedirect(w, r, "/ds/settings")
		return
	}

	slog.Info("dsui: fetching OPML from URL",
		slog.Int64("user_id", user.ID),
		slog.String("url", opmlURL),
	)

	requestBuilder := fetcher.NewRequestBuilder().
		WithTimeout(config.Opts.HTTPClientTimeout()).
		WithProxyRotator(proxyrotator.ProxyRotatorInstance)

	responseHandler := fetcher.NewResponseHandler(requestBuilder.ExecuteRequest(opmlURL))
	defer responseHandler.Close()

	if localizedError := responseHandler.LocalizedError(); localizedError != nil {
		slog.Warn("dsui: unable to fetch OPML", slog.String("url", opmlURL), slog.Any("error", localizedError.Error()))
		response.HTMLRedirect(w, r, "/ds/settings")
		return
	}

	if impErr := opml.NewHandler(h.store).Import(user.ID, responseHandler.Body(config.Opts.HTTPClientMaxBodySize())); impErr != nil {
		slog.Error("dsui: OPML import failed", slog.Any("error", impErr))
	}

	response.HTMLRedirect(w, r, "/ds/unread")
}

func (h *handler) refreshFeeds(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	slog.Info("dsui: refresh feeds triggered", slog.Int64("user_id", user.ID))

	jobs, err := h.store.NewBatchBuilder().
		WithoutDisabledFeeds().
		WithUserID(user.ID).
		FetchJobs()
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	go h.pool.Push(jobs)

	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(map[string]any{
		"refreshMessage": fmt.Sprintf("Refreshing %d feeds...", len(jobs)),
	})
}

func (h *handler) refreshFeed(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	feedID := request.RouteInt64Param(r, "feedID")
	slog.Info("dsui: refresh single feed", slog.Int64("user_id", user.ID), slog.Int64("feed_id", feedID))

	// Build a single job for this feed.
	h.pool.Push(model.JobList{{FeedID: feedID, UserID: user.ID}})

	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(map[string]any{
		"refreshMessage": "Refreshing feed...",
	})
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

	renderSSEResponse(w, r, SSEResponse{
		Fragments: fragments,
	})
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

	detail := entryToDetailView(entry)

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

	renderSSEResponse(w, r, SSEResponse{
		Fragments: []SSEFragment{
			{HTML: contentBuf.String(), Selector: "#entry-content"},
			{HTML: rowBuf.String(), Selector: fmt.Sprintf("#entry-row-%d", entry.ID)},
		},
		Signals: map[string]any{
			"selectedEntryId": entry.ID,
		},
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
	sse.MarshalAndPatchSignals(map[string]any{
		"starred": newStarred,
	})
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
			var toolbarBuf bytes.Buffer
			toolbarData := map[string]any{
				"ID":      entry.ID,
				"Starred": entry.Starred,
				"URL":     entry.URL,
				"Status":  entry.Status,
			}
			if err := h.tpl.ExecuteTemplate(&toolbarBuf, "article_toolbar", toolbarData); err == nil {
				fragments = append(fragments, SSEFragment{
					HTML:     toolbarBuf.String(),
					Selector: "#article-toolbar",
				})
			}
		}
	}

	if len(fragments) > 0 {
	renderSSEResponse(w, r, SSEResponse{
		Fragments: fragments,
	})
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

	renderSSEResponse(w, r, SSEResponse{
		Fragments: []SSEFragment{
			{HTML: listBuf.String(), Selector: "#entry-list"},
			{HTML: subBuf.String(), Selector: "#subscription-panel .feed-tree"},
		},
		Signals: map[string]any{
			"countUnread": 0,
		},
	})
}

func (h *handler) importOPML(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		slog.Error("dsui: OPML file upload error",
			slog.Int64("user_id", user.ID),
			slog.Any("error", err),
		)
		response.HTMLRedirect(w, r, "/ds/unread")
		return
	}
	defer file.Close()

	slog.Info("dsui: OPML file imported",
		slog.Int64("user_id", user.ID),
		slog.String("file_name", fileHeader.Filename),
		slog.Int64("file_size", fileHeader.Size),
	)

	if fileHeader.Size == 0 {
		response.HTMLRedirect(w, r, "/ds/unread")
		return
	}

	if impErr := opml.NewHandler(h.store).Import(user.ID, file); impErr != nil {
		slog.Error("dsui: OPML import failed",
			slog.Int64("user_id", user.ID),
			slog.Any("error", impErr),
		)
	}

	response.HTMLRedirect(w, r, "/ds/unread")
}

func (h *handler) sseMarkPageRead(w http.ResponseWriter, r *http.Request) {
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

	// Load current page entries and mark them as read.
	entries, _, err := h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, req.SearchQuery, req.Offset, user.EntriesPerPage)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	if len(ids) > 0 {
		if err := h.store.SetEntriesStatus(user.ID, ids, model.EntryStatusRead); err != nil {
			slog.Warn("dsui: unable to mark page read", slog.Any("error", err))
		}
	}

	// Reload page for updated statuses and patch.
	entries, _, _ = h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, req.SearchQuery, req.Offset, user.EntriesPerPage)
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

	var listBuf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&listBuf, "entry_list", map[string]any{"Entries": evs}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_list template: %w", err))
		return
	}

	nav, _ := h.store.GetNavMetadata(user.ID)
	sse := datastar.NewSSE(w, r)
	sse.PatchElements(listBuf.String(), datastar.WithSelector("#entry-list"))
	sse.MarshalAndPatchSignals(map[string]any{"countUnread": nav.CountUnread})
}

func (h *handler) sseFetchContent(w http.ResponseWriter, r *http.Request) {
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

	feed, err := h.store.NewFeedQueryBuilder(user.ID).
		WithFeedID(entry.FeedID).
		GetFeed()
	if err != nil || feed == nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if err := processor.ProcessEntryWebPage(feed, entry, user); err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if err := h.store.UpdateEntryTitleAndContent(entry); err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	content := mediaproxy.RewriteDocumentWithRelativeProxyURL(entry.Content)
	detail := &entryDetailView{
		ID:      entry.ID,
		Title:   entry.Title,
		Author:  entry.Author,
		Date:    entry.Date,
		Content: template.HTML(content),
		Starred: entry.Starred,
		URL:     entry.URL,
		Status:  entry.Status,
	}
	if entry.Feed != nil {
		detail.Feed = &feedRef{Title: entry.Feed.Title, ID: entry.Feed.ID}
	}

	var buf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&buf, "entry_content", map[string]any{"SelectedEntry": detail}); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("entry_content template: %w", err))
		return
	}

	sse := datastar.NewSSE(w, r)
	sse.PatchElements(buf.String(), datastar.WithSelector("#entry-content"))
}

func (h *handler) sseToggleShare(w http.ResponseWriter, r *http.Request) {
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

	if entry.ShareCode != "" {
		if err := h.store.UnshareEntry(user.ID, entry.ID); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	} else {
		if _, err := h.store.EntryShareCode(user.ID, entry.ID); err != nil {
			response.HTMLServerError(w, r, err)
			return
		}
	}

	// Re-fetch to get updated share state.
	entry, _ = h.store.NewEntryQueryBuilder(user.ID).
		WithEntryIDs(entryID).
		GetEntry()

	sse := datastar.NewSSE(w, r)
	sse.MarshalAndPatchSignals(map[string]any{
		"shared": entry.ShareCode != "",
	})
}

// ─── Query helpers ───────────────────────────────────────────────────────

func entryToDetailView(entry *model.Entry) *entryDetailView {
	d := &entryDetailView{
		ID:        entry.ID,
		Title:     entry.Title,
		Author:    entry.Author,
		Date:      entry.Date,
		Content:   template.HTML(entry.Content),
		Starred:   entry.Starred,
		URL:       entry.URL,
		Status:    entry.Status,
		ShareCode: entry.ShareCode,
	}
	if entry.Feed != nil {
		d.Feed = &feedRef{Title: entry.Feed.Title, ID: entry.Feed.ID}
	}
	for _, enc := range entry.Enclosures {
		d.Enclosures = append(d.Enclosures, enclosureView{
			URL:      enc.URL,
			MimeType: enc.MimeType,
			Size:     enc.Size,
		})
	}
	return d
}

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

