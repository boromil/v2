// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package form // import "miniflux.app/v2/internal/ui/form"

import (
	"testing"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
)

func TestValid(t *testing.T) {
	settings := &SettingsForm{
		Username:                "user",
		Password:                "hunter2",
		Confirmation:            "hunter2",
		Theme:                   "default",
		Language:                "en_US",
		Timezone:                "UTC",
		EntryDirection:          "asc",
		EntriesPerPage:          50,
		DisplayMode:             "standalone",
		GestureNav:              "tap",
		DefaultReadingSpeed:     35,
		CJKReadingSpeed:         25,
		DefaultHomePage:         "unread",
		MediaPlaybackRate:       1.25,
		AlwaysOpenExternalLinks: true,
	}

	err := settings.Validate()
	if err != nil {
		t.Error(err)
	}
}

func TestConfirmationEmpty(t *testing.T) {
	settings := &SettingsForm{
		Username:                "user",
		Password:                "hunter2",
		Confirmation:            "",
		Theme:                   "default",
		Language:                "en_US",
		Timezone:                "UTC",
		EntryDirection:          "asc",
		EntriesPerPage:          50,
		DisplayMode:             "standalone",
		GestureNav:              "tap",
		DefaultReadingSpeed:     35,
		CJKReadingSpeed:         25,
		DefaultHomePage:         "unread",
		MediaPlaybackRate:       1.25,
		AlwaysOpenExternalLinks: true,
	}

	err := settings.Validate()
	if err != nil {
		t.Error(err)
	}

	if settings.Password != "" {
		t.Error("Password should have been cleared")
	}
}

func TestConfirmationIncorrect(t *testing.T) {
	settings := &SettingsForm{
		Username:                "user",
		Password:                "hunter2",
		Confirmation:            "unter2",
		Theme:                   "default",
		Language:                "en_US",
		Timezone:                "UTC",
		EntryDirection:          "asc",
		EntriesPerPage:          50,
		DisplayMode:             "standalone",
		GestureNav:              "tap",
		DefaultReadingSpeed:     35,
		CJKReadingSpeed:         25,
		DefaultHomePage:         "unread",
		MediaPlaybackRate:       1.25,
		AlwaysOpenExternalLinks: true,
	}

	err := settings.Validate()
	if err == nil {
		t.Error("Validate should return an error")
	}
}

func TestMergeAutoFetchShortEntries(t *testing.T) {
	// Regression test: the auto_fetch_short_entries preference must propagate
	// from the settings form into the user model on save, and from the user
	// model back into the form on page load (see settings_show.go).
	config.Opts = config.NewConfigOptions()
	user := &model.User{}

	// Disabled by default.
	form := &SettingsForm{AutoFetchShortEntries: false}
	form.Merge(user)
	if user.AutoFetchShortEntries {
		t.Error("expected AutoFetchShortEntries to be false after merge")
	}

	// Enabled via form.
	form = &SettingsForm{AutoFetchShortEntries: true}
	form.Merge(user)
	if !user.AutoFetchShortEntries {
		t.Error("expected AutoFetchShortEntries to be true after merge")
	}
}
