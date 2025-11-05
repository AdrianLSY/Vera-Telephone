package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// TokenStore manages encrypted token persistence in SQLite
type TokenStore struct {
	db        *sql.DB
	secretKey []byte
	mu        sync.Mutex
}

// TokenRecord represents a stored token
type TokenRecord struct {
	ID        int64
	Token     string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// NewTokenStore creates a new encrypted token store
func NewTokenStore(dbPath string, secretKeyBase string) (*TokenStore, error) {
	// Open SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Derive a 32-byte key from the secret key base using SHA-256
	hasher := sha256.New()
	hasher.Write([]byte(secretKeyBase))
	secretKey := hasher.Sum(nil)

	store := &TokenStore{
		db:        db,
		secretKey: secretKey,
	}

	// Initialize database schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the tokens table if it doesn't exist
func (ts *TokenStore) initSchema() error {
	query := `
		CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			encrypted_token TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_tokens_updated_at ON tokens(updated_at DESC);
	`

	_, err := ts.db.Exec(query)
	return err
}

// encrypt encrypts the token using AES-256-GCM
func (ts *TokenStore) encrypt(plaintext string) (string, error) {
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

// decrypt decrypts the token using AES-256-GCM
func (ts *TokenStore) decrypt(encoded string) (string, error) {
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

// SaveToken encrypts and saves a token to the database
func (ts *TokenStore) SaveToken(token string, expiresAt time.Time) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

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

	_, err = ts.db.Exec(query, encryptedToken, expiresAt, time.Now())
	if err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	return nil
}

// LoadToken retrieves and decrypts the most recent valid token
func (ts *TokenStore) LoadToken() (string, time.Time, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	query := `
		SELECT encrypted_token, expires_at
		FROM tokens
		WHERE expires_at > ?
		ORDER BY updated_at DESC
		LIMIT 1
	`

	var encryptedToken string
	var expiresAt time.Time

	err := ts.db.QueryRow(query, time.Now()).Scan(&encryptedToken, &expiresAt)
	if err == sql.ErrNoRows {
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

// CleanupExpiredTokens removes expired tokens from the database
func (ts *TokenStore) CleanupExpiredTokens() error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	query := `DELETE FROM tokens WHERE expires_at < ?`
	_, err := ts.db.Exec(query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup expired tokens: %w", err)
	}

	return nil
}

// Close closes the database connection
func (ts *TokenStore) Close() error {
	return ts.db.Close()
}

// Stats returns statistics about stored tokens
func (ts *TokenStore) Stats() (total int, valid int, expired int, err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Total tokens
	err = ts.db.QueryRow("SELECT COUNT(*) FROM tokens").Scan(&total)
	if err != nil {
		return 0, 0, 0, err
	}

	// Valid tokens
	err = ts.db.QueryRow("SELECT COUNT(*) FROM tokens WHERE expires_at > ?", time.Now()).Scan(&valid)
	if err != nil {
		return 0, 0, 0, err
	}

	expired = total - valid
	return total, valid, expired, nil
}
