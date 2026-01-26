// Package storage provides token persistence with encryption.
package storage

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// BenchmarkSaveToken benchmarks token encryption and storage.
func BenchmarkSaveToken(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwianRpIjoidGVzdCIsInBhdGhfaWQiOiJ0ZXN0IiwiaWF0IjoxNjAwMDAwMDAwLCJleHAiOjE2MDAwMDM2MDB9.test"
	expiresAt := time.Now().Add(1 * time.Hour)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := store.SaveToken(token, expiresAt)
		if err != nil {
			b.Fatalf("Failed to save token: %v", err)
		}
	}
}

// BenchmarkLoadToken benchmarks token decryption and loading.
func BenchmarkLoadToken(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	// Save a token first
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0ZXN0IiwianRpIjoidGVzdCIsInBhdGhfaWQiOiJ0ZXN0IiwiaWF0IjoxNjAwMDAwMDAwLCJleHAiOjE2MDAwMDM2MDB9.test"
	expiresAt := time.Now().Add(1 * time.Hour)

	err = store.SaveToken(token, expiresAt)
	if err != nil {
		b.Fatalf("Failed to save token: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := store.LoadToken()
		if err != nil {
			b.Fatalf("Failed to load token: %v", err)
		}
	}
}

// BenchmarkEncryptDecrypt benchmarks encryption/decryption cycle.
func BenchmarkEncryptDecrypt(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	plaintext := "test-token-data-for-encryption-benchmark"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		encrypted, err := store.encrypt(plaintext)
		if err != nil {
			b.Fatalf("Failed to encrypt: %v", err)
		}

		_, err = store.decrypt(encrypted)
		if err != nil {
			b.Fatalf("Failed to decrypt: %v", err)
		}
	}
}

// BenchmarkConcurrentSaveLoad benchmarks concurrent saves and loads.
func BenchmarkConcurrentSaveLoad(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			token := fmt.Sprintf("token-%d", i)
			expiresAt := time.Now().Add(1 * time.Hour)

			// Save token
			err := store.SaveToken(token, expiresAt)
			if err != nil {
				b.Fatalf("Failed to save token: %v", err)
			}

			// Load token
			_, _, err = store.LoadToken()
			if err != nil {
				b.Fatalf("Failed to load token: %v", err)
			}

			i++
		}
	})
}

// BenchmarkCleanupExpiredTokens benchmarks cleanup operation.
func BenchmarkCleanupExpiredTokens(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	// Insert some expired tokens
	for i := 0; i < 100; i++ {
		token := fmt.Sprintf("expired-token-%d", i)
		expiresAt := time.Now().Add(-1 * time.Hour) // Already expired

		err := store.SaveToken(token, expiresAt)
		if err != nil {
			b.Fatalf("Failed to save expired token: %v", err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := store.CleanupExpiredTokens()
		if err != nil {
			b.Fatalf("Failed to cleanup: %v", err)
		}
	}
}

// Benchmark for key derivation is internal to the TokenStore

// BenchmarkLargeTokenStorage benchmarks storing large tokens.
func BenchmarkLargeTokenStorage(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	store, err := NewTokenStore(dbPath, secretKey, timeout)
	if err != nil {
		b.Fatalf("Failed to create token store: %v", err)
	}
	defer store.Close()

	// Create a large token (simulating a token with lots of claims)
	largeToken := ""
	for i := 0; i < 1000; i++ {
		largeToken += "abcdefghij"
	}

	expiresAt := time.Now().Add(1 * time.Hour)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := store.SaveToken(largeToken, expiresAt)
		if err != nil {
			b.Fatalf("Failed to save large token: %v", err)
		}
	}
}

// BenchmarkDatabaseLocking benchmarks database lock contention.
func BenchmarkDatabaseLocking(b *testing.B) {
	dbPath := b.TempDir() + "/bench_tokens.db"
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	// Create multiple store instances to simulate concurrent access
	stores := make([]*TokenStore, 5)

	for i := 0; i < 5; i++ {
		store, err := NewTokenStore(dbPath, secretKey, timeout)
		if err != nil {
			b.Fatalf("Failed to create token store %d: %v", i, err)
		}

		stores[i] = store
	}

	// Cleanup all stores after benchmark completes
	b.Cleanup(func() {
		for _, store := range stores {
			store.Close()
		}
	})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			storeIdx := i % len(stores)
			token := fmt.Sprintf("token-%d", i)
			expiresAt := time.Now().Add(1 * time.Hour)

			err := stores[storeIdx].SaveToken(token, expiresAt)
			if err != nil {
				b.Fatalf("Failed to save token: %v", err)
			}

			i++
		}
	})
}

// BenchmarkFileSystemPerformance benchmarks file system operations.
func BenchmarkFileSystemPerformance(b *testing.B) {
	tempDir := b.TempDir()
	secretKey := "test-secret-key-base-at-least-64-characters-long-for-security-purposes"
	timeout := 5 * time.Second

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		dbPath := fmt.Sprintf("%s/bench_%d.db", tempDir, i)

		store, err := NewTokenStore(dbPath, secretKey, timeout)
		if err != nil {
			b.Fatalf("Failed to create token store: %v", err)
		}

		token := "test-token"
		expiresAt := time.Now().Add(1 * time.Hour)

		err = store.SaveToken(token, expiresAt)
		if err != nil {
			b.Fatalf("Failed to save token: %v", err)
		}

		store.Close()
		os.Remove(dbPath)
	}
}
