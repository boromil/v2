// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package template // import "miniflux.app/v2/internal/template"

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/ui/static"
)

// TestRenderConcurrency renders the same template concurrently in different
// languages. Because Render binds per-request, language-specific functions
// ("t", "plural", "elapsed") onto the template, doing so on a shared template
// while other requests execute it corrupts the output: a request can be served
// another request's language. Each concurrent render must match the output of
// the equivalent sequential render for its language.
func TestRenderConcurrency(t *testing.T) {
	engine := NewEngine("")
	engine.ParseTemplates()

	languages := []string{"en_US", "fr_FR", "de_DE", "es_ES", "pt_BR", "ru_RU", "zh_CN", "it_IT"}

	newData := func(language string) map[string]any {
		return map[string]any{"language": language, "theme": "system_serif"}
	}

	// Establish the expected output for each language sequentially.
	expected := make(map[string][]byte, len(languages))
	for _, language := range languages {
		expected[language] = engine.Render("offline.html", newData(language))
	}

	const iterations = 300

	var wg sync.WaitGroup
	var mu sync.Mutex
	mismatches := make(map[string]int)

	for i := 0; i < iterations; i++ {
		language := languages[i%len(languages)]
		wg.Add(1)
		go func(language string) {
			defer wg.Done()
			got := engine.Render("offline.html", newData(language))
			if !bytes.Equal(got, expected[language]) {
				mu.Lock()
				mismatches[language]++
				mu.Unlock()
			}
		}(language)
	}
	wg.Wait()

	if len(mismatches) > 0 {
		total := 0
		for _, n := range mismatches {
			total += n
		}
		t.Fatalf("concurrent Render produced wrong output for %d/%d requests (wrong-language translations); per-language mismatches: %v", total, iterations, mismatches)
	}
}

func TestCategoriesTemplateDeleteButton(t *testing.T) {
	if config.Opts == nil {
		var err error
		config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
		if err != nil {
			t.Fatalf("failed to init config: %v", err)
		}
	}

	initStaticBundles(t)

	engine := NewEngine("")
	engine.ParseTemplates()

	feedCount3 := 3
	feedCount0 := 0
	unreadCount := new(5)

	zeroUnread := 0
	categories := model.Categories{
		{ID: 1, Title: "Tech", FeedCount: &feedCount3, TotalUnread: unreadCount, UserID: 1},
		{ID: 2, Title: "Empty", FeedCount: &feedCount0, TotalUnread: &zeroUnread, UserID: 1},
	}

	data := map[string]any{
		"language":            "en_US",
		"theme":               "system_serif",
		"theme_checksum":      "dummy",
		"app_js_checksum":     "dummy",
		"sw_js_checksum":      "dummy",
		"csrf":                "test",
		"menu":                "categories",
		"countUnread":         0,
		"countErrorFeeds":     0,
		"flashSuccessMessage": "",
		"flashErrorMessage":   "",
		"webAuthnEnabled":     false,
		"user":                &model.User{ID: 1, Username: "test", Language: "en_US", Theme: "system_serif"},
		"categories":          categories,
		"total":               len(categories),
	}

	html := string(engine.Render("categories.html", data))

	if !strings.Contains(html, `data-url="/category/1/remove"`) {
		t.Error("expected delete button for category with feeds (ID=1)")
	}
	if !strings.Contains(html, `data-url="/category/2/remove"`) {
		t.Error("expected delete button for empty category (ID=2)")
	}

	if !strings.Contains(html, "This will also delete 3 feed(s)") {
		t.Error("expected warning message for category with 3 feeds")
	}
	if strings.Contains(html, `data-label-question="This will also delete`) && !strings.Contains(html, "This will also delete 3 feed(s)") {
		t.Error("warning message should be in rendered HTML for non-empty category")
	}
}

func TestCategoriesTemplateDeleteWarningMessage(t *testing.T) {
	if config.Opts == nil {
		var err error
		config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
		if err != nil {
			t.Fatalf("failed to init config: %v", err)
		}
	}

	initStaticBundles(t)

	engine := NewEngine("")
	engine.ParseTemplates()

	feedCount := 0
	unreadCount := new(5)
	categories := model.Categories{
		{ID: 1, Title: "Empty", FeedCount: &feedCount, TotalUnread: unreadCount, UserID: 1},
	}

	data := map[string]any{
		"language":            "en_US",
		"theme":               "system_serif",
		"theme_checksum":      "dummy",
		"app_js_checksum":     "dummy",
		"sw_js_checksum":      "dummy",
		"csrf":                "test",
		"menu":                "categories",
		"countUnread":         0,
		"countErrorFeeds":     0,
		"flashSuccessMessage": "",
		"flashErrorMessage":   "",
		"webAuthnEnabled":     false,
		"user":                &model.User{ID: 1, Username: "test", Language: "en_US", Theme: "system_serif"},
		"categories":          categories,
		"total":               1,
	}

	html := string(engine.Render("categories.html", data))

	if !strings.Contains(html, `data-url="/category/1/remove"`) {
		t.Error("expected delete button for empty category")
	}
	if strings.Contains(html, `confirm.question.delete_category`) {
		t.Error("empty category should use generic confirmation, not the warning")
	}
}

func initStaticBundles(t *testing.T) {
	t.Helper()

	if static.StylesheetBundles == nil {
		if err := static.GenerateStylesheetsBundles(); err != nil {
			t.Fatalf("failed to generate stylesheet bundles: %v", err)
		}
	}
	if static.BinaryBundles == nil {
		if err := static.GenerateBinaryBundles(); err != nil {
			t.Fatalf("failed to generate binary bundles: %v", err)
		}
	}
	if static.JavascriptBundles == nil {
		if err := static.GenerateJavascriptBundles(false); err != nil {
			t.Fatalf("failed to generate javascript bundles: %v", err)
		}
	}
}
