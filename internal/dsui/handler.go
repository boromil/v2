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
	mux.HandleFunc("GET /ds/feed/{feedID}", h.showApp)
	mux.HandleFunc("GET /ds/category/{categoryID}", h.showApp)

	// SSE fragment endpoints.
	mux.HandleFunc("GET /ds/sse/entries", h.sseEntries)
	mux.HandleFunc("GET /ds/sse/entry/{entryID}", h.sseEntry)
	mux.HandleFunc("GET /ds/sse/subscriptions", h.sseSubscriptions)
	mux.HandleFunc("POST /ds/sse/entry/star/{entryID}", h.sseToggleStar)
	mux.HandleFunc("POST /ds/sse/entry/status", h.sseToggleEntryStatus)
	mux.HandleFunc("POST /ds/sse/mark-all-read", h.sseMarkAllRead)

	// Apply session middleware.
	return sessionMiddleware(store)(mux)
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
	asset, ok := dsstatic.JavascriptBundles["datastar"]
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
	Title         string
	Language      string
	Direction     string
	StyleChecksum string
	JSChecksum    string
	SignalsJSON   template.JS
	ListTitle     string
	CanMarkAllRead bool
	Entries       []entryView
	SelectedEntry *entryDetailView
	Pagination    *paginationView
	MenuSections  []menuSection
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
}

type menuSection struct {
	Label string
	Items []menuItem
}

type menuItem struct {
	Label       string
	URL         string
	SSEURL      string
	UnreadCount string
	Selected    bool
	Children    []menuItem
}

// ─── Full page handler ───────────────────────────────────────────────────

func (h *handler) showApp(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	offset := request.QueryIntParam(r, "offset", 0)
	viewName, feedID, categoryID := parseAppRoute(r)

	vm := appViewModel{
		Language:      user.Language,
		Direction:     "ltr",
		StyleChecksum: dsstatic.StylesheetBundles["app"].Checksum,
		JSChecksum:    dsstatic.JavascriptBundles["datastar"].Checksum,
		CanMarkAllRead: viewName == "unread" || viewName == "feed" || viewName == "category",
	}

	// Build subscription menu.
	vm.MenuSections = h.buildMenu(user, viewName, feedID, categoryID)
	vm.ListTitle = listTitleForView(viewName, feedID, categoryID, h.store, user.ID)

	// Load entries.
	entries, total, err := h.queryEntries(user.ID, viewName, feedID, categoryID, offset, user.EntriesPerPage)
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
			CurrentPage: currentPage,
			TotalPages:  totalPages,
			HasPrev:     offset > 0,
			HasNext:     offset+user.EntriesPerPage < total,
			PrevOffset:  offset - user.EntriesPerPage,
			NextOffset:  offset + user.EntriesPerPage,
		}
		if vm.Pagination.PrevOffset < 0 {
			vm.Pagination.PrevOffset = 0
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

	entries, _, err := h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, req.Offset, user.EntriesPerPage)
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

	data := map[string]any{"Entries": evs}
	renderSSEFragment(w, r, h.tpl, "entry_list", data, "#entry-list")
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
	if err := h.store.SetEntriesStatus(user.ID, []int64{entry.ID}, model.EntryStatusUnread); err != nil {
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

	// Render entry content into #entry-content.
	data := map[string]any{"SelectedEntry": detail}
	renderSSEFragment(w, r, h.tpl, "entry_content", data, "#entry-content")

	// Optionally also update the entry row in the list.
	rowData := map[string]any{
		"Status":  entry.Status,
		"Starred": entry.Starred,
	}
	var rowBuf bytes.Buffer
	if err := h.tpl.ExecuteTemplate(&rowBuf, "entry_row", map[string]any{
		"ID":      entry.ID,
		"Title":   entry.Title,
		"Status":  entry.Status,
		"Starred": entry.Starred,
		"Date":    entry.Date,
		"Feed":    detail.Feed,
	}); err == nil {
		// Send additional patch for the entry row (update read/unread styling).
		_ = rowBuf
		_ = rowData
	}
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

	// Return SSE to patch the star button.
	sse := datastar.NewSSE(w, r)
	starHTML := fmt.Sprintf(`<button class="star-btn %s flex-shrink-0"
		data-on:click="@post('/ds/sse/entry/star/%d')"
		data-starred="%v"
		id="star-icon-%d">%s</button>`,
		map[bool]string{true: "starred", false: ""}[newStarred],
		entryID, newStarred, entryID,
		map[bool]string{true: "★", false: "☆"}[newStarred],
	)
	sse.PatchElements(starHTML, datastar.WithSelector(fmt.Sprintf("#star-icon-%d", entryID)))
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

	// Patch each entry row to update its read/unread class.
	sse := datastar.NewSSE(w, r)
	for _, id := range req.EntryIDs {
		sel := fmt.Sprintf("#entry-row-%d", id)
		if newStatus == model.EntryStatusRead {
			sse.PatchElements("", datastar.WithSelector(sel+" .entry-title"))
			// Instead of patching individual classes, just use JS to reflect state.
		}
	}
	// For now, send a script to refresh the entry list.
	sse.ExecuteScript("document.querySelectorAll('.entry-row').forEach(el => { const id = el.dataset.id; el.classList.toggle('read', true); el.classList.toggle('unread', false); })")
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

	// Refresh subscriptions (to update unread counts) and entry list.
	sections := h.buildMenu(user, req.View, req.FeedID, req.CategoryID)
	entries, _, _ := h.queryEntries(user.ID, req.View, req.FeedID, req.CategoryID, 0, user.EntriesPerPage)
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

	_ = sections
	_ = evs
	// For now, redirect to refresh the page.
	sse := datastar.NewSSE(w, r)
	sse.ExecuteScript("window.location.reload()")
}

// ─── Query helpers ───────────────────────────────────────────────────────

func (h *handler) queryEntries(userID int64, view string, feedID, categoryID int64, offset, limit int) (model.Entries, int, error) {
	builder := h.store.NewEntryQueryBuilder(userID).WithGloballyVisible()

	switch view {
	case "unread":
		builder.WithStatuses(model.EntryStatusUnread)
	case "starred":
		builder.WithStarred(true)
	case "history":
		builder.WithStatuses(model.EntryStatusRead)
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
					Label:       f.Title,
					URL:         fmt.Sprintf("/ds/feed/%d", f.ID),
					SSEURL:      fmt.Sprintf("/ds/sse/entries?feedId=%d", f.ID),
					UnreadCount: count,
					Selected:    activeView == "feed" && activeFeedID == f.ID,
				})
			}

			catCount := ""
			if cat.TotalUnread != nil && *cat.TotalUnread > 0 {
				catCount = fmt.Sprintf("%d", *cat.TotalUnread)
			}
			sections = append(sections, menuSection{
				Label: cat.Title,
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

// ─── Route parsing ───────────────────────────────────────────────────────

func parseAppRoute(r *http.Request) (view string, feedID, categoryID int64) {
	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/ds/starred"):
		return "starred", 0, 0
	case strings.HasPrefix(path, "/ds/history"):
		return "history", 0, 0
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
	return false
}

func dsLoginRedirectURL(requestURI string) string {
	loginURL, _ := url.Parse("/")
	values := loginURL.Query()
	values.Set("redirect_url", requestURI)
	loginURL.RawQuery = values.Encode()
	return loginURL.String()
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
