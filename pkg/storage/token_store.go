package storage

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/pbkdf2"
	_ "modernc.org/sqlite" // SQLite driver
)

// TokenStore manages encrypted token persistence in SQLite.
type TokenStore struct {
	db        *sql.DB
	secretKey []byte
	mu        sync.Mutex
	timeout   time.Duration // Database operation timeout
}

// TokenRecord represents a stored token.
type TokenRecord struct {
	ID        int64
	Token     string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// NewTokenStore creates a new encrypted token store.
func NewTokenStore(dbPath, secretKeyBase string, timeout time.Duration) (*TokenStore, error) {
	if secretKeyBase == "" {
		return nil, fmt.Errorf("secret key base cannot be empty")
	}

	if timeout <= 0 {
		return nil, fmt.Errorf("timeout must be positive, got %v", timeout)
	}

	// Open SQLite database with busy timeout to handle concurrent access
	// The busy_timeout pragma helps prevent "database is locked" errors
	dbPathWithParams := fmt.Sprintf("%s?_busy_timeout=%d", dbPath, int(timeout.Milliseconds()))

	db, err := sql.Open("sqlite", dbPathWithParams)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite works best with single connection
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to enable WAL mode: %w (also failed to close db: %w)", err, closeErr)
		}

		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Derive a 32-byte key from the secret key base using PBKDF2
	// This is more secure than plain SHA-256
	secretKey := pbkdf2.Key(
		[]byte(secretKeyBase),
		[]byte("telephone-token-encryption-v1"), // salt
		100000,                                  // iterations
		32,                                      // key length (AES-256)
		sha256.New,
	)

	store := &TokenStore{
		db:        db,
		secretKey: secretKey,
		timeout:   timeout,
	}

	// Initialize database schema
	if err := store.initSchema(); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to initialize schema: %w (also failed to close db: %w)", err, closeErr)
		}

		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the tokens table if it doesn't exist.
func (ts *TokenStore) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
	defer cancel()

	query := `
		CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			encrypted_token TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tokens_updated_at ON tokens(updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_tokens_expires_at ON tokens(expires_at DESC);
	`

	_, err := ts.db.ExecContext(ctx, query)

	return err
}

// encrypt encrypts the token using AES-256-GCM.
func (ts *TokenStore) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", fmt.Errorf("plaintext cannot be empty")
	}

	block, err := aes.NewCipher(ts.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	// Create a nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt the token
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode to base64 for storage
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts the token using AES-256-GCM.
func (ts *TokenStore) decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", fmt.Errorf("encoded text cannot be empty")
	}

	// Decode from base64
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(ts.secretKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// SaveToken encrypts and saves a token to the database.
func (ts *TokenStore) SaveToken(token string, expiresAt time.Time) error {
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	if expiresAt.Before(time.Now()) {
		return fmt.Errorf("token expiration is in the past")
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
	defer cancel()

	// Start transaction
	tx, err := ts.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			// Log rollback error - we're already in error path or commit succeeded
			_ = rbErr
		}
	}()

	// Encrypt the token
	encryptedToken, err := ts.encrypt(token)
	if err != nil {
		return fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Insert into database
	query := `
		INSERT INTO tokens (encrypted_token, expires_at, updated_at)
		VALUES (?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, query, encryptedToken, expiresAt, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LoadToken retrieves and decrypts the most recent valid token.
func (ts *TokenStore) LoadToken() (string, time.Time, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
	defer cancel()

	query := `
		SELECT encrypted_token, expires_at
		FROM tokens
		WHERE expires_at > ?
		ORDER BY updated_at DESC
		LIMIT 1
	`

	var encryptedToken string

	var expiresAt time.Time

	err := ts.db.QueryRowContext(ctx, query, time.Now()).Scan(&encryptedToken, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, fmt.Errorf("no valid token found")
	}

	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to query token: %w", err)
	}

	// Decrypt the token
	token, err := ts.decrypt(encryptedToken)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to decrypt token: %w", err)
	}

	return token, expiresAt, nil
}

// CleanupExpiredTokens removes expired tokens from the database.
func (ts *TokenStore) CleanupExpiredTokens() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
	defer cancel()

	// Start transaction
	tx, err := ts.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			// Log rollback error - we're already in error path or commit succeeded
			_ = rbErr
		}
	}()

	query := `DELETE FROM tokens WHERE expires_at < ?`

	result, err := tx.ExecContext(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log how many were deleted (error is ignored as this is informational only)
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
		// This will be logged by the caller
		_ = rowsAffected
	}

	return nil
}

// Close closes the database connection and securely zeros the secret key.
func (ts *TokenStore) Close() error {
	// Zero out the secret key to prevent memory scanning attacks
	for i := range ts.secretKey {
		ts.secretKey[i] = 0
	}

	ts.secretKey = nil

	return ts.db.Close()
}

// Stats returns statistics about stored tokens.
func (ts *TokenStore) Stats() (total, valid, expired int, err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), ts.timeout)
	defer cancel()

	// Total tokens
	err = ts.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tokens").Scan(&total)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count total tokens: %w", err)
	}

	// Valid tokens
	err = ts.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tokens WHERE expires_at > ?", time.Now()).Scan(&valid)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count valid tokens: %w", err)
	}

	expired = total - valid

	return total, valid, expired, nil
}
