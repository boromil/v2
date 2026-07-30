// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package static // import "miniflux.app/v2/internal/dsui/static"

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
)

//go:embed css/*.css
var stylesheetFiles embed.FS

//go:embed js/*.js
var javascriptFiles embed.FS

type Asset struct {
	Data     []byte
	Checksum string
}

var (
	StylesheetBundles = make(map[string]Asset)
	JavascriptBundles = make(map[string]Asset)
)

func init() {
	// Process stylesheets.
	entries, err := fs.ReadDir(stylesheetFiles, "css")
	if err != nil {
		panic(fmt.Errorf("dsui: unable to read stylesheet directory: %w", err))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(stylesheetFiles, filepath.Join("css", entry.Name()))
		if err != nil {
			panic(fmt.Errorf("dsui: unable to read stylesheet %s: %w", entry.Name(), err))
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))[:8]
		name := entry.Name()
		key := name[:len(name)-len(filepath.Ext(name))]
		StylesheetBundles[key] = Asset{Data: data, Checksum: checksum}
	}

	// Process JavaScript.
	entries, err = fs.ReadDir(javascriptFiles, "js")
	if err != nil {
		panic(fmt.Errorf("dsui: unable to read javascript directory: %w", err))
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(javascriptFiles, filepath.Join("js", entry.Name()))
		if err != nil {
			panic(fmt.Errorf("dsui: unable to read javascript %s: %w", entry.Name(), err))
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))[:8]
		name := entry.Name()
		key := name[:len(name)-len(filepath.Ext(name))]
		JavascriptBundles[key] = Asset{Data: data, Checksum: checksum}
	}
}
