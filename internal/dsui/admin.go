// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dsui // import "miniflux.app/v2/internal/dsui"

import (
	"net/http"
	"time"

	"miniflux.app/v2/internal/config"
	"miniflux.app/v2/internal/http/request"
	"miniflux.app/v2/internal/http/response"
	"miniflux.app/v2/internal/model"
	"miniflux.app/v2/internal/ui/form"
	"miniflux.app/v2/internal/validator"
)

// apiKeyView is one row of the API keys table.
type apiKeyView struct {
	ID          int64
	Description string
	Token       string
	CreatedAt   time.Time
	LastUsedAt  time.Time
	HasBeenUsed bool
}

// userView is one row of the users table.
type userView struct {
	ID          int64
	Username    string
	IsAdmin     bool
	IsCurrent   bool
	LastLoginAt time.Time
	HasLoggedIn bool
}

// showAPIKeys renders the API keys page.
func (h *handler) showAPIKeys(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if !config.Opts.HasAPI() {
		response.HTMLNotFound(w, r)
		return
	}

	apiKeys, err := h.store.APIKeys(user.ID)
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	views := make([]apiKeyView, 0, len(apiKeys))
	for _, apiKey := range apiKeys {
		view := apiKeyView{
			ID:          apiKey.ID,
			Description: apiKey.Description,
			Token:       apiKey.Token,
			CreatedAt:   apiKey.CreatedAt,
		}
		if apiKey.LastUsedAt != nil {
			view.LastUsedAt = *apiKey.LastUsedAt
			view.HasBeenUsed = true
		}
		views = append(views, view)
	}

	flashError, flashSuccess, clearFlash := takeFlash(r)
	if clearFlash != nil {
		http.SetCookie(w, clearFlash)
	}

	vm := pageViewModel{
		appViewModel: h.newPageViewModel(r, user),
		PageTemplate: "api_keys",
		PageHeading:  printerFor(user.Language).Print("page.api_keys.title"),
		FlashError:   flashError,
		FlashSuccess: flashSuccess,
		APIKeys:      views,
	}
	vm.Title = vm.PageHeading + " — Miniflux"

	h.renderPage(w, r, vm)
}

// createAPIKey adds an API key from the settings form.
func (h *handler) createAPIKey(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	apiKeyForm := form.NewAPIKeyForm(r)
	creationRequest := &model.APIKeyCreationRequest{Description: apiKeyForm.Description}

	if validationErr := validator.ValidateAPIKeyCreation(h.store, user.ID, creationRequest); validationErr != nil {
		setFlashError(w, validationErr.Translate(user.Language))
		response.HTMLRedirect(w, r, "/ds/keys")
		return
	}

	if _, err := h.store.CreateAPIKey(user.ID, creationRequest.Description); err != nil {
		setFlashError(w, printerFor(user.Language).Printf("error.database_error", err))
		response.HTMLRedirect(w, r, "/ds/keys")
		return
	}

	response.HTMLRedirect(w, r, "/ds/keys")
}

// removeAPIKey deletes one of the user's API keys.
func (h *handler) removeAPIKey(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	keyID := request.RouteInt64Param(r, "keyID")
	if err := h.store.DeleteAPIKey(user.ID, keyID); err != nil {
		setFlashError(w, printerFor(user.Language).Printf("error.database_error", err))
	}

	response.HTMLRedirect(w, r, "/ds/keys")
}

// showUsers renders the user management page (administrators only).
func (h *handler) showUsers(w http.ResponseWriter, r *http.Request) {
	user, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if !user.IsAdmin {
		response.HTMLForbidden(w, r)
		return
	}

	users, err := h.store.Users()
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	views := make([]userView, 0, len(users))
	for _, item := range users {
		view := userView{
			ID:        item.ID,
			Username:  item.Username,
			IsAdmin:   item.IsAdmin,
			IsCurrent: item.ID == user.ID,
		}
		if item.LastLoginAt != nil {
			view.LastLoginAt = *item.LastLoginAt
			view.HasLoggedIn = true
		}
		views = append(views, view)
	}

	flashError, flashSuccess, clearFlash := takeFlash(r)
	if clearFlash != nil {
		http.SetCookie(w, clearFlash)
	}

	vm := pageViewModel{
		appViewModel: h.newPageViewModel(r, user),
		PageTemplate: "users",
		PageHeading:  printerFor(user.Language).Print("page.users.title"),
		FlashError:   flashError,
		FlashSuccess: flashSuccess,
		Users:        views,
	}
	vm.Title = vm.PageHeading + " — Miniflux"

	h.renderPage(w, r, vm)
}

// createUser adds a user account.
func (h *handler) createUser(w http.ResponseWriter, r *http.Request) {
	loggedUser, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if !loggedUser.IsAdmin {
		response.HTMLForbidden(w, r)
		return
	}

	userForm := form.NewUserForm(r)
	if validationErr := userForm.ValidateCreation(); validationErr != nil {
		setFlashError(w, validationErr.Translate(loggedUser.Language))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	if h.store.UserExists(userForm.Username) {
		setFlashError(w, printerFor(loggedUser.Language).Print("error.user_already_exists"))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	creationRequest := &model.UserCreationRequest{
		Username: userForm.Username,
		Password: userForm.Password,
		IsAdmin:  userForm.IsAdmin,
	}
	if validationErr := validator.ValidateUserCreationWithPassword(h.store, creationRequest); validationErr != nil {
		setFlashError(w, validationErr.Translate(loggedUser.Language))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	if _, err := h.store.CreateUser(creationRequest); err != nil {
		setFlashError(w, printerFor(loggedUser.Language).Printf("error.database_error", err))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	response.HTMLRedirect(w, r, "/ds/users")
}

// updateUser changes the username, password, or administrator flag of another
// account. Editing yourself goes through the authentication page, which asks
// for the current password.
func (h *handler) updateUser(w http.ResponseWriter, r *http.Request) {
	loggedUser, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if !loggedUser.IsAdmin {
		response.HTMLForbidden(w, r)
		return
	}

	selectedUser, err := h.store.UserByID(request.RouteInt64Param(r, "userID"))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}
	if selectedUser == nil {
		response.HTMLNotFound(w, r)
		return
	}
	// The page never offers these actions on your own account: changing your
	// password must go through the authentication page, which asks for the
	// current password.
	if selectedUser.ID == loggedUser.ID {
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	userForm := form.NewUserForm(r)
	if validationErr := userForm.ValidateModification(); validationErr != nil {
		setFlashError(w, validationErr.Translate(loggedUser.Language))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	modificationRequest := &model.UserModificationRequest{
		Username: model.OptionalString(userForm.Username),
	}
	if validationErr := validator.ValidateUserModification(h.store, selectedUser.ID, modificationRequest); validationErr != nil {
		setFlashError(w, validationErr.Translate(loggedUser.Language))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	userForm.Merge(selectedUser)
	if err := h.store.UpdateUser(selectedUser); err != nil {
		setFlashError(w, printerFor(loggedUser.Language).Printf("error.database_error", err))
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	if userForm.Password != "" {
		if err := h.store.RemoveUserWebSessionsExcept(selectedUser.ID, ""); err != nil {
			setFlashError(w, printerFor(loggedUser.Language).Printf("error.database_error", err))
		}
	}

	response.HTMLRedirect(w, r, "/ds/users")
}

// removeUser deletes another account.
func (h *handler) removeUser(w http.ResponseWriter, r *http.Request) {
	loggedUser, err := h.store.UserByID(request.UserID(r))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}

	if !loggedUser.IsAdmin {
		response.HTMLForbidden(w, r)
		return
	}

	selectedUser, err := h.store.UserByID(request.RouteInt64Param(r, "userID"))
	if err != nil {
		response.HTMLServerError(w, r, err)
		return
	}
	if selectedUser == nil {
		response.HTMLNotFound(w, r)
		return
	}
	// The page never offers these actions on your own account: changing your
	// password must go through the authentication page, which asks for the
	// current password.
	if selectedUser.ID == loggedUser.ID {
		response.HTMLRedirect(w, r, "/ds/users")
		return
	}

	if err := h.store.RemoveUser(selectedUser.ID); err != nil {
		setFlashError(w, printerFor(loggedUser.Language).Printf("error.database_error", err))
	}

	response.HTMLRedirect(w, r, "/ds/users")
}
