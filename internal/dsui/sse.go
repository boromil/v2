// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"net/http"

	"github.com/starfederation/datastar-go/datastar"
)

// SSEFragment represents a single DOM patch in an SSE response.
// Mode is optional: empty means the Datastar default (ElementPatchModeOuter).
// Use ElementPatchModeInner when the fragment should replace the *children* of
// the selector (e.g. a list container like #entry-list whose id must persist
// across patches).
type SSEFragment struct {
	HTML     string
	Selector string
	Mode     datastar.ElementPatchMode
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
			opts := []datastar.PatchElementOption{datastar.WithSelector(f.Selector)}
			if f.Mode != "" {
				opts = append(opts, datastar.WithMode(f.Mode))
			}
			sse.PatchElements(f.HTML, opts...)
		}
	}
	if len(resp.Signals) > 0 {
		sse.MarshalAndPatchSignals(resp.Signals)
	}
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
