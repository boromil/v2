// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"runtime"
	"strings"
	"time"

	"miniflux.app/v2/internal/config"
	dsstatic "miniflux.app/v2/internal/dsui/static"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/version"
)

// pageViewModel carries the shared chrome plus the data of a standalone page.
// PageTemplate names the template the layout renders instead of the app view.
type pageViewModel struct {
	appViewModel
	PageTemplate     string
	PageHeading      string
	About            *aboutView
	Sessions         []sessionView
	CurrentSessionID string
}

type aboutView struct {
	Version         string
	VersionURL      string
	Commit          string
	CommitURL       string
	BuildDate       string
	GoVersion       string
	DatabaseType    string
	DatabaseVersion string
	DBUsage         string
	IsAdmin         bool
	GlobalConfig    []configOption
}

type configOption struct {
	Key   string
	Value string
}

type sessionView struct {
	ID        string
	CreatedAt time.Time
	IP        string
	UserAgent string
	Current   bool
}

// newPageViewModel fills the fields every full-page render needs.
func (h *handler) newPageViewModel(r *http.Request, user *model.User) appViewModel {
	nav, _ := h.store.GetNavMetadata(user.ID)

	return appViewModel{
		Language:           user.Language,
		Direction:          "ltr",
		ThemeClass:         themeClass(user.Theme),
		ThemeFont:          themeFont(user.Theme),
		CSRFToken:          request.WebSession(r).CSRF(),
		StyleChecksum:      dsstatic.StylesheetBundles["app"].Checksum,
		JSChecksum:         dsstatic.JavascriptBundles["datastar"].Checksum,
		KeyboardChecksum:   dsstatic.JavascriptBundles["keyboard"].Checksum,
		ComponentsChecksum: dsstatic.JavascriptBundles["components"].Checksum,
		KeyboardShortcuts:  user.KeyboardShortcuts,
		IsAdmin:            user.IsAdmin,
		APIEnabled:         config.Opts.HasAPI(),
		CountUnread:        nav.CountUnread,
		CountErrorFeeds:    nav.CountErrorFeeds,
	}
}

// renderPage renders a standalone page: the page template becomes the body
// content wrapped by the shared layout.
func (h *handler) renderPage(w http.ResponseWriter, r *http.Request, vm pageViewModel) {
	tpl := h.tplFor(vm.Language)

	var content bytes.Buffer
	if err := tpl.ExecuteTemplate(&content, vm.PageTemplate, vm); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("template render error: %w", err))
		return
	}

	vm.PageContent = template.HTML(content.String())

	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout", vm); err != nil {
		response.HTMLServerError(w, r, fmt.Errorf("template render error: %w", err))
		return
	}
	response.HTML(w, r, buf.Bytes())
}

// showAbout renders the version and database information page.
func (h *handler) showAbout(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	about := &aboutView{
		Version:         version.Version,
		Commit:          version.Commit,
		BuildDate:       version.BuildDate,
		GoVersion:       runtime.Version(),
		DatabaseType:    h.store.DatabaseTypeName(),
		DatabaseVersion: h.store.DatabaseVersion(),
		IsAdmin:         user.IsAdmin,
	}

	if !strings.Contains(version.Version, "dev") {
		about.VersionURL = "https://github.com/miniflux/v2/releases/tag/" + version.Version
	}
	if !strings.HasPrefix(version.Commit, "Unknown") {
		about.CommitURL = "https://github.com/miniflux/v2/commit/" + version.Commit
	}

	if dbSize, err := h.store.DBSize(); err != nil {
		about.DBUsage = err.Error()
	} else {
		about.DBUsage = dbSize
	}

	if user.IsAdmin {
		for _, option := range config.Opts.ConfigMap(true) {
			about.GlobalConfig = append(about.GlobalConfig, configOption{Key: option.Key, Value: option.Value})
		}
	}

	vm := pageViewModel{
		appViewModel: h.newPageViewModel(r, user),
		PageTemplate: "about",
		PageHeading:  printerFor(user.Language).Print("page.about.title"),
		About:        about,
	}
	vm.Title = printerFor(user.Language).Print("page.about.title") + " — Miniflux"

	h.renderPage(w, r, vm)
}

// showSessions renders the list of browser sessions of the user.
func (h *handler) showSessions(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	sessions, err := h.store.WebSessionsByUserID(user.ID)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	currentSessionID := request.WebSession(r).ID
	views := make([]sessionView, 0, len(sessions))
	for _, session := range sessions {
		session.UseTimezone(user.Timezone)
		views = append(views, sessionView{
			ID:        session.ID,
			CreatedAt: session.CreatedAt,
			IP:        session.IP,
			UserAgent: session.UserAgent,
			Current:   session.ID == currentSessionID,
		})
	}

	vm := pageViewModel{
		appViewModel:     h.newPageViewModel(r, user),
		PageTemplate:     "sessions",
		PageHeading:      printerFor(user.Language).Print("page.sessions.title"),
		Sessions:         views,
		CurrentSessionID: currentSessionID,
	}
	vm.Title = printerFor(user.Language).Print("page.sessions.title") + " — Miniflux"

	h.renderPage(w, r, vm)
}

// removeSession revokes one of the user's browser sessions.
func (h *handler) removeSession(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	sessionID := r.PathValue("sessionID")
	if sessionID == "" {
		response.HTMLRedirect(w, r, "/ds/sessions")
		return
	}

	// Never revoke the session that issued the request: the user would be
	// logged out of the page they are on.
	if sessionID == request.WebSession(r).ID {
		response.HTMLRedirect(w, r, "/ds/sessions")
		return
	}

	if err := h.store.RemoveUserWebSession(user.ID, sessionID); err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	response.HTMLRedirect(w, r, "/ds/sessions")
}
