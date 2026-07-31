// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"net/http"
	"sort"
	"strconv"

	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/timezone"
)

// selectOption represents a select dropdown option.
type selectOption struct {
	Value string
	Label string
}

// settingsFormData holds the form values for the settings page.
// JSON tags match Datastar signal names (camelCase from data-bind).
type settingsFormData struct {
	Username                  string  `json:"username"`
	Password                  string  `json:"password"`
	Confirmation              string  `json:"confirmation"`
	Theme                     string  `json:"theme"`
	Language                  string  `json:"language"`
	Timezone                  string  `json:"timezone"`
	EntryDirection            string  `json:"entryDirection"`
	EntryOrder                string  `json:"entryOrder"`
	EntriesPerPage            int     `json:"entriesPerPage"`
	DefaultHomePage           string  `json:"defaultHomePage"`
	CategoriesSortingOrder    string  `json:"categoriesSortingOrder"`
	DisplayMode               string  `json:"displayMode"`
	GestureNav                string  `json:"gestureNav"`
	DefaultReadingSpeed       int     `json:"defaultReadingSpeed"`
	CJKReadingSpeed           int     `json:"cjkReadingSpeed"`
	MediaPlaybackRate         float64 `json:"mediaPlaybackRate"`
	ShowReadingTime           bool    `json:"showReadingTime"`
	KeyboardShortcuts         bool    `json:"keyboardShortcuts"`
	EntrySwipe                bool    `json:"entrySwipe"`
	AlwaysOpenExternalLinks   bool    `json:"alwaysOpenExternalLinks"`
	OpenExternalLinksInNewTab bool    `json:"openExternalLinksInNewTab"`
	AutoFetchShortEntries     bool    `json:"autoFetchShortEntries"`
	BlockFilterEntryRules     string  `json:"blockFilterEntryRules"`
	KeepFilterEntryRules      string  `json:"keepFilterEntryRules"`
	Stylesheet                string  `json:"stylesheet"`
	CustomJS                  string  `json:"customJs"`
	ExternalFontHosts         string  `json:"externalFontHosts"`
	MarkReadBehavior          string  `json:"markReadBehavior"`
}

func settingsFormFromUser(user *model.User) *settingsFormData {
	return &settingsFormData{
		Username:                  user.Username,
		Theme:                     user.Theme,
		Language:                  user.Language,
		Timezone:                  user.Timezone,
		EntryDirection:            user.EntryDirection,
		EntryOrder:                user.EntryOrder,
		EntriesPerPage:            user.EntriesPerPage,
		DefaultHomePage:           user.DefaultHomePage,
		CategoriesSortingOrder:    user.CategoriesSortingOrder,
		DisplayMode:               user.DisplayMode,
		GestureNav:                user.GestureNav,
		DefaultReadingSpeed:       user.DefaultReadingSpeed,
		CJKReadingSpeed:           user.CJKReadingSpeed,
		MediaPlaybackRate:         user.MediaPlaybackRate,
		ShowReadingTime:           user.ShowReadingTime,
		KeyboardShortcuts:         user.KeyboardShortcuts,
		EntrySwipe:                user.EntrySwipe,
		AlwaysOpenExternalLinks:   user.AlwaysOpenExternalLinks,
		OpenExternalLinksInNewTab: user.OpenExternalLinksInNewTab,
		AutoFetchShortEntries:     user.AutoFetchShortEntries,
		BlockFilterEntryRules:     user.BlockFilterEntryRules,
		KeepFilterEntryRules:      user.KeepFilterEntryRules,
		Stylesheet:                user.Stylesheet,
		CustomJS:                  user.CustomJS,
		ExternalFontHosts:         user.ExternalFontHosts,
		MarkReadBehavior:          markReadBehavior(user.MarkReadOnView, user.MarkReadOnMediaPlayerCompletion),
	}
}

// parseSettingsForm reads settings from an HTTP request form (used for regular POST).
func parseSettingsForm(r *http.Request) *settingsFormData {
	f := &settingsFormData{}
	f.Username = r.FormValue("username")
	f.Password = r.FormValue("password")
	f.Confirmation = r.FormValue("confirmation")
	f.Theme = r.FormValue("theme")
	f.Language = r.FormValue("language")
	f.Timezone = r.FormValue("timezone")
	f.EntryDirection = r.FormValue("entry_direction")
	f.EntryOrder = r.FormValue("entry_order")
	f.EntriesPerPage, _ = strconv.Atoi(r.FormValue("entries_per_page"))
	f.DefaultHomePage = r.FormValue("default_home_page")
	f.CategoriesSortingOrder = r.FormValue("categories_sorting_order")
	f.DisplayMode = r.FormValue("display_mode")
	f.GestureNav = r.FormValue("gesture_nav")
	f.DefaultReadingSpeed, _ = strconv.Atoi(r.FormValue("default_reading_speed"))
	f.CJKReadingSpeed, _ = strconv.Atoi(r.FormValue("cjk_reading_speed"))
	f.MediaPlaybackRate, _ = strconv.ParseFloat(r.FormValue("media_playback_rate"), 64)
	f.ShowReadingTime = r.FormValue("show_reading_time") == "1"
	f.KeyboardShortcuts = r.FormValue("keyboard_shortcuts") == "1"
	f.EntrySwipe = r.FormValue("entry_swipe") == "1"
	f.AlwaysOpenExternalLinks = r.FormValue("always_open_external_links") == "1"
	f.OpenExternalLinksInNewTab = r.FormValue("open_external_links_in_new_tab") == "1"
	f.AutoFetchShortEntries = r.FormValue("auto_fetch_short_entries") == "1"
	f.BlockFilterEntryRules = r.FormValue("block_filter_entry_rules")
	f.KeepFilterEntryRules = r.FormValue("keep_filter_entry_rules")
	f.Stylesheet = r.FormValue("stylesheet")
	f.CustomJS = r.FormValue("custom_js")    // Match HTML name attribute
	f.ExternalFontHosts = r.FormValue("external_font_hosts")
	f.MarkReadBehavior = r.FormValue("mark_read_behavior")
	return f
}

// applyToUser applies non-empty form values to the user.
func (f *settingsFormData) applyToUser(user *model.User) {
	if f.Username != "" {
		user.Username = f.Username
	}
	if f.Password != "" {
		user.Password = f.Password
	}
	user.Theme = f.Theme
	user.Language = f.Language
	user.Timezone = f.Timezone
	user.EntryDirection = f.EntryDirection
	user.EntryOrder = f.EntryOrder
	if f.EntriesPerPage > 0 {
		user.EntriesPerPage = f.EntriesPerPage
	}
	user.DefaultHomePage = f.DefaultHomePage
	user.CategoriesSortingOrder = f.CategoriesSortingOrder
	user.DisplayMode = f.DisplayMode
	user.GestureNav = f.GestureNav
	if f.DefaultReadingSpeed > 0 {
		user.DefaultReadingSpeed = f.DefaultReadingSpeed
	}
	if f.CJKReadingSpeed > 0 {
		user.CJKReadingSpeed = f.CJKReadingSpeed
	}
	if f.MediaPlaybackRate > 0 {
		user.MediaPlaybackRate = f.MediaPlaybackRate
	}
	user.ShowReadingTime = f.ShowReadingTime
	user.KeyboardShortcuts = f.KeyboardShortcuts
	user.EntrySwipe = f.EntrySwipe
	user.AlwaysOpenExternalLinks = f.AlwaysOpenExternalLinks
	user.OpenExternalLinksInNewTab = f.OpenExternalLinksInNewTab
	user.AutoFetchShortEntries = f.AutoFetchShortEntries
	user.BlockFilterEntryRules = f.BlockFilterEntryRules
	user.KeepFilterEntryRules = f.KeepFilterEntryRules
	user.Stylesheet = f.Stylesheet
	user.CustomJS = f.CustomJS
	user.ExternalFontHosts = f.ExternalFontHosts
	user.MarkReadOnView, user.MarkReadOnMediaPlayerCompletion = applyMarkReadBehavior(f.MarkReadBehavior)
}

func markReadBehavior(onView, onMediaPlayerCompletion bool) string {
	switch {
	case onView && onMediaPlayerCompletion:
		return "on-view-but-wait-for-player-completion"
	case onView:
		return "on-view"
	case onMediaPlayerCompletion:
		return "on-player-completion"
	default:
		return "no-auto"
	}
}

func applyMarkReadBehavior(behavior string) (onView, onMediaPlayerCompletion bool) {
	switch behavior {
	case "on-view":
		return true, false
	case "on-view-but-wait-for-player-completion":
		return true, true
	case "on-player-completion":
		return false, true
	default:
		return false, false
	}
}

func themeOptions() []selectOption {
	// Map old theme names to clean UI-friendly labels.
	// The Datastar UI uses system preference for dark/light;
	// theme setting mainly affects the classic UI at /.
	return []selectOption{
		{Value: "system_sans_serif", Label: "System (default)"},
		{Value: "light_sans_serif", Label: "Light"},
		{Value: "dark_sans_serif", Label: "Dark"},
		{Value: "system_serif", Label: "System, serif"},
		{Value: "light_serif", Label: "Light, serif"},
		{Value: "dark_serif", Label: "Dark, serif"},
	}
}

func languageOptions() []selectOption {
	opts := make([]selectOption, 0, len(locale.AvailableLanguages))
	for lang, label := range locale.AvailableLanguages {
		opts = append(opts, selectOption{Value: lang, Label: label})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
	return opts
}

func timezoneOptions() []selectOption {
	opts := make([]selectOption, 0)
	for tz := range timezone.AvailableTimezones() {
		opts = append(opts, selectOption{Value: tz, Label: tz})
	}
	sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
	return opts
}

