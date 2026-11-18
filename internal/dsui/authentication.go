// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"encoding/hex"
	"net/http"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
)

// passkeyView is one row of the passkeys table.
type passkeyView struct {
	Handle      string
	Name        string
	AddedOn     time.Time
	HasBeenUsed bool
	LastSeenOn  time.Time
}

// showAuthentication renders the authentication page: password, linked
// accounts, and passkeys.
func (h *handler) showAuthentication(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	credentials, err := h.store.WebAuthnCredentialsByUserID(user.ID)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	hasPassword, err := h.store.HasPassword(user.ID)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	passkeys := make([]passkeyView, 0, len(credentials))
	for _, credential := range credentials {
		view := passkeyView{
			Handle: hex.EncodeToString(credential.Handle),
			Name:   credential.Name,
		}
		if credential.AddedOn != nil {
			view.AddedOn = *credential.AddedOn
		}
		if credential.LastSeenOn != nil {
			view.LastSeenOn = *credential.LastSeenOn
			view.HasBeenUsed = true
		}
		passkeys = append(passkeys, view)
	}

	flashError, flashSuccess, clearFlash := takeFlash(r)
	if clearFlash != nil {
		http.SetCookie(w, clearFlash)
	}

	vm := pageViewModel{
		appViewModel:     h.newPageViewModel(r, user),
		PageTemplate:     "authentication",
		PageHeading:      printerFor(user.Language).Print("menu.authentication"),
		FlashError:       flashError,
		FlashSuccess:     flashSuccess,
		HasPassword:      hasPassword,
		Passkeys:         passkeys,
		WebAuthnEnabled:  config.Opts.WebAuthn(),
		OAuth2Provider:   config.Opts.OAuth2Provider(),
		OIDCProviderName: config.Opts.OAuth2OIDCProviderName(),
		HasGoogleID:      user.GoogleID != "",
		HasOIDCID:        user.OpenIDConnectID != "",
	}
	vm.Title = vm.PageHeading + " — Miniflux"

	h.renderPage(w, r, vm)
}

// renamePasskey updates the label of one passkey.
func (h *handler) renamePasskey(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	handle, err := hex.DecodeString(r.PathValue("handle"))
	if err != nil {
		response.HTMLNotFound(w, r)
		return
	}

	name := r.FormValue("name")
	if name == "" {
		setFlashError(w, printerFor(user.Language).Print("error.fields_mandatory"))
		response.HTMLRedirect(w, r, "/ds/authentication")
		return
	}

	rowsAffected, err := h.store.WebAuthnUpdateName(user.ID, handle, name)
	if err != nil {
		setFlashError(w, printerFor(user.Language).Printf("error.database_error", err))
		response.HTMLRedirect(w, r, "/ds/authentication")
		return
	}
	if rowsAffected == 0 {
		response.HTMLNotFound(w, r)
		return
	}

	response.HTMLRedirect(w, r, "/ds/authentication")
}

// removePasskey deletes one passkey.
func (h *handler) removePasskey(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	handle, err := hex.DecodeString(r.PathValue("handle"))
	if err != nil {
		response.HTMLNotFound(w, r)
		return
	}

	if err := h.store.DeleteCredentialByHandle(user.ID, handle); err != nil {
		setFlashError(w, printerFor(user.Language).Printf("error.database_error", err))
	}

	response.HTMLRedirect(w, r, "/ds/authentication")
}
