// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"miniflux.app/v2/internal/config"
)

func TestHSTSMiddleware(t *testing.T) {
	prevOpts := config.Opts
	defer func() { config.Opts = prevOpts }()

	tests := []struct {
		name           string
		cfgLines       []string
		tlsConn        bool
		remoteAddr     string
		forwardedProto string
		expectHSTS     bool
	}{
		{
			name:       "direct TLS connection",
			tlsConn:    true,
			expectHSTS: true,
		},
		{
			name:       "plain HTTP request",
			expectHSTS: false,
		},
		{
			name:           "trusted proxy with X-Forwarded-Proto: https",
			cfgLines:       []string{"TRUSTED_REVERSE_PROXY_NETWORKS=10.0.0.0/8"},
			remoteAddr:     "10.0.0.1:12345",
			forwardedProto: "https",
			expectHSTS:     true,
		},
		{
			name:       "trusted proxy without X-Forwarded-Proto header",
			cfgLines:   []string{"TRUSTED_REVERSE_PROXY_NETWORKS=10.0.0.0/8"},
			remoteAddr: "10.0.0.1:12345",
			expectHSTS: false,
		},
		{
			name:           "untrusted proxy with X-Forwarded-Proto: https",
			cfgLines:       []string{"TRUSTED_REVERSE_PROXY_NETWORKS=10.0.0.0/8"},
			remoteAddr:     "192.168.1.1:12345",
			forwardedProto: "https",
			expectHSTS:     false,
		},
		{
			name:       "TLS with HSTS disabled",
			cfgLines:   []string{"DISABLE_HSTS=1"},
			tlsConn:    true,
			expectHSTS: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setConfig(t, tc.cfgLines)

			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)

			if tc.tlsConn {
				r.TLS = &tls.ConnectionState{}
			}
			if tc.remoteAddr != "" {
				r.RemoteAddr = tc.remoteAddr
			}
			if tc.forwardedProto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.forwardedProto)
			}

			handler.ServeHTTP(w, r)

			hsts := w.Header().Get("Strict-Transport-Security")
			if tc.expectHSTS && hsts == "" {
				t.Errorf("expected HSTS header to be set, got none")
			}
			if !tc.expectHSTS && hsts != "" {
				t.Errorf("expected no HSTS header, got %q", hsts)
			}
		})
	}
}

func setConfig(t *testing.T, lines []string) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "miniflux.conf")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	parser := config.NewConfigParser()
	opts, err := parser.ParseFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	config.Opts = opts
}
