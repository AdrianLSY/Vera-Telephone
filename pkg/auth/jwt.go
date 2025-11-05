package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// JWTClaims represents the decoded JWT claims from Plugboard
type JWTClaims struct {
	Sub    string `json:"sub"`     // Subject (mount point ID)
	JTI    string `json:"jti"`     // JWT ID
	PathID string `json:"path_id"` // Path identifier
	IAT    int64  `json:"iat"`     // Issued at
	Exp    int64  `json:"exp"`     // Expiration time
}

// ParseJWT decodes a JWT token without verification
// Note: We don't verify the signature since we trust the token from environment
func ParseJWT(token string) (*JWTClaims, error) {
	parts := strings.Split(token, ".")
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

// IsExpired checks if the token has expired
func (c *JWTClaims) IsExpired() bool {
	return time.Now().Unix() > c.Exp
}

// ExpiresIn returns the duration until token expiration
func (c *JWTClaims) ExpiresIn() time.Duration {
	expiresAt := time.Unix(c.Exp, 0)
	return time.Until(expiresAt)
}

// IssuedAt returns the time when the token was issued
func (c *JWTClaims) IssuedAt() time.Time {
	return time.Unix(c.IAT, 0)
}

// ExpiresAt returns the expiration time
func (c *JWTClaims) ExpiresAt() time.Time {
	return time.Unix(c.Exp, 0)
}
