package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewTokenStore(t *testing.T) {
	tests := []struct {
		name          string
		dbPath        string
		secretKeyBase string
		expectError   bool
	}{
		{
			name:          "valid parameters",
			dbPath:        ":memory:",
			secretKeyBase: "test-secret-key-base-for-encryption",
			expectError:   false,
		},
		{
			name:          "empty secret key",
			dbPath:        ":memory:",
			secretKeyBase: "",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewTokenStore(tt.dbPath, tt.secretKeyBase)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if store != nil {
					store.Close()
					t.Errorf("expected nil store but got %+v", store)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if store == nil {
					t.Errorf("expected store but got nil")
				} else {
					store.Close()
				}
			}
		})
	}
}

func TestTokenStoreEncryptDecrypt(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	tests := []struct {
		name      string
		plaintext string
	}{
		{
			name:      "simple token",
			plaintext: "test-token-123",
		},
		{
			name:      "jwt token",
			plaintext: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
		},
		{
			name:      "long token",
			plaintext: "very-long-token-" + string(make([]byte, 1000)),
		},
		{
			name:      "special characters",
			plaintext: "token-with-special-chars-!@#$%^&*()_+-=[]{}|;:',.<>?/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encrypt
			encrypted, err := store.encrypt(tt.plaintext)
			if err != nil {
				t.Fatalf("failed to encrypt: %v", err)
			}

			if encrypted == "" {
				t.Errorf("encrypted text is empty")
			}

			if encrypted == tt.plaintext {
				t.Errorf("encrypted text should differ from plaintext")
			}

			// Decrypt
			decrypted, err := store.decrypt(encrypted)
			if err != nil {
				t.Fatalf("failed to decrypt: %v", err)
			}

			if decrypted != tt.plaintext {
				t.Errorf("decrypted text doesn't match plaintext: got %s, want %s", decrypted, tt.plaintext)
			}
		})
	}
}

func TestTokenStoreEncryptEmptyString(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	_, err = store.encrypt("")
	if err == nil {
		t.Errorf("expected error when encrypting empty string")
	}
}

func TestTokenStoreDecryptEmptyString(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	_, err = store.decrypt("")
	if err == nil {
		t.Errorf("expected error when decrypting empty string")
	}
}

func TestTokenStoreDecryptInvalidData(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	tests := []struct {
		name      string
		encrypted string
	}{
		{
			name:      "invalid base64",
			encrypted: "not-valid-base64!@#$",
		},
		{
			name:      "valid base64 but invalid ciphertext",
			encrypted: "dGhpcyBpcyBub3QgdmFsaWQgY2lwaGVydGV4dA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.decrypt(tt.encrypted)
			if err == nil {
				t.Errorf("expected error when decrypting invalid data")
			}
		})
	}
}

func TestTokenStoreSaveAndLoad(t *testing.T) {
	// Create temp directory for test database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := NewTokenStore(dbPath, "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	token := "test-jwt-token-abc123"
	expiresAt := time.Now().Add(1 * time.Hour)

	// Save token
	err = store.SaveToken(token, expiresAt)
	if err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Load token
	loadedToken, loadedExpiry, err := store.LoadToken()
	if err != nil {
		t.Fatalf("failed to load token: %v", err)
	}

	if loadedToken != token {
		t.Errorf("loaded token doesn't match: got %s, want %s", loadedToken, token)
	}

	// Check expiry (within 1 second tolerance)
	if loadedExpiry.Unix() != expiresAt.Unix() {
		t.Errorf("loaded expiry doesn't match: got %v, want %v", loadedExpiry, expiresAt)
	}
}

func TestTokenStoreSaveEmptyToken(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	err = store.SaveToken("", time.Now().Add(1*time.Hour))
	if err == nil {
		t.Errorf("expected error when saving empty token")
	}
}

func TestTokenStoreSaveExpiredToken(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	err = store.SaveToken("test-token", time.Now().Add(-1*time.Hour))
	if err == nil {
		t.Errorf("expected error when saving expired token")
	}
}

func TestTokenStoreLoadNoToken(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	_, _, err = store.LoadToken()
	if err == nil {
		t.Errorf("expected error when loading from empty store")
	}
}

func TestTokenStoreLoadExpiredToken(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	// Save an already expired token
	token := "test-token"
	expiresAt := time.Now().Add(-1 * time.Hour)

	// We need to bypass the validation in SaveToken
	// So we'll use a future time for saving
	futureExpiry := time.Now().Add(1 * time.Hour)
	err = store.SaveToken(token, futureExpiry)
	if err != nil {
		t.Fatalf("failed to save token: %v", err)
	}

	// Now manually update the expiry to the past
	store.mu.Lock()
	_, err = store.db.Exec("UPDATE tokens SET expires_at = ?", expiresAt)
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("failed to update expiry: %v", err)
	}

	// Try to load - should get error as token is expired
	_, _, err = store.LoadToken()
	if err == nil {
		t.Errorf("expected error when loading expired token")
	}
}

func TestTokenStoreLoadMostRecent(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	// Save multiple tokens
	token1 := "token-1"
	token2 := "token-2"
	token3 := "token-3"
	expiresAt := time.Now().Add(1 * time.Hour)

	err = store.SaveToken(token1, expiresAt)
	if err != nil {
		t.Fatalf("failed to save token1: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	err = store.SaveToken(token2, expiresAt)
	if err != nil {
		t.Fatalf("failed to save token2: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	err = store.SaveToken(token3, expiresAt)
	if err != nil {
		t.Fatalf("failed to save token3: %v", err)
	}

	// Load should return the most recent (token3)
	loadedToken, _, err := store.LoadToken()
	if err != nil {
		t.Fatalf("failed to load token: %v", err)
	}

	if loadedToken != token3 {
		t.Errorf("expected most recent token %s, got %s", token3, loadedToken)
	}
}

func TestTokenStoreCleanupExpiredTokens(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	now := time.Now()

	// Save valid token
	validToken := "valid-token"
	err = store.SaveToken(validToken, now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("failed to save valid token: %v", err)
	}

	// Save expired tokens by bypassing validation
	expiredToken1 := "expired-token-1"
	expiredToken2 := "expired-token-2"

	// Save with future expiry first
	err = store.SaveToken(expiredToken1, now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("failed to save expired token 1: %v", err)
	}
	err = store.SaveToken(expiredToken2, now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("failed to save expired token 2: %v", err)
	}

	// Update them to be expired
	store.mu.Lock()
	_, err = store.db.Exec("UPDATE tokens SET expires_at = ? WHERE encrypted_token != (SELECT encrypted_token FROM tokens WHERE encrypted_token = (SELECT encrypted_token FROM tokens ORDER BY updated_at DESC LIMIT 1))", now.Add(-1*time.Hour))
	store.mu.Unlock()
	if err != nil {
		t.Fatalf("failed to expire tokens: %v", err)
	}

	// Get stats before cleanup
	total, valid, expired, err := store.Stats()
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if total != 3 {
		t.Errorf("expected 3 total tokens, got %d", total)
	}

	// Cleanup expired tokens
	err = store.CleanupExpiredTokens()
	if err != nil {
		t.Fatalf("failed to cleanup: %v", err)
	}

	// Get stats after cleanup
	total, valid, expired, err = store.Stats()
	if err != nil {
		t.Fatalf("failed to get stats after cleanup: %v", err)
	}

	if expired != 0 {
		t.Errorf("expected 0 expired tokens after cleanup, got %d", expired)
	}

	if valid != 1 {
		t.Errorf("expected 1 valid token after cleanup, got %d", valid)
	}

	// Verify we can still load the valid token
	loadedToken, _, err := store.LoadToken()
	if err != nil {
		t.Fatalf("failed to load token after cleanup: %v", err)
	}

	if loadedToken != validToken {
		t.Errorf("expected valid token %s after cleanup, got %s", validToken, loadedToken)
	}
}

func TestTokenStoreStats(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	// Initially should have 0 tokens
	total, valid, expired, err := store.Stats()
	if err != nil {
		t.Fatalf("failed to get initial stats: %v", err)
	}

	if total != 0 || valid != 0 || expired != 0 {
		t.Errorf("expected all 0 stats, got total=%d valid=%d expired=%d", total, valid, expired)
	}

	// Save a valid token
	now := time.Now()
	err = store.SaveToken("valid-token", now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("failed to save valid token: %v", err)
	}

	total, valid, expired, err = store.Stats()
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if total != 1 || valid != 1 || expired != 0 {
		t.Errorf("expected 1 total, 1 valid, 0 expired; got total=%d valid=%d expired=%d", total, valid, expired)
	}
}

func TestTokenStoreConcurrentAccess(t *testing.T) {
	store, err := NewTokenStore(":memory:", "test-secret-key-base")
	if err != nil {
		t.Fatalf("failed to create token store: %v", err)
	}
	defer store.Close()

	// Test concurrent saves
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			token := "token-" + string(rune('0'+idx))
			err := store.SaveToken(token, time.Now().Add(1*time.Hour))
			if err != nil {
				t.Errorf("failed to save token %d: %v", idx, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify we can load a token
	_, _, err = store.LoadToken()
	if err != nil {
		t.Errorf("failed to load token after concurrent saves: %v", err)
	}
}

func TestTokenStorePersistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "persistence.db")

	token := "persistent-token"
	expiresAt := time.Now().Add(1 * time.Hour)
	secretKey := "test-secret-key-base"

	// Create store and save token
	{
		store, err := NewTokenStore(dbPath, secretKey)
		if err != nil {
			t.Fatalf("failed to create token store: %v", err)
		}

		err = store.SaveToken(token, expiresAt)
		if err != nil {
			t.Fatalf("failed to save token: %v", err)
		}

		store.Close()
	}

	// Reopen store and load token
	{
		store, err := NewTokenStore(dbPath, secretKey)
		if err != nil {
			t.Fatalf("failed to reopen token store: %v", err)
		}
		defer store.Close()

		loadedToken, _, err := store.LoadToken()
		if err != nil {
			t.Fatalf("failed to load token from reopened store: %v", err)
		}

		if loadedToken != token {
			t.Errorf("loaded token doesn't match after reopen: got %s, want %s", loadedToken, token)
		}
	}
}

func TestTokenStoreWrongSecretKey(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "wrongkey.db")

	token := "test-token"
	expiresAt := time.Now().Add(1 * time.Hour)
	secretKey1 := "secret-key-1"
	secretKey2 := "secret-key-2"

	// Create store with first key and save token
	{
		store, err := NewTokenStore(dbPath, secretKey1)
		if err != nil {
			t.Fatalf("failed to create token store: %v", err)
		}

		err = store.SaveToken(token, expiresAt)
		if err != nil {
			t.Fatalf("failed to save token: %v", err)
		}

		store.Close()
	}

	// Try to open with different key
	{
		store, err := NewTokenStore(dbPath, secretKey2)
		if err != nil {
			t.Fatalf("failed to reopen token store: %v", err)
		}
		defer store.Close()

		// Should fail to decrypt with wrong key
		_, _, err = store.LoadToken()
		if err == nil {
			t.Errorf("expected error when loading with wrong secret key")
		}
	}
}
