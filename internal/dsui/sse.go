// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"bytes"
	"html/template"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"
)

// SSEFragment represents a single DOM patch in an SSE response.
type SSEFragment struct {
	HTML     string
	Selector string
}

// SSEResponse bundles element patches and optional signal updates into a
// single SSE stream. Only one NewSSE call is allowed per HTTP response.
type SSEResponse struct {
	Fragments []SSEFragment
	Signals   map[string]any
}

// renderSSEResponse sends a complete SSE response with element patches
// and optional signal updates in a single stream.
func renderSSEResponse(w http.ResponseWriter, r *http.Request, resp SSEResponse) {
	sse := datastar.NewSSE(w, r)
	for _, f := range resp.Fragments {
		if f.HTML != "" || f.Selector != "" {
			sse.PatchElements(f.HTML, datastar.WithSelector(f.Selector))
		}
	}
	if len(resp.Signals) > 0 {
		sse.MarshalAndPatchSignals(resp.Signals)
	}
}

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

// sendSSERedirect sends a client-side redirect via Datastar.
func sendSSERedirect(w http.ResponseWriter, r *http.Request, url string) {
	sse := datastar.NewSSE(w, r)
	sse.Redirect(url)
}

// readSignals reads Datastar signals from the request into the provided struct.
func readSignals(r *http.Request, v any) error {
	return datastar.ReadSignals(r, v)
}
