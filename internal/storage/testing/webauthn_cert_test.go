// SPDX-FileCopyrightText: Copyright The Miniflux Authors. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package testing

import (
	"context"
	"testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"golang.org/x/crypto/acme/autocert"

	"miniflux.app/v2/internal/storage"
)

func TestAddWebAuthnCredential(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			handle := []byte("test-handle-001")
			credential := &webauthn.Credential{
				ID:              []byte{1, 2, 3, 4},
				PublicKey:       []byte{5, 6, 7, 8},
				AttestationType: "none",
				Authenticator: webauthn.Authenticator{
					AAGUID:       []byte{9, 10, 11, 12},
					SignCount:    1,
					CloneWarning: false,
				},
			}

			if err := s.AddWebAuthnCredential(user.ID, handle, credential); err != nil {
				t.Fatalf("AddWebAuthnCredential failed: %v", err)
			}

			count := s.CountWebAuthnCredentialsByUserID(user.ID)
			if count != 1 {
				t.Errorf("expected 1 credential, got %d", count)
			}
		})
	}
}

func TestWebAuthnCredentialByHandle(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			handle := []byte("lookup-handle")
			credential := &webauthn.Credential{
				ID:              []byte{10, 20, 30},
				PublicKey:       []byte{40, 50, 60},
				AttestationType: "packed",
				Authenticator: webauthn.Authenticator{
					AAGUID:       []byte{70, 80, 90},
					SignCount:    3,
					CloneWarning: true,
				},
			}
			s.AddWebAuthnCredential(user.ID, handle, credential)

			storedUserID, fetched, err := s.WebAuthnCredentialByHandle(handle)
			if err != nil {
				t.Fatalf("WebAuthnCredentialByHandle failed: %v", err)
			}
			if fetched == nil {
				t.Fatal("expected non-nil credential")
			}
			if storedUserID != user.ID {
				t.Errorf("expected user ID %d, got %d", user.ID, storedUserID)
			}
			if len(fetched.Handle) != len(handle) {
				t.Errorf("expected handle length %d, got %d", len(handle), len(fetched.Handle))
			}
		})
	}
}

func TestWebAuthnCredentialsByUserID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			s.AddWebAuthnCredential(user.ID, []byte("h1"), &webauthn.Credential{
				ID:              []byte{1},
				PublicKey:       []byte{2},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{3}, SignCount: 0},
			})
			s.AddWebAuthnCredential(user.ID, []byte("h2"), &webauthn.Credential{
				ID:              []byte{4},
				PublicKey:       []byte{5},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{6}, SignCount: 0},
			})

			creds, err := s.WebAuthnCredentialsByUserID(user.ID)
			if err != nil {
				t.Fatalf("WebAuthnCredentialsByUserID failed: %v", err)
			}
			if len(creds) != 2 {
				t.Errorf("expected 2 credentials, got %d", len(creds))
			}
		})
	}
}

func TestWebAuthnSaveLogin(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			handle := []byte("login-handle")
			s.AddWebAuthnCredential(user.ID, handle, &webauthn.Credential{
				ID:              []byte{1},
				PublicKey:       []byte{2},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{3}, SignCount: 0},
			})

			if err := s.WebAuthnSaveLogin(handle); err != nil {
				t.Fatalf("WebAuthnSaveLogin failed: %v", err)
			}

			_, fetched, _ := s.WebAuthnCredentialByHandle(handle)
			if fetched.LastSeenOn == nil || fetched.LastSeenOn.IsZero() {
				t.Error("expected non-zero last_seen_on after login")
			}
		})
	}
}

func TestWebAuthnUpdateName(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			handle := []byte("name-handle")
			s.AddWebAuthnCredential(user.ID, handle, &webauthn.Credential{
				ID:              []byte{1},
				PublicKey:       []byte{2},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{3}, SignCount: 0},
			})

			if err := s.WebAuthnUpdateName(handle, "My Security Key"); err != nil {
				t.Fatalf("WebAuthnUpdateName failed: %v", err)
			}

			_, fetched, _ := s.WebAuthnCredentialByHandle(handle)
			if fetched.Name != "My Security Key" {
				t.Errorf("expected name 'My Security Key', got %q", fetched.Name)
			}
		})
	}
}

func TestDeleteCredentialByHandle(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			handle := []byte("delete-handle")
			s.AddWebAuthnCredential(user.ID, handle, &webauthn.Credential{
				ID:              []byte{1},
				PublicKey:       []byte{2},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{3}, SignCount: 0},
			})

			if err := s.DeleteCredentialByHandle(user.ID, handle); err != nil {
				t.Fatalf("DeleteCredentialByHandle failed: %v", err)
			}
			if s.CountWebAuthnCredentialsByUserID(user.ID) != 0 {
				t.Error("expected 0 credentials after delete")
			}
		})
	}
}

func TestDeleteAllWebAuthnCredentialsByUserID(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			user := CreateTestUser(t, s)

			s.AddWebAuthnCredential(user.ID, []byte("h1"), &webauthn.Credential{
				ID:              []byte{1},
				PublicKey:       []byte{2},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{3}, SignCount: 0},
			})
			s.AddWebAuthnCredential(user.ID, []byte("h2"), &webauthn.Credential{
				ID:              []byte{4},
				PublicKey:       []byte{5},
				AttestationType: "none",
				Authenticator:   webauthn.Authenticator{AAGUID: []byte{6}, SignCount: 0},
			})

			if err := s.DeleteAllWebAuthnCredentialsByUserID(user.ID); err != nil {
				t.Fatalf("DeleteAllWebAuthnCredentialsByUserID failed: %v", err)
			}
			if s.CountWebAuthnCredentialsByUserID(user.ID) != 0 {
				t.Error("expected 0 credentials after delete all")
			}
		})
	}
}

func TestCertificateCache(t *testing.T) {
	for _, dbType := range allDBTypes() {
		t.Run(dbTypeName(dbType), func(t *testing.T) {
			s := SetupTestDB(t, dbType)
			var certCache autocert.Cache = storage.NewCertificateCache(s)

			ctx := context.Background()
			data := []byte("test-certificate-data")

			if err := certCache.Put(ctx, "test-key", data); err != nil {
				t.Fatalf("certCache.Put failed: %v", err)
			}

			fetched, err := certCache.Get(ctx, "test-key")
			if err != nil {
				t.Fatalf("certCache.Get failed: %v", err)
			}
			if string(fetched) != string(data) {
				t.Errorf("expected %q, got %q", string(data), string(fetched))
			}

			if err := certCache.Delete(ctx, "test-key"); err != nil {
				t.Fatalf("certCache.Delete failed: %v", err)
			}

			_, err = certCache.Get(ctx, "test-key")
			if err != autocert.ErrCacheMiss {
				t.Errorf("expected ErrCacheMiss after delete, got %v", err)
			}
		})
	}
}
