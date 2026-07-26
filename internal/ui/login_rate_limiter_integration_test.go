// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build !race

package ui

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/database/dialect"
	storageTesting "miniflux.app/v2/internal/storage/testing"
	"miniflux.app/v2/internal/worker"
)

func TestLoginRateLimitIntegration(t *testing.T) {
	if config.Opts == nil {
		var err error
		config.Opts, err = config.NewConfigParser().ParseEnvironmentVariables()
		if err != nil {
			t.Fatalf("failed to initialize config defaults: %v", err)
		}
	}

	store := storageTesting.SetupTestDB(t, dialect.SQLite)
	pool := worker.NewPool(store, 1)

	handler := Serve(store, pool)

	// First GET to establish session and capture CSRF token.
	ts := httptest.NewServer(handler)
	defer ts.Close()

	client := &http.Client{}

	getCSRFToken := func() (string, string) {
		req, err := http.NewRequest("GET", ts.URL+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		// Extract Set-Cookie for session.
		setCookies := resp.Header.Values("Set-Cookie")
		var cookieHeader string
		for _, sc := range setCookies {
			if name := extractCookieName(sc); name == "MinifluxSessionID" {
				cookieHeader = fmt.Sprintf("MinifluxSessionID=%s", extractCookieValue(sc))
			}
		}

		// Extract CSRF token from HTML.
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		csrfToken := extractCSRFToken(string(body))
		if csrfToken == "" {
			t.Fatal("could not find CSRF token in login page")
		}
		return cookieHeader, csrfToken
	}

	cookieHeader, csrfToken := getCSRFToken()

	// Send 6 failed login attempts. The 6th should return 429.
	expectedStatuses := []int{
		200, // 1st: bad credentials → login page with error
		200, // 2nd
		200, // 3rd
		200, // 4th
		200, // 5th
		429, // 6th: rate limited
	}

	for i, wantStatus := range expectedStatuses {
		data := fmt.Sprintf("username=test&password=wrong%d&csrf=%s", i, csrfToken)
		req, err := http.NewRequest("POST", ts.URL+"/login", strings.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Cookie", cookieHeader)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
		req.Header.Set("X-Csrf-Token", csrfToken)
		// Don't follow redirects.
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request %d failed: %v", i+1, err)
		}

		status := resp.StatusCode
		resp.Body.Close()

		if status != wantStatus {
			t.Errorf("attempt %d: expected status %d, got %d", i+1, wantStatus, status)
		}
	}

	// 7th attempt should also be 429.
	data := fmt.Sprintf("username=test&password=wrong7&csrf=%s", csrfToken)
	req, _ := http.NewRequest("POST", ts.URL+"/login", strings.NewReader(data))
	req.Header.Set("Cookie", cookieHeader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
	req.Header.Set("X-Csrf-Token", csrfToken)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 429 {
		t.Errorf("7th attempt: expected 429, got %d", resp.StatusCode)
	}

	// Verify Retry-After header is present on 429 response and is a positive integer.
	got := resp.Header.Get("Retry-After")
	if got == "" {
		t.Fatal("429 response should have Retry-After header")
	}
	n, err := strconv.Atoi(got)
	if err != nil {
		t.Fatalf("Retry-After should be an integer, got %q: %v", got, err)
	}
	if n < 1 {
		t.Errorf("Retry-After should be a positive integer, got %d", n)
	}
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func extractCSRFToken(body string) string {
	matches := csrfRe.FindStringSubmatch(body)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func extractCookieName(setCookie string) string {
	parts := strings.SplitN(setCookie, "=", 2)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func extractCookieValue(setCookie string) string {
	parts := strings.SplitN(setCookie, "=", 2)
	if len(parts) < 2 {
		return ""
	}
	// Split on semicolon to get just the value (before attributes).
	val := strings.SplitN(parts[1], ";", 2)
	return strings.TrimSpace(val[0])
}

// Note: this test uses SQLite which doesn't require external DB setup.
// It does require a real store and template engine, so it cannot be a pure unit test.
