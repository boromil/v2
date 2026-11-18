// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package form // import "miniflux.app/v2/internal/ui/form"

import (
	"crypto/md5"
	"fmt"

	"miniflux.app/v2/internal/crypto"
	"miniflux.app/v2/internal/locale"
	"miniflux.app/v2/internal/storage"
)

// Apply persists the form values on top of the stored integration.
//
// It owns the derived fields and the cross-field checks so the classic UI and
// the Datastar UI share one implementation: the Fever token is derived from the
// username and password, the Google Reader password is hashed, the webhook
// secret is generated on first save, and duplicate usernames are rejected.
func (i IntegrationForm) Apply(store *storage.Storage, userID int64) *locale.LocalizedError {
	integration, err := store.Integration(userID)
	if err != nil {
		return locale.NewLocalizedError("error.database_error", err)
	}

	i.Merge(integration)

	if integration.FeverUsername != "" && store.HasDuplicateFeverUsername(userID, integration.FeverUsername) {
		return locale.NewLocalizedError("error.duplicate_fever_username")
	}

	if integration.FeverEnabled {
		if i.FeverPassword != "" {
			integration.FeverToken = fmt.Sprintf("%x", md5.Sum([]byte(integration.FeverUsername+":"+i.FeverPassword)))
		}
	} else {
		integration.FeverToken = ""
	}

	if validationErr := i.ValidateGoogleReader(integration.GoogleReaderPassword); validationErr != nil {
		return validationErr
	}

	if integration.GoogleReaderUsername != "" && store.HasDuplicateGoogleReaderUsername(userID, integration.GoogleReaderUsername) {
		return locale.NewLocalizedError("error.duplicate_googlereader_username")
	}

	if integration.GoogleReaderEnabled {
		if i.GoogleReaderPassword != "" {
			hashedPassword, err := crypto.HashPassword(i.GoogleReaderPassword)
			if err != nil {
				return locale.NewLocalizedError("error.database_error", err)
			}
			integration.GoogleReaderPassword = hashedPassword
		}
	} else {
		integration.GoogleReaderPassword = ""
	}

	if i.WebhookEnabled {
		if i.WebhookURL == "" {
			integration.WebhookEnabled = false
			integration.WebhookSecret = ""
		} else if integration.WebhookSecret == "" {
			integration.WebhookSecret = crypto.GenerateRandomStringHex(32)
		}
	} else {
		integration.WebhookURL = ""
		integration.WebhookSecret = ""
	}

	if i.LinktacoEnabled {
		if i.LinktacoAPIToken == "" || i.LinktacoOrgSlug == "" {
			return locale.NewLocalizedError("error.linktaco_missing_required_fields")
		}
	}

	// The visibility column is constrained to PUBLIC or PRIVATE: fall back to
	// PUBLIC when the form did not send the select (partial posts, API clients).
	if integration.LinktacoVisibility == "" {
		integration.LinktacoVisibility = "PUBLIC"
	}

	if err := store.UpdateIntegration(integration); err != nil {
		return locale.NewLocalizedError("error.database_error", err)
	}

	return nil
}
