package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
	"github.com/verastack/telephone/pkg/auth"
)

func main() {
	envFile := flag.String("env", ".env", "Path to .env file")
	dbPath := flag.String("db", "telephone.db", "Path to token database")
	flag.Parse()

	log.SetFlags(0)

	fmt.Println("=== Telephone Token Diagnostic ===")

	// Check .env file
	fmt.Println("1. Checking .env file token:")
	if _, err := os.Stat(*envFile); err != nil {
		fmt.Printf("   ❌ .env file not found: %s\n", *envFile)
	} else {
		if err := godotenv.Load(*envFile); err != nil {
			fmt.Printf("   ❌ Error loading .env: %v\n", err)
		} else {
			token := os.Getenv("TELEPHONE_TOKEN")
			if token == "" {
				fmt.Println("   ❌ No token found in .env")
			} else {
				fmt.Printf("   ✓ Token found in .env (length: %d)\n", len(token))

				// Try to parse it
				claims, err := auth.ParseJWTUnsafe(token)
				if err != nil {
					fmt.Printf("   ❌ Failed to parse token: %v\n", err)
				} else {
					fmt.Printf("   ✓ Token parsed successfully\n")
					fmt.Printf("     - Path ID: %s\n", claims.PathID)
					fmt.Printf("     - Subject: %s\n", claims.Sub)
					fmt.Printf("     - Expires: %s\n", claims.ExpiresAt())

					if err := claims.Validate(); err != nil {
						fmt.Printf("   ❌ Token validation failed: %v\n", err)
						if claims.ExpiresAt().Before(time.Now()) {
							timeSince := time.Since(claims.ExpiresAt())
							fmt.Printf("     → Token expired %s ago\n", timeSince.Round(time.Second))
						}
					} else {
						timeUntil := time.Until(claims.ExpiresAt())
						fmt.Printf("   ✓ Token is valid (expires in %s)\n", timeUntil.Round(time.Second))
					}
				}
			}
		}
	}

	fmt.Println()

	// Check database
	fmt.Println("2. Checking database tokens:")
	if _, err := os.Stat(*dbPath); err != nil {
		fmt.Printf("   ℹ Database file not found: %s\n", *dbPath)
		fmt.Println("   (This is normal if Telephone hasn't run successfully yet)")
	} else {
		db, err := sql.Open("sqlite3", *dbPath)
		if err != nil {
			fmt.Printf("   ❌ Failed to open database: %v\n", err)
		} else {
			defer db.Close()

			query := `
				SELECT id, expires_at, updated_at
				FROM tokens
				ORDER BY updated_at DESC
			`

			rows, err := db.Query(query)
			if err != nil {
				fmt.Printf("   ❌ Failed to query database: %v\n", err)
			} else {
				defer rows.Close()

				tokenCount := 0
				validCount := 0
				expiredCount := 0
				now := time.Now()

				for rows.Next() {
					var id int64
					var expiresAt, updatedAt time.Time

					if err := rows.Scan(&id, &expiresAt, &updatedAt); err != nil {
						fmt.Printf("   ❌ Failed to scan row: %v\n", err)
						continue
					}

					tokenCount++
					isExpired := expiresAt.Before(now)

					if isExpired {
						expiredCount++
					} else {
						validCount++
					}

					status := "✓"
					statusText := "valid"
					timeInfo := fmt.Sprintf("expires in %s", time.Until(expiresAt).Round(time.Second))

					if isExpired {
						status = "✗"
						statusText = "expired"
						timeInfo = fmt.Sprintf("expired %s ago", time.Since(expiresAt).Round(time.Second))
					}

					fmt.Printf("   %s Token #%d (%s)\n", status, id, statusText)
					fmt.Printf("     - Updated: %s\n", updatedAt.Format("2006-01-02 15:04:05"))
					fmt.Printf("     - Expires: %s\n", expiresAt.Format("2006-01-02 15:04:05"))
					fmt.Printf("     - Status: %s\n", timeInfo)
					fmt.Println()
				}

				if tokenCount == 0 {
					fmt.Println("   ℹ No tokens found in database")
				} else {
					fmt.Printf("   Summary: %d total tokens (%d valid, %d expired)\n",
						tokenCount, validCount, expiredCount)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("=== Recommendations ===")

	// Provide recommendations
	secretKey := os.Getenv("SECRET_KEY_BASE")
	if secretKey == "" {
		fmt.Println("⚠  SECRET_KEY_BASE not found in environment")
		fmt.Println("   Generate one with: openssl rand -hex 32")
		fmt.Println()
	}

	fmt.Println("To fix token issues:")
	fmt.Println("1. Generate a new token from your Plugboard server")
	fmt.Println("2. Update the 'token' variable in your .env file")
	fmt.Println("3. Run 'make run' to start Telephone")
	fmt.Println("4. Once connected, tokens will auto-refresh and persist")
}
