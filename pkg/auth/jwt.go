// Package auth provides JWT token parsing and validation functionality.
package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims represents the decoded JWT claims from Plugboard.
type JWTClaims struct {
	Sub    string `json:"sub"`     // Subject (mount point ID)
	JTI    string `json:"jti"`     // JWT ID
	PathID string `json:"path_id"` // Path identifier
	IAT    int64  `json:"iat"`     // Issued at
	Exp    int64  `json:"exp"`     // Expiration time
	jwt.RegisteredClaims
}

// ParseJWT decodes and verifies a JWT token.
func ParseJWT(tokenString, secretKey string) (*JWTClaims, error) {
	if tokenString == "" {
		return nil, fmt.Errorf("token is empty")
	}

	if secretKey == "" {
		return nil, fmt.Errorf("secret key is required for token verification")
	}

	// Parse and verify the token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		return []byte(secretKey), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	// Extract claims
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	// Validate required custom claims
	if claims.PathID == "" {
		return nil, fmt.Errorf("missing path_id claim")
	}

	if claims.Sub == "" {
		return nil, fmt.Errorf("missing sub claim")
	}

	return claims, nil
}

// ParseJWTUnsafe decodes a JWT without verifying its signature.
// WARNING: Only use this for debugging or when signature verification is not required.
func ParseJWTUnsafe(tokenString string) (*JWTClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims JWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JWT claims: %w", err)
	}

	return &claims, nil
}

// IsExpired checks if the token has expired.
func (c *JWTClaims) IsExpired() bool {
	return time.Now().Unix() > c.Exp
}

// ExpiresIn returns the duration until token expiration.
func (c *JWTClaims) ExpiresIn() time.Duration {
	expiresAt := time.Unix(c.Exp, 0)
	return time.Until(expiresAt)
}

// IssuedAt returns the time when the token was issued.
func (c *JWTClaims) IssuedAt() time.Time {
	return time.Unix(c.IAT, 0)
}

// ExpiresAt returns the expiration time.
func (c *JWTClaims) ExpiresAt() time.Time {
	return time.Unix(c.Exp, 0)
}

// Lifespan returns the total duration of the token (from issued to expiration).
func (c *JWTClaims) Lifespan() time.Duration {
	return time.Unix(c.Exp, 0).Sub(time.Unix(c.IAT, 0))
}

// TimeUntilHalfLife returns the duration until the token reaches half of its lifespan.
// Returns 0 if the token has already passed half-life.
func (c *JWTClaims) TimeUntilHalfLife() time.Duration {
	lifespan := c.Lifespan()
	halfLifeTime := time.Unix(c.IAT, 0).Add(lifespan / 2)

	duration := time.Until(halfLifeTime)
	if duration < 0 {
		return 0
	}

	return duration
}

// IsAtHalfLife returns true if the token has reached or passed half of its lifespan.
func (c *JWTClaims) IsAtHalfLife() bool {
	return c.TimeUntilHalfLife() == 0
}

// Validate performs additional validation on the claims.
func (c *JWTClaims) Validate() error {
	// Check expiration
	if c.IsExpired() {
		return fmt.Errorf("token expired at %s", c.ExpiresAt())
	}

	// Check issued at time is not in the future
	if c.IAT > time.Now().Unix() {
		return fmt.Errorf("token issued in the future: %s", c.IssuedAt())
	}

	// Validate required fields
	if c.PathID == "" {
		return fmt.Errorf("missing path_id")
	}

	if c.Sub == "" {
		return fmt.Errorf("missing subject")
	}

	if c.JTI == "" {
		return fmt.Errorf("missing JWT ID")
	}

	return nil
}
