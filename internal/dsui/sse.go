// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"
)

// renderSSEFragment renders a template fragment and sends it as a Datastar SSE
// element patch to the client. The fragment replaces the DOM element matching
// the given CSS selector.
func renderSSEFragment(w http.ResponseWriter, r *http.Request, tpl *template.Template, name string, data any, selector string) {
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "Template render error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	sse.PatchElements(buf.String(), datastar.WithSelector(selector))
}

// renderSSEMulti renders multiple template fragments and sends them as
// separate SSE element patches.
func renderSSEMulti(w http.ResponseWriter, r *http.Request, fragments []SSEFragment) {
	sse := datastar.NewSSE(w, r)
	for _, f := range fragments {
		sse.PatchElements(f.HTML, datastar.WithSelector(f.Selector))
	}
}

// SSEFragment represents a single DOM patch in an SSE response.
type SSEFragment struct {
	HTML     string
	Selector string
}

// sendSSERedirect sends a client-side redirect via Datastar.
func sendSSERedirect(w http.ResponseWriter, r *http.Request, url string) {
	sse := datastar.NewSSE(w, r)
	sse.Redirect(url)
}

// readSignals reads Datastar signals from the request into the provided struct.
func readSignals(r *http.Request, v any) error {
	return datastar.ReadSignals(r, v)
}
