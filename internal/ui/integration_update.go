// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package ui // import "miniflux.app/v2/internal/ui"

import (
	"net/http"

	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/ui/form"
)

func (h *handler) updateIntegration(w http.ResponseWriter, r *http.Request) {
	sess := request.WebSession(r)
	printer := locale.NewPrinter(sess.Language())

	integrationForm := form.NewIntegrationForm(r)
	if localizedErr := integrationForm.Apply(h.store, request.UserID(r)); localizedErr != nil {
		sess.SetErrorMessage(localizedErr.Translate(sess.Language()))
		response.HTMLRedirect(w, r, h.routePath("/integrations"))
		return
	}

	sess.SetSuccessMessage(printer.Print("alert.prefs_saved"))
	response.HTMLRedirect(w, r, h.routePath("/integrations"))
}
